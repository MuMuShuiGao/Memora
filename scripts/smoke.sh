#!/usr/bin/env bash
# 端到端冒烟：本机起 HTTPS 服务 + 假模型，把设计与代码里所有可验收的断言跑一遍。
# 不需要腾讯云安全组规则，也不需要真 API key（writing-rules §9：真值不进仓库）。
#
#   make smoke
set -uo pipefail

cd "$(dirname "$0")/.."
ROOT=$PWD
WORK=$(mktemp -d)
PORT=${SMOKE_PORT:-18443}
STUB_PORT=${STUB_PORT:-18480}
BASE=https://127.0.0.1:${PORT}
KEY='冒烟口令-勿复用'
STUB_KEY='smoke-stub-key-ascii-only'
ENV=$WORK/memora.env
HITS=$WORK/llm-hits.jsonl
BIN=$WORK/memora
SRV_PID=''
STUB_PID=''

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
jqp()    { python3 -c "import json,sys; d=json.load(sys.stdin); print(d$1)"; }
# 时间戳转 epoch 秒。Go 写的是纳秒，Python 3.10 的 fromisoformat 最多吃到微秒，先裁掉尾巴。
iso_ts() { python3 -c '
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

MEMORA_PUBLIC_IP=127.0.0.1 bash deploy/mkcert.sh "$WORK/tls" >"$WORK/mkcert.log" 2>&1
[[ -f "$WORK/tls/server.crt" && -f "$WORK/tls/server.key" ]] \
  || { echo "证书没生成：$(cat "$WORK/mkcert.log")"; exit 1; }
ok "自签 ECDSA 证书就绪（SAN=IP:127.0.0.1）"

# 口令走标准输入，不进 argv——argv 会出现在进程列表里。
HASH=$(printf '%s' "$KEY" | "$BIN" hashpass | head -1)
[[ "$HASH" == '$2'* ]] && ok "hashpass 产出 bcrypt 哈希" || { echo "hashpass 没吐出哈希: $HASH"; exit 1; }
"$BIN" hashpass </dev/null >/dev/null 2>&1 && bad "空输入应失败" "却成功了" || ok "空输入被拒绝"
printf '%s' "$(python3 -c "print('长'*100)")" | "$BIN" hashpass >/dev/null 2>&1 \
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
EOF

STUB_LLM_HITS="$HITS" python3 scripts/stub-llm.py "127.0.0.1:${STUB_PORT}" >"$WORK/stub.log" 2>&1 &
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

say "记录读写与归属"
TODAY=$(TZ=UTC-8 date +%F)   # 服务配的是 +08，这里换成同一套日历
OUT=$(curl -sk -H "X-Access-Key: $KEY" -d '{"body":"给张三写季度总结"}' -w $'\n%{http_code}' "$BASE/v1/notes")
check "新建 = 201" 201 "${OUT##*$'\n'}"
read -r ID WEEK MONTH <<<"$(printf '%s' "${OUT%$'\n'*}" | python3 -c 'import json,sys; n=json.load(sys.stdin); print(n["id"], n["week_start"], n["month_start"])')"
check "响应带回 id" 1 "$ID"
python3 - "$WEEK" "$TODAY" <<'EOF' || bad "归属周" "$WEEK / $TODAY"
import sys, datetime
week, today = (datetime.date.fromisoformat(x) for x in sys.argv[1:3])
assert week.weekday() == 0, "week_start 不是周一"
assert week <= today < week + datetime.timedelta(days=7), "今天不在这个周里"
EOF
ok "归属周 = $WEEK（周一，含今天 $TODAY）"
check "归属月 = 本月 1 号" "${TODAY%-*}-01" "$MONTH"

OUT=$(curl -sk -X PATCH -H "X-Access-Key: $KEY" -d '{"body":"给张三写半年总结"}' -w $'\n%{http_code}' "$BASE/v1/notes/$ID")
check "修改 = 200" 200 "${OUT##*$'\n'}"
has "改后的正文读得回" "半年" "${OUT%$'\n'*}"
has "改正文不搬动归属" "\"week_start\":\"$WEEK\"" "${OUT%$'\n'*}"

LONG=$(python3 -c "print('阿'*2001)")
EXACT=$(python3 -c "print('阿'*2000)")
check "2001 字 = 413（不静默截断）" 413 "$(code -X POST -H "X-Access-Key: $KEY" -d "{\"body\":\"$LONG\"}" "$BASE/v1/notes")"
check "恰好 2000 字 = 201" 201 "$(code -X POST -H "X-Access-Key: $KEY" -d "{\"body\":\"$EXACT\"}" "$BASE/v1/notes")"
check "空正文 = 400" 400 "$(code -X POST -H "X-Access-Key: $KEY" -d '{"body":"   "}' "$BASE/v1/notes")"
check "坏 JSON = 400" 400 "$(code -X POST -H "X-Access-Key: $KEY" -d '{' "$BASE/v1/notes")"
check "非整数 id = 400" 400 "$(code -H "X-Access-Key: $KEY" "$BASE/v1/notes/abc")"
check "不存在的 id = 404" 404 "$(code -H "X-Access-Key: $KEY" "$BASE/v1/notes/9999")"
check "搜索命中 = 1 条" 1 "$(curl -sk -H "X-Access-Key: $KEY" "$BASE/v1/notes?q=总结" | jqp "['count']")"
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
curl -sk -X POST -H "X-Access-Key: $KEY" -d '{"body":"本周把报表模块打通"}' "$BASE/v1/notes" >/dev/null
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
check "报表列表按周期倒序且含 status" ready "$(curl -sk -H "X-Access-Key: $KEY" "$BASE/v1/reports?kind=week" | python3 -c 'import json,sys; print(json.load(sys.stdin)["reports"][0]["status"])')"
check "带口令才有报表数据" 200 "$(code -H "X-Access-Key: $KEY" "$BASE/v1/reports")"

say "报表：参数与错误形状"
BADWEEK=$(python3 -c "import datetime;print((datetime.date.fromisoformat('$WEEK')+datetime.timedelta(days=1)).isoformat())")
H0=$(hits)
check "非周一的周报起始日 = 400 bad_period" 400 "$(code -X POST -H "X-Access-Key: $KEY" "$BASE/v1/reports/week/$BADWEEK/generate")"
check "非 1 号的月报起始日 = 400" 400 "$(code -H "X-Access-Key: $KEY" "$BASE/v1/reports/month/${TODAY%-*}-15")"
check "未知 kind = 400" 400 "$(code -H "X-Access-Key: $KEY" "$BASE/v1/reports/day/$WEEK")"
check "坏日期格式 = 400" 400 "$(code -H "X-Access-Key: $KEY" "$BASE/v1/reports/week/not-a-date")"
check "参数不合法时一次模型都没打" 0 "$(($(hits) - H0))"
check "空周期生成 = 409 no_records" 409 "$(code -X POST -H "X-Access-Key: $KEY" "$BASE/v1/reports/week/2020-01-06/generate")"
check "没生成过的周期 = 404" 404 "$(code -H "X-Access-Key: $KEY" "$BASE/v1/reports/week/2020-01-06")"
has "404 带 not_generated 码" "not_generated" "$(get "/v1/reports/week/2020-01-06")"

say "报表：已结束的周期——失败要留得下来（FR-05 不静默缺失）"
# 把那条记录搬进上一个完整周：归属列是插入时算好的，这里直接改库模拟"上一周记的事"，
# 好让补跑判据（周期已结束 + 没有 ready）在冒烟里有东西可判。
PREV=$(python3 -c "import datetime;print((datetime.date.fromisoformat('$WEEK')-datetime.timedelta(days=7)).isoformat())")
PREVMONTH=${PREV%-*}-01
# 直接插一条属于上一周的记录：归属列本该由服务端在插入那一刻算好，但冒烟要的是
# "一个已结束的周期"，而今天永远落在当前周里，等不到上一个周期自己结束。
stop_server
sqlite3 "$WORK/memora.db" "
DELETE FROM notes;
DELETE FROM reports;
INSERT INTO notes (id, body, created_at, updated_at, week_start, month_start)
VALUES (900, '上周留在库里的一条', '${PREV}T01:00:00.000Z', '${PREV}T01:00:00.000Z', '$PREV', '$PREVMONTH');"
start_server server-prev.log || exit 1
check "已结束周期的记录读得回" "$PREV" "$(get "/v1/notes/900" | jqp "['week_start']")"
check "它确实落在过去的一周" 1 "$(python3 -c "import datetime;print(int(datetime.date.fromisoformat('$PREV') < datetime.date.fromisoformat('$TODAY')))")"

: >"$HITS"
sed -i "s/^MEMORA_LLM_API_KEY=.*/MEMORA_LLM_API_KEY=故意写错的key/" "$ENV"
stop_server; start_server server-badkey.log || exit 1
# 起来的这一轮补跑也会撞同一个坏 key，先让它跑完，再把计数清零，
# 这样下面那 2 次调用只对应这一次手动生成。
sleep 4; : >"$HITS"
check "坏 key 下补跑也没把报表悄悄造出来" failed "$(get "/v1/reports/week/$PREV" | jqp "['status']")"
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
EXPECTED=$(python3 -c "
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

say "限速：连续失败到阈值后，口令对了也不给过"
for _ in 1 2 3 4 5; do code -H 'X-Access-Key: 故意错' "$BASE/v1/notes" >/dev/null; done
check "第 6 次仍错 = 429" 429 "$(code -H 'X-Access-Key: 故意错' "$BASE/v1/notes")"
HEAD=$(curl -sk -D - -o /dev/null -H "X-Access-Key: $KEY" "$BASE/v1/notes" | tr -d '\r' | tr 'A-Z' 'a-z')
has "锁定期间口令对也吃 429" "http/2 429" "$HEAD"
has "带上 Retry-After" "retry-after" "$HEAD"
check "healthz 不受限速影响" 200 "$(code "$BASE/healthz")"

printf '\n== 结果：%d 项通过，%d 项失败\n' "$pass" "$fail"
[[ $fail -eq 0 ]]
