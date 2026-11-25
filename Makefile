# ADR-0006：一个二进制、systemd 起停。构建动作收在这里，不让人各自记命令。

GO      ?= go
BINARY  ?= memora
LDFLAGS := -s -w

.PHONY: help build windows vet test smoke fmt tidy run clean

help:
	@echo "make build    编译静态二进制（CGO 关，纯 Go 驱动）"
	@echo "make windows  交叉编译 memora.exe，给 Windows 上的模拟器用"
	@echo "make vet      go vet + gofmt 检查"
	@echo "make test     全部单测，含 -race"
	@echo "make smoke    本机端到端冒烟：真 HTTPS + 假模型 + 限速 + 补跑"
	@echo "make clean    删产物"

build:
	CGO_ENABLED=0 $(GO) build -ldflags '$(LDFLAGS)' -o $(BINARY) ./cmd/memora

# 安卓模拟器只能走 10.0.2.2 连宿主机，所以测试回路是"Windows 上跑服务端 + 模拟器跑客户端"。
# 交叉编译在这台机器上做，Windows 那边不用装 Go 工具链。
windows:
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 $(GO) build -ldflags '$(LDFLAGS)' -o memora.exe ./cmd/memora

vet:
	$(GO) vet ./...
	@test -z "$$(gofmt -l .)" || { echo "gofmt 未通过："; gofmt -l .; exit 1; }

test:
	$(GO) test -race ./...

smoke: build
	bash scripts/smoke.sh

fmt:
	gofmt -w .

tidy:
	$(GO) mod tidy

run: build
	./$(BINARY) serve -config ./memora.env

clean:
	rm -f $(BINARY) memora.exe
