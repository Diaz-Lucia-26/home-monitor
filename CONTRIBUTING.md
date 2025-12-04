# 贡献指南

感谢你对 Home Monitor 项目的关注！我们欢迎各种形式的贡献。

## 如何贡献

### 报告问题

如果你发现了 bug 或有功能建议，请：

1. 先搜索 [已有的 Issues](https://github.com/your-username/home-monitor/issues) 确认问题是否已被报告
2. 如果没有，创建一个新的 Issue，并提供：
   - 清晰的问题描述
   - 复现步骤
   - 预期行为 vs 实际行为
   - 系统环境（OS、Go 版本、FFmpeg 版本等）
   - 相关日志或截图

### 提交代码

1. **Fork 项目** - 点击右上角的 Fork 按钮
2. **克隆你的 Fork**
   ```bash
   git clone https://github.com/your-username/home-monitor.git
   cd home-monitor
   ```
3. **创建功能分支**
   ```bash
   git checkout -b feature/your-feature-name
   ```
4. **进行修改** - 编写代码并确保：
   - 代码风格符合 Go 规范
   - 添加必要的注释
   - 更新相关文档
5. **运行测试**
   ```bash
   go test ./...
   go vet ./...
   ```
6. **提交更改**
   ```bash
   git add .
   git commit -m "feat: 添加某某功能"
   ```
7. **推送到你的 Fork**
   ```bash
   git push origin feature/your-feature-name
   ```
8. **创建 Pull Request** - 在 GitHub 上创建 PR，描述你的更改

## 提交信息规范

我们使用 [Conventional Commits](https://www.conventionalcommits.org/) 规范：

- `feat:` 新功能
- `fix:` Bug 修复
- `docs:` 文档更新
- `style:` 代码格式调整（不影响功能）
- `refactor:` 代码重构
- `perf:` 性能优化
- `test:` 测试相关
- `chore:` 构建/工具相关

示例：
```
feat: 添加动态检测功能
fix: 修复 WebRTC 连接断开后未清理资源的问题
docs: 更新树莓派部署文档
```

## 代码风格

- 遵循 [Effective Go](https://golang.org/doc/effective_go)
- 使用 `gofmt` 格式化代码
- 使用 `golint` 和 `go vet` 检查代码
- 函数和类型添加注释（godoc 风格）
- 错误处理要完整，不要忽略错误

## 开发环境设置

```bash
# 安装依赖
go mod tidy

# 运行
go run ./cmd/server -config configs/config.yaml

# 构建
go build -o bin/server ./cmd/server

# 测试
go test ./...
```

## 项目结构

```
home-monitor/
├── cmd/server/         # 主程序入口
├── configs/            # 配置文件
├── internal/           # 内部包
│   ├── capture/        # 音视频采集
│   ├── config/         # 配置解析
│   ├── handler/        # HTTP 处理器
│   ├── monitor/        # 性能监控
│   ├── rtmp/           # RTMP 推流
│   ├── storage/        # 录像存储
│   ├── stream/         # 流处理
│   └── webrtc/         # WebRTC 服务
└── web/                # 前端资源
```

## 需要帮助？

- 查看 [README](README.md) 了解项目概况
- 在 [Discussions](https://github.com/your-username/home-monitor/discussions) 中提问
- 通过 Issue 寻求帮助

再次感谢你的贡献！🙏
