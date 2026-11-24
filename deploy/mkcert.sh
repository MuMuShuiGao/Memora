#!/usr/bin/env bash
# 生成自签证书（ADR-0009）。服务端只做 HTTPS，证书公钥要打进 APK 做校验。
#
# 用法：
#   MEMORA_PUBLIC_IP=你的公网IP ./deploy/mkcert.sh /etc/memora/tls
#
# 地址从环境变量来，绝不写进仓库——写进去就等于把机器标识记进文档（writing-rules §9）。
set -euo pipefail

OUT_DIR="${1:-/etc/memora/tls}"
IP="${MEMORA_PUBLIC_IP:?需要 MEMORA_PUBLIC_IP，例如 MEMORA_PUBLIC_IP=1.2.3.4 $0}"
DAYS="${MEMORA_CERT_DAYS:-730}"

mkdir -p "$OUT_DIR"
umask 077

openssl ecparam -name prime256v1 -genkey -noout -out "$OUT_DIR/server.key"
openssl req -x509 -new -sha256 -days "$DAYS" \
  -key "$OUT_DIR/server.key" \
  -out "$OUT_DIR/server.crt" \
  -subj "/CN=memora" \
  -addext "subjectAltName=IP:${IP}" \
  -addext "basicConstraints=critical,CA:TRUE" \
  -addext "keyUsage=critical,keyCertSign,digitalSignature" \
  -addext "extendedKeyUsage=serverAuth"

chmod 600 "$OUT_DIR/server.key"
chmod 644 "$OUT_DIR/server.crt"

echo "已生成：$OUT_DIR/server.crt  $OUT_DIR/server.key"
echo "有效期 ${DAYS} 天，SAN=IP:${IP}"
echo "把 server.crt 拷进安卓工程的网络安全配置里做证书锁定；私钥不要离开这台机器。"
