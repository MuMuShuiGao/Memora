# ADR-0006：一个二进制、systemd 起停。构建动作收在这里，不让人各自记命令。

GO      ?= go
BINARY  ?= memora
LDFLAGS := -s -w

.PHONY: help build vet test smoke fmt tidy run clean

help:
	@echo "make build   编译静态二进制（CGO 关，纯 Go 驱动）"
	@echo "make vet     go vet + gofmt 检查"
	@echo "make test    全部单测，含 -race"
	@echo "make smoke   本机端到端冒烟：真 HTTPS + 假模型 + 限速 + 补跑"
	@echo "make clean   删产物"

build:
	CGO_ENABLED=0 $(GO) build -ldflags '$(LDFLAGS)' -o $(BINARY) ./cmd/memora

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
	rm -f $(BINARY)
