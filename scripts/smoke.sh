#!/usr/bin/env bash
# 端到端冒烟：本机起 HTTPS 服务 + 假模型，把设计与代码里所有可验收的断言跑一遍。
# 不需要云厂商的安全组规则，也不需要真 API key（writing-rules §9：真值不进仓库）。
#
#   make smoke
set -uo pipefail

cd "$(dirname "$0")/.."
ROOT=$PWD
WORK=$(mktemp -d)
# MSYS 的 /tmp 是它自己的虚拟挂载点，原生 Windows 程序（go、被 -config 喂进去的那些路径）
# 照着当前盘符去解析它，于是库文件与证书会写到别处去。换成正斜杠的 Windows 路径，
# 两边说的是同一个目录；Linux 上没有 cygpath，这一行自然跳过。
command -v cygpath >/dev/null 2>&1 && WORK=$(cygpath -m "$WORK")
PORT=${SMOKE_PORT:-18443}
STUB_PORT=${STUB_PORT:-18480}
BASE=https://127.0.0.1:${PORT}
# 必须 ASCII，和 STUB_KEY 同一条理由：Git Bash 的 curl 会先把命令行参数按本机 ANSI 码页
# （中文 Windows 是 GBK）压进请求头，服务端 latin-1 解出来的字节和 printf 喂给 hashpass 的
# UTF-8 不是同一串，bcrypt 对不上——于是"口令正确"这一项在 Windows 上永远 401。
# 安卓端不受影响：OkHttp 直接发 UTF-8。
KEY='smoke-passphrase-not-reused'
STUB_KEY='smoke-stub-key-ascii-only'
ENV=$WORK/memora.env
HITS=$WORK/llm-hits.jsonl
RELEASE=$WORK/release
BIN=$WORK/memora$(go env GOEXE)
SRV_PID=''
STUB_PID=''

# 这台机器上叫 python3 的那条可能是 Windows 的应用商店占位符（跑什么都不输出、退出码 49），
# 所以按"能不能真跑起来"挑解释器而不是按名字挑。真值不进仓库，这里挑到的只是个命令名。
PY=''
for p in python3 python py; do
  if command -v "$p" >/dev/null 2>&1 && "$p" -c 'import sys, sqlite3' >/dev/null 2>&1; then PY=$p; break; fi
done
if [[ -z "$PY" ]]; then
  echo "冒烟需要带 sqlite3 模块的 Python（python3 / python / py 任一）"
  exit 1
fi
# Python 在中文 Windows 上按 ANSI 码页收发 stdio：服务端回的是 UTF-8，它拿 GBK 解，
# 一个汉字后面的引号会被当成第二个尾字节吃掉，于是 JSON 解析当场报错。
# 另外 print('阿'*2000) 经 GBK 出来是 4000 字节，正文长度上限那两项会误判成超限。
export PYTHONUTF8=1

# Git Bash 把 UTF-8 命令行参数按 ANSI 码页转一遍才交给 Windows 版 curl，中文正文进库前
# 就成了 GBK（安卓端不受影响：OkHttp 直接发 UTF-8）。凡是载荷里带非 ASCII，一律写进文件
# 再 -d @文件——curl 自己按字节读，中间没人转码。用法：-d "$(payload '{...}')"。
BODYF=$WORK/body.json
payload() { printf '%s' "$1" >"$BODYF"; printf '@%s' "$BODYF"; }
# 查询串同理：先按 UTF-8 百分号编码，落到 curl 参数上时已经全是 ASCII。
qenc() { printf '%s' "$1" | "$PY" -c 'import sys,urllib.parse;print(urllib.parse.quote(sys.stdin.buffer.read()))'; }

