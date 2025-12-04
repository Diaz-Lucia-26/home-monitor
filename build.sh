#!/bin/bash

# 家庭监控系统构建脚本

set -e

# 颜色输出
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

echo -e "${GREEN}🏠 家庭监控系统构建脚本${NC}"
echo ""

# 检查 Go 是否安装
if ! command -v go &> /dev/null; then
    echo -e "${RED}错误: Go 未安装，请先安装 Go 1.21 或更高版本${NC}"
    exit 1
fi

# 检查 FFmpeg 是否安装
if ! command -v ffmpeg &> /dev/null; then
    echo -e "${YELLOW}警告: FFmpeg 未安装，视频功能将无法使用${NC}"
    echo "请安装 FFmpeg:"
    echo "  - Linux: sudo apt install ffmpeg"
    echo "  - macOS: brew install ffmpeg"
    echo "  - Windows: 从 https://ffmpeg.org/download.html 下载"
    echo ""
fi

# 安装依赖
echo -e "${GREEN}📦 安装依赖...${NC}"
go mod tidy

# 创建输出目录
mkdir -p build

# 获取版本信息
VERSION=${VERSION:-"1.0.0"}
BUILD_TIME=$(date -u '+%Y-%m-%d %H:%M:%S')

# 构建参数
LDFLAGS="-s -w"

# 构建当前平台
echo -e "${GREEN}🔨 构建当前平台...${NC}"
go build -ldflags "$LDFLAGS" -o build/home-monitor ./cmd/server/main.go
echo -e "${GREEN}✅ 构建完成: build/home-monitor${NC}"

# 是否构建所有平台
if [ "$1" == "--all" ]; then
    echo ""
    echo -e "${GREEN}🔨 构建所有平台...${NC}"
    
    # Linux AMD64
    echo "  - Linux AMD64..."
    GOOS=linux GOARCH=amd64 go build -ldflags "$LDFLAGS" -o build/home-monitor-linux-amd64 ./cmd/server/main.go
    
    # Linux ARM64 (树莓派 4)
    echo "  - Linux ARM64 (树莓派 4)..."
    GOOS=linux GOARCH=arm64 go build -ldflags "$LDFLAGS" -o build/home-monitor-linux-arm64 ./cmd/server/main.go
    
    # Linux ARM (树莓派 3/Zero)
    echo "  - Linux ARM (树莓派 3/Zero)..."
    GOOS=linux GOARCH=arm GOARM=7 go build -ldflags "$LDFLAGS" -o build/home-monitor-linux-arm ./cmd/server/main.go
    
    # Windows AMD64
    echo "  - Windows AMD64..."
    GOOS=windows GOARCH=amd64 go build -ldflags "$LDFLAGS" -o build/home-monitor-windows-amd64.exe ./cmd/server/main.go
    
    # macOS AMD64
    echo "  - macOS AMD64..."
    GOOS=darwin GOARCH=amd64 go build -ldflags "$LDFLAGS" -o build/home-monitor-darwin-amd64 ./cmd/server/main.go
    
    # macOS ARM64 (Apple Silicon)
    echo "  - macOS ARM64 (Apple Silicon)..."
    GOOS=darwin GOARCH=arm64 go build -ldflags "$LDFLAGS" -o build/home-monitor-darwin-arm64 ./cmd/server/main.go
    
    echo -e "${GREEN}✅ 所有平台构建完成${NC}"
fi

# 复制配置文件
echo ""
echo -e "${GREEN}📄 复制配置文件...${NC}"
cp -r configs build/
cp -r web build/

echo ""
echo -e "${GREEN}🎉 构建完成！${NC}"
echo ""
echo "运行方式:"
echo "  cd build"
echo "  ./home-monitor"
echo ""
echo "或指定配置文件:"
echo "  ./home-monitor -config configs/config.yaml"
