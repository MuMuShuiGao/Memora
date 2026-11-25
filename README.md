# Memora
memo+memory

给一个人用的安卓备忘录：随手记纯文字想法，由大模型按自然周/自然月汇总成能直接交出去的周报和月报。

- 设计与决策：先读 [docs/INDEX.md](docs/INDEX.md)，进度断点见 [docs/NOW.md](docs/NOW.md)
- 服务端：`make build`（Linux）或 `make windows`（出 `memora.exe`），配置照 [deploy/memora.env.example](deploy/memora.env.example) 抄，证书用 `deploy/mkcert.sh` 签
- 安卓端：[android/README.md](android/README.md)——两个构建变体（`local` 连模拟器里的本机服务，`server` 连公网），装之前要把对应环境的证书拷进 `res/raw/`
- 验证：`make vet && make test && make smoke`，冒烟全程走本机回环，不需要公网也不需要模型 key
