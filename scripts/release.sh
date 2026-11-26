#!/usr/bin/env bash
# 把打好的安卓包发到自建更新通道（ADR-0015）。
#
# 用法（仓库根目录）：
#   cd android && ./gradlew assembleServerRelease && cd ..
#   APK=android/app/build/outputs/apk/server/release/app-server-release.apk \
#   MEMORA_HOST=<ssh 别名> \
#   NOTES=这次改了什么 \
#   ./scripts/release.sh
#
# 主机、目录、口令一律走环境变量或 ssh 配置别名，不进脚本也不进仓库（writing-rules §9）。
# 服务端每次请求才读这两个文件，所以发版动作就是"把它们放对位置"，不需要重启服务。
set -euo pipefail

APK=${APK:?需要 APK，指到 gradle 打出来的那个文件}
HOST=${MEMORA_HOST:?需要 MEMORA_HOST，一个能 publickey 登录的 ssh 别名或 用户@地址}
SSH=${MEMORA_SSH:-ssh}
SCP=${MEMORA_SCP:-scp}
REMOTE_DIR=${MEMORA_RELEASE_DIR:-/var/lib/memora/release}
NOTES=${NOTES:-}
# 目录名带单引号会把后面那条远程命令的引号拼坏，先挡掉。
case "$REMOTE_DIR" in
  *"'"*) echo "MEMORA_RELEASE_DIR 里不要放单引号"; exit 1 ;;
esac
[[ -f "$APK" ]] || { echo "找不到 APK：$APK"; exit 1; }

# 版本号从工程里读，不让人在命令行上手打一个和包内容不一致的数——那是最难查的发错。
GRADLE=${GRADLE:-android/app/build.gradle.kts}
VERSION_CODE=${VERSION_CODE:-$(sed -n 's/.*versionCode = \([0-9][0-9]*\).*/\1/p' "$GRADLE" | head -1)}
VERSION_NAME=${VERSION_NAME:-$(sed -n 's/.*versionName = "\([^"]*\)".*/\1/p' "$GRADLE" | head -1)}
[[ -n "$VERSION_CODE" && -n "$VERSION_NAME" ]] || { echo "没从 $GRADLE 读到 versionCode / versionName"; exit 1; }

PY=''
for p in python3 python py; do
  if command -v "$p" >/dev/null 2>&1 && "$p" -c 'import sys' >/dev/null 2>&1; then PY=$p; break; fi
done
[[ -n "$PY" ]] || { echo "生成 release.json 需要一个 Python（python3 / python / py）"; exit 1; }

WORK=$(mktemp -d)
trap 'rm -rf "$WORK"' EXIT

# notes 里可能有引号、换行、中文，用 json 库生成，不在 shell 里拼字符串。
# 拼错一个引号，客户端只会安静地什么都不显示，比报错更难查。
NOTES="$NOTES" VERSION_CODE="$VERSION_CODE" VERSION_NAME="$VERSION_NAME" \
  "$PY" -c '
import json, os, sys
json.dump({
    "version_code": int(os.environ["VERSION_CODE"]),
    "version_name": os.environ["VERSION_NAME"],
    "notes": os.environ["NOTES"],
}, open(sys.argv[1], "w", encoding="utf-8"), ensure_ascii=False, indent=2)
' "$WORK/release.json"

LOCAL_SIZE=$(wc -c <"$APK" | tr -d ' ')
# 上传落 tmp，再一次性换名：中途不会留下"清单在、包不在"的半发布状态。
REMOTE_TMP="$REMOTE_DIR.tmp.$$"
$SSH "$HOST" "umask 022 && rm -rf '${REMOTE_DIR}.tmp.$$' && mkdir -p '${REMOTE_DIR}.tmp.$$'"
$SCP -q "$WORK/release.json" "$HOST:$REMOTE_TMP/release.json"
$SCP -q "$APK" "$HOST:$REMOTE_TMP/app.apk"
$SSH "$HOST" "
  test -f '${REMOTE_TMP}/app.apk' || { echo '上传没落到位'; exit 1; }
  rm -rf '$REMOTE_DIR'
  mv '${REMOTE_TMP}' '$REMOTE_DIR'
  ls -l '$REMOTE_DIR'
"

cat <<EOF
已发布 v${VERSION_NAME}（versionCode=${VERSION_CODE}，${LOCAL_SIZE} 字节）到 ${REMOTE_DIR}。
服务端配置里要有 MEMORA_RELEASE_DIR=${REMOTE_DIR}，且进程用户读得到这两个文件——
第一次发布时确认一次即可，systemd 的 ReadWritePaths 已经覆盖 ${REMOTE_DIR} 的父目录。
客户端下一次进主屏就会提醒更新。
EOF