pass=0; fail=0
ok()     { pass=$((pass+1)); printf '  ✓ %s\n' "$1"; }
bad()    { fail=$((fail+1)); printf '  ✗ %s\n     %s\n' "$1" "$2"; }
check()  { [[ "$2" == "$3" ]] && ok "$1" || bad "$1" "期望 [$2] 实得 [$3]"; }
has()    { [[ "$3" == *"$2"* ]] && ok "$1" || bad "$1" "[$3] 里找不到 [$2]"; }
# 检文件内容，不检字符串：把路径喂给 has() 只会永远失败，看起来像产品坏了。
infl()   { grep -qF -- "$2" "$3" && ok "$1" || bad "$1" "$3 里找不到 [$2]（$(tail -3 "$3")）"; }
say()    { printf '\n== %s\n' "$1"; }
hits()   { awk 'END{print NR+0}' "$HITS" 2>/dev/null; }
code()   { curl -sk -o /dev/null -w '%{http_code}' "$@"; }
get()    { curl -sk -H "X-Access-Key: $KEY" "$BASE$1"; }
jqp()    { "$PY" -c "import json,sys; d=json.load(sys.stdin); print(d$1)"; }
# 时间戳转 epoch 秒。Go 写的是纳秒，Python 3.10 的 fromisoformat 最多吃到微秒，先裁掉尾巴。
iso_ts() { "$PY" -c '
import json, re, sys, datetime as dt
s = json.load(sys.stdin)[sys.argv[1]]
s = re.sub(r"(\.\d{6})\d+", r"\1", s)
print(int(dt.datetime.fromisoformat(s.replace("Z", "+00:00")).timestamp()))' "$1"; }

cleanup() {
  [[ -n "$SRV_PID"  ]] && kill "$SRV_PID"  2>/dev/null
  [[ -n "$STUB_PID" ]] && kill "$STUB_PID" 2>/dev/null
  wait 2>/dev/null
  rm -rf "$WORK"
}
trap cleanup EXIT

start_server() { # $1 = 日志文件名
  "$BIN" serve -config "$ENV" >"$WORK/$1" 2>&1 &
  SRV_PID=$!
  for _ in $(seq 60); do [[ "$(code "$BASE/healthz")" == "200" ]] && return 0; sleep 0.2; done
  bad "服务起不来" "$(tail -5 "$WORK/$1")"
  return 1
}

stop_server() { [[ -n "$SRV_PID" ]] && { kill "$SRV_PID"; wait "$SRV_PID" 2>/dev/null; }; SRV_PID=''; }

say "编译、证书、口令哈希"
go build -o "$BIN" ./cmd/memora || { echo "编译失败"; exit 1; }
[[ -x "$BIN" ]] || { echo "二进制没产出"; exit 1; }
ok "go build 产出单二进制"

# MSYS_NO_PATHCONV 只在签证书这一步要：Git Bash 会把 openssl 的 -subj "/CN=memora"
# 改写成 D:/Git/CN=memora，openssl 直接报格式错（同 android/README.md 那条）。
MEMORA_PUBLIC_IP=127.0.0.1 MSYS_NO_PATHCONV=1 bash deploy/mkcert.sh "$WORK/tls" >"$WORK/mkcert.log" 2>&1
[[ -f "$WORK/tls/server.crt" && -f "$WORK/tls/server.key" ]] \
  || { echo "证书没生成：$(cat "$WORK/mkcert.log")"; exit 1; }
ok "自签 ECDSA 证书就绪（SAN=IP:127.0.0.1）"

# 口令走标准输入，不进 argv——argv 会出现在进程列表里。
HASH=$(printf '%s' "$KEY" | "$BIN" hashpass | head -1)
[[ "$HASH" == '$2'* ]] && ok "hashpass 产出 bcrypt 哈希" || { echo "hashpass 没吐出哈希: $HASH"; exit 1; }
"$BIN" hashpass </dev/null >/dev/null 2>&1 && bad "空输入应失败" "却成功了" || ok "空输入被拒绝"
printf '%s' "$("$PY" -c "print('长'*100)")" | "$BIN" hashpass >/dev/null 2>&1 \
  && bad "超 72 字节应失败" "却成功了" || ok "超 72 字节的口令被拒绝"
"$BIN" version >/dev/null && ok "version 子命令可用"

cat >"$ENV" <<EOF
MEMORA_LISTEN_ADDR=127.0.0.1:${PORT}
MEMORA_DB_PATH=$WORK/memora.db
MEMORA_TLS_CERT=$WORK/tls/server.crt
MEMORA_TLS_KEY=$WORK/tls/server.key
MEMORA_PASS_HASH=$HASH
MEMORA_LOCAL_TZ=+08:00
MEMORA_LLM_BASE_URL=http://127.0.0.1:${STUB_PORT}/v1
MEMORA_LLM_MODEL=冒烟假模型
MEMORA_LLM_API_KEY=$STUB_KEY
MEMORA_RELEASE_DIR=$RELEASE
EOF

# 更新目录先空着：进版那一段要的就是"一个版都没发过"这个起点（ADR-0015）。
mkdir -p "$RELEASE"

STUB_LLM_HITS="$HITS" "$PY" scripts/stub-llm.py "127.0.0.1:${STUB_PORT}" >"$WORK/stub.log" 2>&1 &
STUB_PID=$!
: >"$HITS"
start_server server.log || exit 1

say "口令边界（FR-07）"
check "healthz = 200" 200 "$(code "$BASE/healthz")"
check "无口令读记录 = 401" 401 "$(code "$BASE/v1/notes")"
has "401 响应体不含业务数据" '"unauthorized"' "$(curl -sk "$BASE/v1/notes")"
check "错口令 = 401" 401 "$(code -H 'X-Access-Key: 故意错' "$BASE/v1/notes")"
check "对口令 = 200" 200 "$(code -H "X-Access-Key: $KEY" "$BASE/v1/notes")"
check "报表接口同样要口令" 401 "$(code "$BASE/v1/reports")"
check "周期接口同样要口令" 401 "$(code "$BASE/v1/periods/now")"

say "记录读写与归属"
TODAY=$(TZ=UTC-8 date +%F)   # 服务配的是 +08，这里换成同一套日历
OUT=$(curl -sk -H "X-Access-Key: $KEY" -d "$(payload '{"body":"给张三写季度总结"}')" -w $'\n%{http_code}' "$BASE/v1/notes")
check "新建 = 201" 201 "${OUT##*$'\n'}"
read -r ID WEEK MONTH <<<"$(printf '%s' "${OUT%$'\n'*}" | "$PY" -c 'import json,sys; n=json.load(sys.stdin); print(n["id"], n["week_start"], n["month_start"])')"
check "响应带回 id" 1 "$ID"
"$PY" - "$WEEK" "$TODAY" <<'EOF' || bad "归属周" "$WEEK / $TODAY"
import sys, datetime
week, today = (datetime.date.fromisoformat(x) for x in sys.argv[1:3])
assert week.weekday() == 0, "week_start 不是周一"
assert week <= today < week + datetime.timedelta(days=7), "今天不在这个周里"
EOF
ok "归属周 = $WEEK（周一，含今天 $TODAY）"
check "归属月 = 本月 1 号" "${TODAY%-*}-01" "$MONTH"

# 客户端不许自己算周一：它要的生成的钥匙从这个接口拿。三条比对钉住
# "接口给的"和"服务端写记录时算的"是同一个答案。
PER=$(get "/v1/periods/now")
check "接口给的周 = 刚写记录的归属周" "$WEEK" "$(printf '%s' "$PER" | jqp "['week_start']")"
check "接口给的月 = 刚写记录的归属月" "$MONTH" "$(printf '%s' "$PER" | jqp "['month_start']")"
check "接口给的周末日 = 该周 +6 天" \
  "$("$PY" -c "import datetime;print((datetime.date.fromisoformat('$WEEK')+datetime.timedelta(days=6)).isoformat())")" \
  "$(printf '%s' "$PER" | jqp "['week_end']")"

OUT=$(curl -sk -X PATCH -H "X-Access-Key: $KEY" -d "$(payload '{"body":"给张三写半年总结"}')" -w $'\n%{http_code}' "$BASE/v1/notes/$ID")
check "修改 = 200" 200 "${OUT##*$'\n'}"
has "改后的正文读得回" "半年" "${OUT%$'\n'*}"
has "改正文不搬动归属" "\"week_start\":\"$WEEK\"" "${OUT%$'\n'*}"

# 字符按码点给，不写成字面量：Windows 版 Python 的 argv 同样先过一遍 ANSI 码页，
# 一个字面量"阿"会被转成两三个字符，"恰好 2000 字"这条就永远测不准了。
LONG=$("$PY" -c "print(chr(38463)*2001)")
EXACT=$("$PY" -c "print(chr(38463)*2000)")
check "2001 字 = 413（不静默截断）" 413 "$(code -X POST -H "X-Access-Key: $KEY" -d "$(payload "{\"body\":\"$LONG\"}")" "$BASE/v1/notes")"
check "恰好 2000 字 = 201" 201 "$(code -X POST -H "X-Access-Key: $KEY" -d "$(payload "{\"body\":\"$EXACT\"}")" "$BASE/v1/notes")"
check "空正文 = 400" 400 "$(code -X POST -H "X-Access-Key: $KEY" -d '{"body":"   "}' "$BASE/v1/notes")"
check "坏 JSON = 400" 400 "$(code -X POST -H "X-Access-Key: $KEY" -d '{' "$BASE/v1/notes")"
check "非整数 id = 400" 400 "$(code -H "X-Access-Key: $KEY" "$BASE/v1/notes/abc")"
check "不存在的 id = 404" 404 "$(code -H "X-Access-Key: $KEY" "$BASE/v1/notes/9999")"
check "搜索命中 = 1 条" 1 "$(curl -sk -H "X-Access-Key: $KEY" "$BASE/v1/notes?q=$(qenc 总结)" | jqp "['count']")"
check "q=% 当字面量，不串味" 0 "$(curl -sk -H "X-Access-Key: $KEY" "$BASE/v1/notes?q=%25" | jqp "['count']")"
check "before 游标生效" 0 "$(curl -sk -H "X-Access-Key: $KEY" "$BASE/v1/notes?before=2000-01-01T00:00:00Z" | jqp "['count']")"
check "DELETE = 204" 204 "$(code -X DELETE -H "X-Access-Key: $KEY" "$BASE/v1/notes/$ID")"
check "重复 DELETE = 404" 404 "$(code -X DELETE -H "X-Access-Key: $KEY" "$BASE/v1/notes/$ID")"

say "日志与库文件"
grep -qE "给张三|半年总结|X-Access-Key|$KEY" "$WORK/server.log" \
  && bad "访问日志不该有正文和口令" "$(grep -m1 -E '给张三|半年总结' "$WORK/server.log")" \
  || ok "访问日志只有方法/路径/状态/耗时/来源"
infl "日志里有访问记录本身" "path=/v1/notes" "$WORK/server.log"
[[ -f "$WORK/memora.db" ]] && ok "库文件在配置指定路径" || bad "库文件" "不存在"

say "报表：手动生成进行中的本周（FR-06）"
curl -sk -X POST -H "X-Access-Key: $KEY" -d "$(payload '{"body":"本周把报表模块打通"}')" "$BASE/v1/notes" >/dev/null
N=$(get "/v1/notes?limit=200" | jqp "['count']")   # 本周实际有几条，冒烟前面故意造了几条边界数据
START=$(date -u +%s)
OUT=$(curl -sk -X POST -H "X-Access-Key: $KEY" -w $'\n%{http_code}' "$BASE/v1/reports/week/$WEEK/generate")
CODE=${OUT##*$'\n'}; BODY=${OUT%$'\n'*}
check "生成本周 = 200" 200 "$CODE"
has "正文来自模型，条数与库里一致" "收到 $N 条记录" "$BODY"
has "提示词规则也传给了模型" "你是周报与月报的写手" "$BODY"
check "source_count = 本周记录数 $N" "$N" "$(printf '%s' "$BODY" | jqp "['source_count']")"
GEND=$(printf '%s' "$BODY" | iso_ts generated_at)
GAP=$((GEND - START))
[[ $GAP -ge 1 && $GAP -le 5 ]] && ok "generated_at = 模型答完那一刻（发起后 ${GAP}s，不是发起瞬间）" \
  || bad "generated_at 时机" "发起后 ${GAP}s（期望 1–5s，假模型睡 1s）"
check "单份读得回" ready "$(get "/v1/reports/week/$WEEK" | jqp "['status']")"
check "报表列表按周期倒序且含 status" ready "$(curl -sk -H "X-Access-Key: $KEY" "$BASE/v1/reports?kind=week" | "$PY" -c 'import json,sys; print(json.load(sys.stdin)["reports"][0]["status"])')"
check "带口令才有报表数据" 200 "$(code -H "X-Access-Key: $KEY" "$BASE/v1/reports")"

say "报表：参数与错误形状"
BADWEEK=$("$PY" -c "import datetime;print((datetime.date.fromisoformat('$WEEK')+datetime.timedelta(days=1)).isoformat())")
H0=$(hits)
check "非周一的周报起始日 = 400 bad_period" 400 "$(code -X POST -H "X-Access-Key: $KEY" "$BASE/v1/reports/week/$BADWEEK/generate")"
check "非 1 号的月报起始日 = 400" 400 "$(code -H "X-Access-Key: $KEY" "$BASE/v1/reports/month/${TODAY%-*}-15")"
check "未知 kind = 400" 400 "$(code -H "X-Access-Key: $KEY" "$BASE/v1/reports/day/$WEEK")"
check "坏日期格式 = 400" 400 "$(code -H "X-Access-Key: $KEY" "$BASE/v1/reports/week/not-a-date")"
check "参数不合法时一次模型都没打" 0 "$(($(hits) - H0))"
check "空周期生成 = 409 no_records" 409 "$(code -X POST -H "X-Access-Key: $KEY" "$BASE/v1/reports/week/2020-01-06/generate")"
check "没生成过的周期 = 404" 404 "$(code -H "X-Access-Key: $KEY" "$BASE/v1/reports/week/2020-01-06")"
has "404 带 not_generated 码" "not_generated" "$(get "/v1/reports/week/2020-01-06")"

say "待办：队首插入、拖拽整列提交、勾掉与取消（FR-09/FR-10）"
# 顺序、名次、归属都从接口现读，不在 shell 里算日期：那正是客户端不许自己算周一的理由。
tbodies() { get "/v1/tasks" | "$PY" -c 'import json,sys; print(",".join(t["body"] for t in json.load(sys.stdin)["tasks"]))'; }
tranks()  { get "/v1/tasks" | "$PY" -c 'import json,sys; print(",".join(str(t["rank"]) for t in json.load(sys.stdin)["tasks"] if not t["done"]))'; }
tid()     { get "/v1/tasks" | "$PY" -c "import json,sys; print(json.load(sys.stdin)['tasks'][$1]['id'])"; }
tweek()   { get "/v1/tasks" | "$PY" -c "import json,sys; print([t for t in json.load(sys.stdin)['tasks'] if t['id']==$1][0]['done_week_start'])"; }

check "待办接口同样要口令" 401 "$(code "$BASE/v1/tasks")"
for B in 约评审 买猫粮 修漏水; do
  curl -sk -X POST -H "X-Access-Key: $KEY" -d "$(payload "{\"body\":\"记着$B\"}")" "$BASE/v1/tasks" >/dev/null
done
check "新建插到队首，读出来是提交顺序的倒序" "记着修漏水,记着买猫粮,记着约评审" "$(tbodies)"
check "未完成名次连续无空洞" "1,2,3" "$(tranks)"
check "超限的待办正文 = 413，不静默截断" 413 "$(code -X POST -H "X-Access-Key: $KEY" -d "$(payload "{\"body\":\"$LONG\"}")" "$BASE/v1/tasks")"
check "空 PATCH = 400" 400 "$(code -X PATCH -H "X-Access-Key: $KEY" -d '{}' "$BASE/v1/tasks/1")"

# 安排日期（ADR-0021）：显式日期原样回读、坏日期与清空都拒；缺省必须是合法自然日。
DATED_JSON=$(curl -sk -X POST -H "X-Access-Key: $KEY" -d "$(payload '{"body":"十二月的事","due_on":"2026-12-24"}')" "$BASE/v1/tasks")
DATED=$(printf '%s' "$DATED_JSON" | jqp "['id']")
check "带安排日期创建 = 2026-12-24" "2026-12-24" "$(printf '%s' "$DATED_JSON" | jqp "['due_on']")"
check "PATCH 改安排日期 = 2026-12-25" "2026-12-25" \
  "$(curl -sk -X PATCH -H "X-Access-Key: $KEY" -d '{"due_on":"2026-12-25"}' "$BASE/v1/tasks/$DATED" | jqp "['due_on']")"
check "坏安排日期 = 400" 400 "$(code -X POST -H "X-Access-Key: $KEY" -d '{"body":"坏日期","due_on":"2026-02-30"}' "$BASE/v1/tasks")"
check "清空安排日期 = 400" 400 "$(code -X PATCH -H "X-Access-Key: $KEY" -d '{"due_on":""}' "$BASE/v1/tasks/$DATED")"
check "缺省的安排日期是合法自然日" "ok" \
  "$(get "/v1/tasks" | "$PY" -c 'import datetime,json,sys
try:
    datetime.date.fromisoformat(json.load(sys.stdin)["tasks"][-1]["due_on"]); print("ok")
except Exception:
    print("bad")')"
check "删掉带日期的临时待办 = 204" 204 "$(code -X DELETE -H "X-Access-Key: $KEY" "$BASE/v1/tasks/$DATED")"

MID=$(tid 1)
IDS=$(get "/v1/tasks" | "$PY" -c 'import json,sys; print(",".join(str(t["id"]) for t in json.load(sys.stdin)["tasks"] if not t["done"]))')
STALE=$("$PY" -c "print('[' + ','.join(reversed('$IDS'.split(','))) + ']')")
check "整列重排 = 200，顺序就是提交的那个" "记着约评审,记着买猫粮,记着修漏水" \
  "$(curl -sk -X POST -H "X-Access-Key: $KEY" -d "{\"ids\":$STALE}" "$BASE/v1/tasks/reorder" >/dev/null; tbodies)"
check "名次跟着重排走" "1,2,3" "$(tranks)"
BAD=$(printf '[%s]' "$(cut -d, -f1-2 <<<"$IDS")")
check "漏一条的重排 = 400" 400 "$(code -X POST -H "X-Access-Key: $KEY" -d "{\"ids\":$BAD}" "$BASE/v1/tasks/reorder")"
has "被拒时说要刷新后重拖" "刷新后重拖" "$(curl -sk -X POST -H "X-Access-Key: $KEY" -d "{\"ids\":$BAD}" "$BASE/v1/tasks/reorder")"
check "被拒的那次一点没生效" "记着约评审,记着买猫粮,记着修漏水" "$(tbodies)"

check "勾掉 = 200" 200 "$(code -X PATCH -H "X-Access-Key: $KEY" -d '{"done":true}' "$BASE/v1/tasks/$MID")"
check "勾掉的沉到已完成区" "记着约评审,记着修漏水,记着买猫粮" "$(tbodies)"
check "勾掉之后前面的名次收紧" "1,2" "$(tranks)"
check "勾掉那一刻盖上归属周" "$WEEK" "$(tweek "$MID")"
check "取消勾选 = 200" 200 "$(code -X PATCH -H "X-Access-Key: $KEY" -d '{"done":false}' "$BASE/v1/tasks/$MID")"
check "取消之后落到未完成队尾" "记着约评审,记着修漏水,记着买猫粮" "$(tbodies)"
check "取消之后归属列清空（报表不再认它）" "" "$(tweek "$MID")"
check "重复勾同一条不改写归属" "$WEEK" \
  "$(curl -sk -X PATCH -H "X-Access-Key: $KEY" -d '{"done":true}' "$BASE/v1/tasks/$MID" >/dev/null; tweek "$MID")"

DROP=$(curl -sk -X POST -H "X-Access-Key: $KEY" -d "$(payload '{"body":"这条是用来删的"}')" "$BASE/v1/tasks" | jqp "['id']")
check "删除 = 204" 204 "$(code -X DELETE -H "X-Access-Key: $KEY" "$BASE/v1/tasks/$DROP")"
check "重复删除 = 404" 404 "$(code -X DELETE -H "X-Access-Key: $KEY" "$BASE/v1/tasks/$DROP")"

say "待办进报表：勾掉的算这一期，没勾掉的不算（FR-11）"
# 再勾一条，让这一周既有记录又有勾掉的待办；第三条留着不勾，它一个字都不该进提示词。
curl -sk -X PATCH -H "X-Access-Key: $KEY" -d '{"done":true}' "$BASE/v1/tasks/$(tid 0)" >/dev/null
N2=$(get "/v1/notes?limit=200" | jqp "['count']")
T2=$(get "/v1/tasks" | "$PY" -c 'import json,sys; print(sum(1 for t in json.load(sys.stdin)["tasks"] if t["done"]))')
: >"$HITS"
OUT=$(curl -sk -X POST -H "X-Access-Key: $KEY" -w $'\n%{http_code}' "$BASE/v1/reports/week/$WEEK/generate")
BODY=${OUT%$'\n'*}
check "生成 = 200" 200 "${OUT##*$'\n'}"
check "source_count = 记录数 + 勾掉的待办数" "$((N2 + T2))" "$(printf '%s' "$BODY" | jqp "['source_count']")"
has "喂给模型的素材行数跟着一起涨" "收到 $((N2 + T2)) 条记录" "$BODY"
infl "待办单独成段，不跟记录混排" "本周期内已勾掉的待办" "$HITS"
infl "勾掉的正文真的进了提示词" "记着约评审" "$HITS"
grep -qF -- "记着修漏水" "$HITS" && bad "没勾掉的待办混进了提示词" "$(grep -m1 记着修漏水 "$HITS")" \
  || ok "没勾掉的待办一个字都不出现（FR-11）"

say "报表：已结束的周期——失败要留得下来（FR-05 不静默缺失）"
# 把那条记录搬进上一个完整周：归属列是插入时算好的，这里直接改库模拟"上一周记的事"，
# 好让补跑判据（周期已结束 + 没有 ready）在冒烟里有东西可判。
PREV=$("$PY" -c "import datetime;print((datetime.date.fromisoformat('$WEEK')-datetime.timedelta(days=7)).isoformat())")
PREVMONTH=${PREV%-*}-01
# 直接插一条属于上一周的记录：归属列本该由服务端在插入那一刻算好，但冒烟要的是
# "一个已结束的周期"，而今天永远落在当前周里，等不到上一个周期自己结束。
stop_server
# 用 Python 的标准库改库而不是 sqlite3 命令行：Windows 那台机器上没装 CLI，
# 而这一笔是补跑用例的前提，缺了它整段"已结束的周期"就没东西可判。
"$PY" - "$WORK/memora.db" "$PREV" "$PREVMONTH" <<'EOF'
import sqlite3, sys
db, prev, prevmonth = sys.argv[1:4]
con = sqlite3.connect(db)
con.executescript(f"""
DELETE FROM notes;
DELETE FROM reports;
INSERT INTO notes (id, body, created_at, updated_at, week_start, month_start)
VALUES (900, '上周留在库里的一条', '{prev}T01:00:00.000Z', '{prev}T01:00:00.000Z', '{prev}', '{prevmonth}');
""")
con.commit()
con.close()
EOF
# key 在起服务之前就换错。留着好 key 起这一次，启动即跑的补例会先把上一周补成 ready，
# 下面"失败要留得下来"读到的就是那份成功的报表——能不能撞上，取决于本机那两次 curl
# 有没有熬过假模型的 1s 延迟。Linux 上碰巧赢了这个竞态，Windows 上必输。
sed -i "s/^MEMORA_LLM_API_KEY=.*/MEMORA_LLM_API_KEY=故意写错的key/" "$ENV"
start_server server-prev.log || exit 1
check "已结束周期的记录读得回" "$PREV" "$(get "/v1/notes/900" | jqp "['week_start']")"
check "它确实落在过去的一周" 1 "$("$PY" -c "import datetime;print(int(datetime.date.fromisoformat('$PREV') < datetime.date.fromisoformat('$TODAY')))")"

# 补跑是启动时另起的协程，坏 key 下模型立刻回 401（假延迟在鉴权之后），但落库仍要等它跑完，
# 所以轮询而不是拍一个固定 sleep。第一轮没生成成功前读它是 404，把 jqp 的报错咽掉。
for _ in $(seq 40); do
  [[ "$(get "/v1/reports/week/$PREV" | jqp "['status']" 2>/dev/null)" == failed ]] && break
  sleep 0.25
done
check "坏 key 下补跑没造出报表，但把失败留进了库" failed "$(get "/v1/reports/week/$PREV" | jqp "['status']")"
: >"$HITS"
check "key 不对时生成 = 502" 502 "$(code -X POST -H "X-Access-Key: $KEY" "$BASE/v1/reports/week/$PREV/generate")"
BODY=$(get "/v1/reports/week/$PREV")
has "失败报表读得回且状态 failed" '"status":"failed"' "$BODY"
has "失败原因落库" "key 不对" "$BODY"
check "重试恰好 1 次（共 2 次调用）" 2 "$(hits)"

say "补跑：服务重启后自动把 failed 治好"
stop_server
sed -i "s/^MEMORA_LLM_API_KEY=.*/MEMORA_LLM_API_KEY=$STUB_KEY/" "$ENV"
: >"$HITS"
start_server server-backfill.log || exit 1
# 上一周所属的那个月可能也结束了（月初那几天必然如此），那样月报同样是待补项。
EXPECTED=$("$PY" -c "
import datetime
prev = datetime.date.fromisoformat('$PREV')
today = datetime.date.fromisoformat('$TODAY')
nxt = (prev.replace(day=1) + datetime.timedelta(days=32)).replace(day=1)
print(1 + (1 if nxt <= today else 0))")
# 启动即扫一轮，不等 ticker。候选数在动手生成就之前打印，所以这行不该要等模型跑完。
grep -q 待补周期 "$WORK/server-backfill.log" && ok "扫描一开头就报告候选数" \
  || bad "扫描没打候选数" "$(tail -3 "$WORK/server-backfill.log")"
check "首轮候选数 = $EXPECTED（那个 failed 的周期）" "$EXPECTED" "$(grep -o '待补周期=[0-9]*' "$WORK/server-backfill.log" | head -1 | cut -d= -f2)"
for _ in $(seq 60); do [[ "$(get "/v1/reports/week/$PREV" | jqp "['status']")" == ready ]] && break; sleep 0.5; done
check "一个请求都没发，报表自己变成 ready" ready "$(get "/v1/reports/week/$PREV" | jqp "['status']")"
check "补跑只按候选数打模型，一次不多" "$EXPECTED" "$(hits)"

say "补跑：已 ready 的周期不重复生成"
stop_server
: >"$HITS"
start_server server-again.log || exit 1
sleep 3
check "第二轮什么都没补" 0 "$(hits)"
infl "但判据确实跑了" "待补周期=0" "$WORK/server-again.log"

say "更新通道：一个版都没发过的样子（ADR-0015）"
check "更新元数据同样要口令" 401 "$(code "$BASE/v1/app/release")"
check "更新包同样要口令" 401 "$(code "$BASE/v1/app/release/file")"
check "空目录 = 404，不是 500" 404 "$(code -H "X-Access-Key: $KEY" "$BASE/v1/app/release")"
has "404 带 not_released 码（客户端据此静默）" "not_released" "$(get "/v1/app/release")"
check "没包的时候下载也是 404" 404 "$(code -H "X-Access-Key: $KEY" "$BASE/v1/app/release/file")"

say "更新通道：发一版"
printf '%s' '{"version_code":99,"version_name":"9.9","notes":"冒烟版"}' >"$RELEASE/release.json"
"$PY" -c "import sys;open(sys.argv[1],'wb').write(b'MEMORA-SMOKE-APK'*64)" "$RELEASE/app.apk"
META=$(get "/v1/app/release")
check "version_code 原样带回，客户端只比这个数" 99 "$(printf '%s' "$META" | jqp "['version_code']")"
check "size 由服务端 stat 出来，不是发布时手填的" 1024 "$(printf '%s' "$META" | jqp "['size']")"
has "更新说明带回给客户端" "冒烟版" "$META"
check "拉到的包与服务端那个字节一致" \
  "$("$PY" -c "import hashlib,sys;print(hashlib.sha256(open(sys.argv[1],'rb').read()).hexdigest()[:12])" "$RELEASE/app.apk")" \
  "$(curl -sk -H "X-Access-Key: $KEY" "$BASE/v1/app/release/file" | "$PY" -c 'import hashlib,sys;print(hashlib.sha256(sys.stdin.buffer.read()).hexdigest()[:12])')"
check "Range 续传生效（断了不必重拉整包）" 206 "$(code -H "X-Access-Key: $KEY" -H "Range: bytes=0-9" "$BASE/v1/app/release/file")"
# 发了一半（json 在、包不在）属于部署事故，不能安静地当成"没发版"
mv "$RELEASE/app.apk" "$RELEASE/app.apk.off"
check "只有 release.json = 500 release_bad" 500 "$(code -H "X-Access-Key: $KEY" "$BASE/v1/app/release")"
mv "$RELEASE/app.apk.off" "$RELEASE/app.apk"

say "限速：连续失败到阈值后，口令对了也不给过"
for _ in 1 2 3 4 5; do code -H 'X-Access-Key: 故意错' "$BASE/v1/notes" >/dev/null; done
check "第 6 次仍错 = 429" 429 "$(code -H 'X-Access-Key: 故意错' "$BASE/v1/notes")"
HEAD=$(curl -sk -D - -o /dev/null -H "X-Access-Key: $KEY" "$BASE/v1/notes" | tr -d '\r' | tr 'A-Z' 'a-z')
# 状态行按协议版本写死会挑环境：Windows 那份 curl 没编 HTTP/2，回的是 http/1.1。
# 这条要钉的是"锁定期间口令对也吃 429"，与协商到哪个版本无关。
has "锁定期间口令对也吃 429" " 429" "$HEAD"
has "带上 Retry-After" "retry-after" "$HEAD"
check "healthz 不受限速影响" 200 "$(code "$BASE/healthz")"

printf '\n== 结果：%d 项通过，%d 项失败\n' "$pass" "$fail"
[[ $fail -eq 0 ]]
