#!/usr/bin/env python3
"""假模型端点：只用于冒烟测试，不联网、不要真 key。

形状对齐 DeepSeek/OpenAI 的对话补全接口，冒烟要钉的四件事都在这里：
  1. 要求固定 Bearer key —— 服务端漏传或传错，这里就回 401，报表应落 failed；
  2. 响应延迟 1 秒 —— 让 generated_at 和"发起时刻"能分辨开；
  3. 正文里回显收到的提示词行数 —— 用来断言周期范围确实传对了；
  4. 把每次收到的请求打到 stdout —— 补跑判据"不该重复生成"靠数它验证。
"""
import json
import os
import sys
import time
from http.server import BaseHTTPRequestHandler, HTTPServer

KEY = os.environ.get("STUB_LLM_KEY", "smoke-stub-key-ascii-only")  # 必须 ASCII：HTTP 头按 latin-1 解码
DELAY = float(os.environ.get("STUB_LLM_DELAY", "1"))
HITS_PATH = os.environ.get("STUB_LLM_HITS", "")


class Handler(BaseHTTPRequestHandler):
    def do_POST(self):
        n = int(self.headers.get("Content-Length", 0))
        raw = self.rfile.read(n)
        try:
            req = json.loads(raw)
        except json.JSONDecodeError:
            self.reply(400, {"error": {"message": "不是 JSON"}})
            return

        if self.path != "/v1/chat/completions" or not req.get("messages"):
            self.reply(404, {"error": {"message": "只认 /v1/chat/completions"}})
            return

        if HITS_PATH:
            with open(HITS_PATH, "a", encoding="utf-8") as f:
                f.write(json.dumps(req, ensure_ascii=False) + "\n")

        if self.headers.get("Authorization") != "Bearer " + KEY:
            self.reply(401, {"error": {"message": "key 不对", "type": "invalid_request_error"}})
            return

        time.sleep(DELAY)
        提示 = req["messages"][0]["content"]
        记录 = req["messages"][1]["content"]
        行数 = sum(1 for line in 记录.splitlines() if line.startswith("["))
        text = (
            "本周期完成\n"
            f"- 冒烟：收到 {行数} 条记录\n"
            "- 提示词规则首行：" + 提示.splitlines()[0]
        )
        self.reply(200, {
            "choices": [{"message": {"role": "assistant", "content": text}}],
            "usage": {"total_tokens": 行数 * 10},
        })

    def reply(self, code, obj):
        body = json.dumps(obj, ensure_ascii=False).encode()
        self.send_response(code)
        self.send_header("Content-Type", "application/json; charset=utf-8")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def log_message(self, fmt, *args):
        sys.stderr.write("stub-llm: " + fmt % args + "\n")


if __name__ == "__main__":
    addr = sys.argv[1] if len(sys.argv) > 1 else "127.0.0.1:18480"
    host, port = addr.rsplit(":", 1)
    srv = HTTPServer((host, int(port)), Handler)
    sys.stderr.write(f"stub-llm 监听 {addr}，key 长度 {len(KEY)}\n")
    srv.serve_forever()
