@echo off
REM 家庭监控系统 Windows 构建脚本

echo 🏠 家庭监控系统构建脚本
echo.

REM 检查 Go 是否安装
where go >nul 2>nul
if %ERRORLEVEL% neq 0 (
    echo 错误: Go 未安装，请先安装 Go 1.21 或更高版本
    exit /b 1
)

REM 检查 FFmpeg 是否安装
where ffmpeg >nul 2>nul
if %ERRORLEVEL% neq 0 (
    echo 警告: FFmpeg 未安装，视频功能将无法使用
    echo 请从 https://ffmpeg.org/download.html 下载并安装 FFmpeg
    echo.
)

REM 安装依赖
echo 📦 安装依赖...
go mod tidy

REM 创建输出目录
if not exist build mkdir build

REM 构建
echo 🔨 构建中...
go build -ldflags "-s -w" -o build\home-monitor.exe cmd\server\main.go

if %ERRORLEVEL% equ 0 (
    echo ✅ 构建完成: build\home-monitor.exe
) else (
    echo ❌ 构建失败
    exit /b 1
)

REM 复制配置文件
echo 📄 复制配置文件...
xcopy /E /I /Y configs build\configs
xcopy /E /I /Y web build\web

echo.
echo 🎉 构建完成！
echo.
echo 运行方式:
echo   cd build
echo   home-monitor.exe
echo.
echo 或指定配置文件:
echo   home-monitor.exe -config configs\config.yaml

pause
