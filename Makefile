.PHONY: build run test clean install-deps lint help

# 变量
BINARY_NAME=server
BINARY_DIR=bin
CMD_DIR=cmd/server
CONFIG_FILE=configs/config.yaml

# Go 相关
GOCMD=go
GOBUILD=$(GOCMD) build
GORUN=$(GOCMD) run
GOTEST=$(GOCMD) test
GOGET=$(GOCMD) get
GOMOD=$(GOCMD) mod
GOVET=$(GOCMD) vet

# 默认目标
all: build

## build: 编译项目
build:
	@echo "🔨 Building..."
	@mkdir -p $(BINARY_DIR)
	$(GOBUILD) -o $(BINARY_DIR)/$(BINARY_NAME) ./$(CMD_DIR)
	@echo "✅ Build complete: $(BINARY_DIR)/$(BINARY_NAME)"

## run: 运行项目
run: build
	@echo "🚀 Running..."
	./$(BINARY_DIR)/$(BINARY_NAME) -config $(CONFIG_FILE)

## test: 运行测试
test:
	@echo "🧪 Running tests..."
	$(GOTEST) -v ./...

## lint: 代码检查
lint:
	@echo "🔍 Running linters..."
	$(GOVET) ./...
	@if command -v golint > /dev/null; then golint ./...; fi

## clean: 清理构建产物
clean:
	@echo "🧹 Cleaning..."
	@rm -rf $(BINARY_DIR)
	@rm -rf temp
	@rm -rf recordings
	@rm -rf hls_output
	@echo "✅ Clean complete"

## deps: 安装依赖
deps:
	@echo "📦 Installing dependencies..."
	$(GOMOD) tidy
	$(GOMOD) download
	@echo "✅ Dependencies installed"

## build-linux: 交叉编译 Linux 版本
build-linux:
	@echo "🔨 Building for Linux..."
	@mkdir -p $(BINARY_DIR)
	GOOS=linux GOARCH=amd64 $(GOBUILD) -o $(BINARY_DIR)/$(BINARY_NAME)-linux-amd64 ./$(CMD_DIR)
	GOOS=linux GOARCH=arm64 $(GOBUILD) -o $(BINARY_DIR)/$(BINARY_NAME)-linux-arm64 ./$(CMD_DIR)
	GOOS=linux GOARCH=arm GOARM=7 $(GOBUILD) -o $(BINARY_DIR)/$(BINARY_NAME)-linux-armv7 ./$(CMD_DIR)
	@echo "✅ Linux builds complete"

## build-darwin: 交叉编译 macOS 版本
build-darwin:
	@echo "🔨 Building for macOS..."
	@mkdir -p $(BINARY_DIR)
	GOOS=darwin GOARCH=amd64 $(GOBUILD) -o $(BINARY_DIR)/$(BINARY_NAME)-darwin-amd64 ./$(CMD_DIR)
	GOOS=darwin GOARCH=arm64 $(GOBUILD) -o $(BINARY_DIR)/$(BINARY_NAME)-darwin-arm64 ./$(CMD_DIR)
	@echo "✅ macOS builds complete"

## build-windows: 交叉编译 Windows 版本
build-windows:
	@echo "🔨 Building for Windows..."
	@mkdir -p $(BINARY_DIR)
	GOOS=windows GOARCH=amd64 $(GOBUILD) -o $(BINARY_DIR)/$(BINARY_NAME)-windows-amd64.exe ./$(CMD_DIR)
	@echo "✅ Windows build complete"

## build-all: 编译所有平台
build-all: build-linux build-darwin build-windows
	@echo "✅ All platforms built"

## docker-build: 构建 Docker 镜像
docker-build:
	@echo "🐳 Building Docker image..."
	docker build -t home-monitor:latest .
	@echo "✅ Docker image built"

## docker-run: 运行 Docker 容器
docker-run:
	@echo "🐳 Running Docker container..."
	docker run -d --name home-monitor \
		-p 8080:8080 -p 8081:8081 -p 8082:8082 \
		-v $(PWD)/recordings:/app/recordings \
		-v $(PWD)/configs:/app/configs \
		--device /dev/video0:/dev/video0 \
		home-monitor:latest

## help: 显示帮助信息
help:
	@echo "Home Monitor - 家庭监控系统"
	@echo ""
	@echo "使用方法:"
	@echo "  make <target>"
	@echo ""
	@echo "目标:"
	@sed -n 's/^##//p' $(MAKEFILE_LIST) | column -t -s ':' | sed -e 's/^/  /'
