.PHONY: all test lint build clean dev help

BIN_NAME := pkgapp
GO_MOD := github.com/fufuok/pkg

# 跟随 pkg 所需 Go 版本, 命令行可覆盖 GO_BIN; 本机专用路径不存在时使用 PATH.
GO_BIN := /usr/local/go127/bin/go
GO_BIN2 := go
LINT_BIN ?= golangci-lint

CI_BIN := ../bin/$(BIN_NAME)

CHECK_GO := $(shell test -x '$(GO_BIN)' && echo ok)
ifeq ($(CHECK_GO),)
	GO_BIN := $(GO_BIN2)
endif

# 禁止 Go 按 go.mod 自动下载/切换 toolchain; CI 与本机都必须使用显式 GO_BIN.
export GOTOOLCHAIN := local
# 忽略未入库的 go.work, 避免 CI/本机因本地 replace 走不同模块图.
export GOWORK := off
# 模块下载默认走国内镜像; 环境已设置 GOPROXY/GOSUMDB 时不覆盖.
GOPROXY ?= https://goproxy.cn,direct
GOSUMDB ?= sum.golang.google.cn
export GOPROXY
export GOSUMDB
# golangci-lint 会调用 PATH 上的 go; 把 GO_BIN 所在目录置前, 避免落到旧系统 go 再触发下载.
GO_BIN_DIR := $(dir $(shell command -v '$(GO_BIN)' 2>/dev/null || echo '$(GO_BIN)'))
GO_PATH := $(GO_BIN_DIR):$(PATH)
# golangci-lint --timeout 只取消内部 context, 杀不掉卡住的 go list 子进程.
# GNU timeout 按进程组发 SIGTERM/SIGKILL; Windows timeout.exe 语义不同, 不能用.
LINT_HARD_TIMEOUT :=
ifeq ($(shell uname -s 2>/dev/null),Linux)
	LINT_HARD_TIMEOUT := timeout --kill-after=30s 16m
endif
# 历史 CI 2 核 RSS 约 1.6GB; 4 核冷分析实测会顶到 2.5GB.
# 软限制必须高于峰值, 否则 GC 会把 staticcheck/unused 拖过时限.
export GOMAXPROCS ?= 4
export GOMEMLIMIT ?= 4GiB

GIT_TAG_RAW := $(shell git tag -l 'v[0-9]*' --sort=-v:refname 2>/dev/null | grep -E '^v[0-9]+\.[0-9]+\.[0-9]+$$' | head -n 1)
GIT_TAG := $(patsubst v%,%,$(GIT_TAG_RAW))
BUILD_TIME := $(shell date +%y%m%d%H%M%S)
GIT_COMMIT := $(shell git log --pretty=format:'%cd %h' -1 --date=iso 2>/dev/null || echo "nogit")
GO_VERSION := $(shell $(GO_BIN) version)
ifeq ($(strip $(GIT_TAG)),)
	VERSION := dev.$(BUILD_TIME)
else
	VERSION := $(GIT_TAG).$(BUILD_TIME)
endif
LDFLAGS = -X "$(GO_MOD)/config.Version=$(VERSION)" \
-X "$(GO_MOD)/config.GitCommit=$(GIT_COMMIT)" \
-X "$(GO_MOD)/config.GoVersion=$(GO_VERSION)"

all: build

test:
	$(GO_BIN) version
	$(GO_BIN) vet ./...
	$(GO_BIN) test -count=1 ./...

lint:
	$(GO_BIN) version
	$(GO_BIN) env GOTOOLCHAIN GOROOT GOPROXY GOSUMDB GOPATH GOMODCACHE GOWORK GOMAXPROCS GOMEMLIMIT
	$(LINT_BIN) version
	$(LINT_HARD_TIMEOUT) $(GO_BIN) mod download
	$(LINT_HARD_TIMEOUT) $(GO_BIN) list -mod=readonly ./...
	PATH='$(GO_PATH)' $(LINT_HARD_TIMEOUT) $(LINT_BIN) run -c ../.golangci.yml -v --timeout 15m --modules-download-mode=readonly --allow-parallel-runners ./...

build:
	$(GO_BIN) version
	CGO_ENABLED=1 $(GO_BIN) build -ldflags '$(LDFLAGS)' -o $(CI_BIN)

clean:
	rm -f $(CI_BIN)

dev:
	gofumpt -l -d -w . || exit 1
	$(GO_BIN) vet ./... || exit 1
	$(GO_BIN) test -count=1 ./... || exit 1
	PATH='$(GO_PATH)' $(LINT_HARD_TIMEOUT) $(LINT_BIN) run -c ../.golangci.yml -v --timeout 15m --modules-download-mode=readonly --allow-parallel-runners ./... || exit 1
	CGO_ENABLED=1 $(GO_BIN) build -ldflags '$(LDFLAGS)' -o $(CI_BIN) || exit 1
	sleep 3
	cd .. && ./bin/$(BIN_NAME) -debug -c etc/local-$(BIN_NAME).json

help:
	@echo make: compile packages and dependencies
	@echo make dev: format, vet, test, lint, build and start local config
	@echo make clean: remove object files
