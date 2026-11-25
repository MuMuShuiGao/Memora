#!/usr/bin/env bash
# 生成自签证书（ADR-0009）。服务端只做 HTTPS，证书公钥要打进 APK 做校验。
#
# 用法：
#   MEMORA_PUBLIC_IP=<地址> ./deploy/mkcert.sh <输出目录>
#   MEMORA_PUBLIC_IP=<地址1,地址2> ./deploy/mkcert.sh <输出目录>   # 多地址（本机测试用）
#
# 地址从环境变量来，绝不写进仓库——写进去就等于把机器标识记进文档（writing-rules §9）。
# 证书要对上客户端实际连的那个地址：自签证书的 SAN 里没有它，TLS 握手就失败。
set -euo pipefail

OUT_DIR="${1:-/etc/memora/tls}"
DAYS="${MEMORA_CERT_DAYS:-730}"

# 逗号分隔的每一项：带前缀（IP: / DNS:）照原样用，不带就按 IP 处理。
SAN=''
IFS=',' read -ra ITEMS <<< "${MEMORA_PUBLIC_IP:?需要 MEMORA_PUBLIC_IP，值可以是逗号分隔的多个地址}"
for item in "${ITEMS[@]}"; do
  item="${item//[[:space:]]/}"
  [[ -z "$item" ]] && continue
  if [[ "$item" == *:* ]]; then
    entry="$item"
  else
    entry="IP:${item}"
  fi
  SAN="${SAN:+$SAN,}${entry}"
done
[[ -n "$SAN" ]] || { echo "MEMORA_PUBLIC_IP 里一个地址都没有" >&2; exit 1; }

mkdir -p "$OUT_DIR"
umask 077

openssl ecparam -name prime256v1 -genkey -noout -out "$OUT_DIR/server.key"
openssl req -x509 -new -sha256 -days "$DAYS" \
  -key "$OUT_DIR/server.key" \
  -out "$OUT_DIR/server.crt" \
  -subj "/CN=memora" \
  -addext "subjectAltName=${SAN}" \
  -addext "basicConstraints=critical,CA:TRUE" \
  -addext "keyUsage=critical,keyCertSign,digitalSignature" \
  -addext "extendedKeyUsage=serverAuth"

chmod 600 "$OUT_DIR/server.key"
chmod 644 "$OUT_DIR/server.crt"

echo "已生成：$OUT_DIR/server.crt  $OUT_DIR/server.key"
echo "有效期 ${DAYS} 天，SAN=${SAN}"
echo "把 server.crt 拷进安卓工程的网络安全配置里做证书锁定；私钥不要离开这台机器。"
