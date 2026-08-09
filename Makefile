# ============================================================
# bilitoolgo Makefile
# 用法:
#   make build     # 当前平台构建
#   make build-all # 交叉编译 linux/darwin/windows × amd64/arm64
#   make test      # 运行单元测试
#   make lint      # golangci-lint 代码检查（未安装时跳过）
#   make clean     # 清理构建产物
# ============================================================

BINARY   := bilipro
BUILD_DIR := build
LDFLAGS  := -ldflags="-s -w"
GO       := go

.PHONY: build build-all test lint clean

build:
	$(GO) build $(LDFLAGS) -o $(BINARY) ./main.go

# 交叉编译：linux/darwin/windows × amd64/arm64
build-all:
	@mkdir -p $(BUILD_DIR)
	@set -e; \
	for os in linux darwin windows; do \
		for arch in amd64 arm64; do \
			ext=""; \
			if [ "$$os" = "windows" ]; then ext=".exe"; fi; \
			out="$(BUILD_DIR)/$(BINARY)-$$os-$$arch$$ext"; \
			echo "==> building $$out"; \
			CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch $(GO) build $(LDFLAGS) -o "$$out" ./main.go; \
		done; \
	done
	@echo "build-all done -> $(BUILD_DIR)/"

test:
	$(GO) test ./...

lint:
	@command -v golangci-lint >/dev/null 2>&1 || { echo "golangci-lint not found, skipping"; exit 0; }
	golangci-lint run ./...

clean:
	rm -rf $(BUILD_DIR) $(BINARY)
