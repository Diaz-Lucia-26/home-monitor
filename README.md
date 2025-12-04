# 🏠 Home Monitor - 家庭监控系统

[![Go Version](https://img.shields.io/badge/Go-1.21+-00ADD8?style=flat&logo=go)](https://go.dev/)
[![License](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Build Status](https://github.com/your-username/home-monitor/actions/workflows/build.yml/badge.svg)](https://github.com/your-username/home-monitor/actions)
[![Release](https://img.shields.io/github/v/release/your-username/home-monitor?include_prereleases)](https://github.com/your-username/home-monitor/releases)

一个功能丰富的家庭监控服务，支持树莓派、Linux、Windows、macOS 系统，提供实时视频预览、录像存储、直播推流等功能。

<p align="center">
  <img src="docs/screenshot.png" alt="Home Monitor Screenshot" width="800">
</p>

## ✨ 功能特性

### 🎥 多源视频输入
- **USB 摄像头** - 本地 USB 摄像头采集
- **RTSP 流** - 支持网络摄像头 RTSP 流
- **HLS/m3u8 流** - 支持 HLS 网络流输入（如电视台直播）

### 📺 多协议实时预览
- **MJPEG** - 兼容性最好，所有浏览器支持（独立端口 8081）
- **WebRTC** - 超低延迟 P2P 传输，支持音视频（独立端口 8082）
- **WebSocket** - 实时帧推送

### 📡 直播推流
- **RTMP 推流** - 推送到 B站、抖音、YouTube 等直播平台
- **HLS 输出** - 生成 HLS 流供外部播放器访问

### 💾 录像存储
- **自动分段录像** - 支持自定义时长（如 30m, 1h, 1d）
- **自动清理** - 按保留天数自动删除过期录像
- **音视频同步** - 支持音频录制

### 📊 性能监控
- **Go 进程监控** - 内存、Goroutines、GC 状态
- **FFmpeg 进程监控** - 子进程 CPU/内存使用
- **磁盘使用监控** - 录像目录空间监控
- **实时图表** - 历史趋势可视化
- **告警系统** - 内存/协程数阈值告警

### 🎨 Web 管理界面
- 现代化暗色主题设计
- 响应式布局，支持移动端
- 实时状态显示

## 📦 系统要求

- **Go** 1.21+
- **FFmpeg** 4.0+（用于视频采集和编码）

### FFmpeg 安装

```bash
# macOS
brew install ffmpeg

# Ubuntu/Debian
sudo apt update && sudo apt install ffmpeg

# Windows
# 从 https://ffmpeg.org/download.html 下载，添加到 PATH
```

## 🚀 快速开始

### 1. 克隆项目

```bash
git clone <repository-url>
cd home-monitor
```

### 2. 安装依赖

```bash
go mod tidy
```

### 3. 配置摄像头

编辑 `configs/config.yaml`：

```yaml
cameras:
  - id: "cam1"
    name: "客厅摄像头"
    type: "usb"           # usb, rtsp, hls
    device_index: 0       # USB 设备索引
    width: 1280
    height: 720
    fps: 30
    enabled: true
    audio:
      enabled: true
      type: "avfoundation"  # macOS: avfoundation, Linux: alsa/pulse
      device_index: 0
```

### 4. 编译运行

```bash
# 编译
go build -o bin/server ./cmd/server

# 运行
./bin/server -config configs/config.yaml
```

### 5. 访问界面

| 服务 | 地址 | 说明 |
|------|------|------|
| 主控制台 | http://localhost:8080 | 管理后台 |
| MJPEG 预览 | http://localhost:8081 | MJPEG 流服务 |
| WebRTC 预览 | http://localhost:8082 | WebRTC 流服务 |

## 📖 配置说明

### 服务器配置

```yaml
server:
  host: "0.0.0.0"
  port: 8080

preview:
  mjpeg:
    enabled: true
    port: 8081
    quality: 5          # JPEG 质量 1-31
  webrtc:
    enabled: true
    port: 8082
    stun_servers:
      - "stun:stun.l.google.com:19302"
```

### 摄像头配置

```yaml
cameras:
  # USB 摄像头
  - id: "cam1"
    name: "本地摄像头"
    type: "usb"
    device_index: 0
    width: 1280
    height: 720
    fps: 30
    enabled: true
    audio:
      enabled: true
      type: "avfoundation"  # macOS
      device_index: 0

  # RTSP 网络摄像头
  - id: "ipcam"
    name: "网络摄像头"
    type: "rtsp"
    rtsp_url: "rtsp://192.168.1.100:554/stream"
    enabled: true

  # HLS 流（如电视台直播）
  - id: "tv"
    name: "电视直播"
    type: "hls"
    hls_url: "http://example.com/live/playlist.m3u8"
    enabled: true
```

### 存储配置

```yaml
storage:
  enabled: true
  path: "./recordings"
  segment_duration: "30m"   # 支持: 300, "5m", "1h", "1h30m", "1d"
  retention_days: 7
  format: "mp4"
```

## 🔌 API 接口

### 摄像头

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/cameras` | 获取所有摄像头 |
| GET | `/api/cameras/:id` | 获取摄像头详情 |
| GET | `/api/cameras/:id/snapshot` | 获取快照 |

### 视频流

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/stream/:id/mjpeg` | MJPEG 视频流 |
| GET | `/api/stream/:id/ws` | WebSocket 视频流 |

### WebRTC

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | `/api/webrtc/offer` | 发送 SDP Offer |
| POST | `/api/webrtc/ice-candidate` | 发送 ICE Candidate |
| DELETE | `/api/webrtc/connection/:id` | 关闭连接 |
| GET | `/api/webrtc/status` | 获取状态 |

### RTMP 推流

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | `/api/rtmp/:camera_id/start` | 开始推流 |
| POST | `/api/rtmp/:camera_id/stop` | 停止推流 |
| GET | `/api/rtmp/:camera_id/status` | 推流状态 |
| GET | `/api/rtmp/streams` | 所有推流状态 |

### HLS 输出

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | `/api/hls/:camera_id/start` | 开始 HLS 输出 |
| POST | `/api/hls/:camera_id/stop` | 停止 HLS 输出 |
| GET | `/api/hls/:camera_id/status` | HLS 状态 |
| GET | `/api/hls/status` | 所有 HLS 状态 |
| GET | `/hls/:camera_id/index.m3u8` | HLS 播放地址 |

### 录像管理

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/recordings` | 获取录像列表 |
| GET | `/api/recordings/:camera_id/:filename` | 播放录像 |
| GET | `/api/recordings/:camera_id/:filename/download` | 下载录像 |
| DELETE | `/api/recordings/:camera_id/:filename` | 删除录像 |

### 性能监控

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/monitor/metrics` | 当前指标 |
| GET | `/api/monitor/history` | 历史数据 |
| GET | `/api/monitor/alerts` | 告警列表 |
| POST | `/api/monitor/gc` | 强制 GC |
| GET | `/api/monitor/subprocesses` | FFmpeg 子进程 |
| GET | `/api/monitor/disk` | 磁盘使用 |

## 🌐 Web 页面

| 页面 | 路径 | 说明 |
|------|------|------|
| 主页 | `/` | 控制台首页 |
| MJPEG 预览 | `/mjpeg` (端口 8081) | MJPEG 实时预览 |
| WebRTC 预览 | `/webrtc` (端口 8082) | WebRTC 实时预览 |
| RTMP 管理 | `/rtmp` | RTMP 推流管理 |
| HLS 管理 | `/hls` | HLS 输出管理 |
| 性能监控 | `/monitor` | 系统性能监控 |

## 🍓 树莓派部署

### 编译

```bash
# 在树莓派上编译
go build -o bin/server ./cmd/server

# 或交叉编译
GOOS=linux GOARCH=arm64 go build -o bin/server ./cmd/server  # Pi 4 64位
GOOS=linux GOARCH=arm GOARM=7 go build -o bin/server ./cmd/server  # Pi 3/Zero
```

### 设置开机自启

```bash
sudo nano /etc/systemd/system/home-monitor.service
```

```ini
[Unit]
Description=Home Monitor Service
After=network.target

[Service]
Type=simple
User=pi
WorkingDirectory=/home/pi/home-monitor
ExecStart=/home/pi/home-monitor/bin/server -config configs/config.yaml
Restart=always
RestartSec=10

[Install]
WantedBy=multi-user.target
```

```bash
sudo systemctl daemon-reload
sudo systemctl enable home-monitor
sudo systemctl start home-monitor

# 查看日志
sudo journalctl -u home-monitor -f
```

## 🪟 Windows 部署

```powershell
# 编译
go build -o bin/server.exe ./cmd/server

# 运行
./bin/server.exe -config configs/config.yaml
```

使用 [NSSM](https://nssm.cc/) 注册为 Windows 服务：

```powershell
nssm install HomeMonitor C:\path\to\server.exe -config C:\path\to\config.yaml
nssm start HomeMonitor
```

## 🔧 故障排除

### 摄像头无法识别

```bash
# Linux - 检查设备
ls /dev/video*
sudo chmod 666 /dev/video0

# macOS - 列出设备
ffmpeg -f avfoundation -list_devices true -i ""
```

### 视频卡顿

1. 降低分辨率和帧率
2. 检查网络带宽
3. 检查 CPU 使用率（访问 /monitor）

### RTMP 推流失败

1. 检查 RTMP 服务器地址是否正确
2. 确认推流码是否有效
3. 检查网络连通性

## 📁 项目结构

```
home-monitor/
├── cmd/server/         # 主程序入口
├── configs/            # 配置文件
├── internal/
│   ├── capture/        # 音视频采集
│   ├── config/         # 配置解析
│   ├── handler/        # HTTP 处理器
│   ├── monitor/        # 性能监控
│   ├── rtmp/           # RTMP 推流
│   ├── storage/        # 录像存储
│   ├── stream/         # 流处理 (HLS/MJPEG)
│   └── webrtc/         # WebRTC 服务
├── web/
│   ├── static/         # 静态资源
│   └── templates/      # HTML 模板
├── recordings/         # 录像存储目录
└── temp/               # 临时文件目录
```

## 📄 许可证

本项目基于 MIT 许可证开源 - 查看 [LICENSE](LICENSE) 文件了解详情。

## 🤝 贡献

欢迎贡献代码！请查看 [CONTRIBUTING.md](CONTRIBUTING.md) 了解如何参与。

## ⭐ Star History

如果这个项目对你有帮助，请给它一个 Star ⭐

## 📞 联系方式

- 提交 [Issue](https://github.com/your-username/home-monitor/issues) 报告 Bug 或建议功能
- 参与 [Discussions](https://github.com/your-username/home-monitor/discussions) 进行讨论
