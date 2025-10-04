# 🚀 TCP 连接管理与监控服务器

一个基于 **Go 语言** 构建的高性能 TCP 通信服务器，并集成了优雅的 **本地 Web 可视化界面**，用于实时监控和管理客户端连接。

## ✨ 特性概览

- **🎯 TCP 服务端**: 稳健的 Goroutine 并发模型，可处理大量客户端连接。
- **🌐 Web 可视化仪表盘**: 通过本地网页实时查看连接统计、客户端列表和详细信息。
- **🔌 连接管理**: 支持查看、监控和主动断开指定客户端连接。
- **🤝 客户端就绪**: 专为与 [`chat-client`](https://github.com/little-heep/chat-client) 仓库的客户端配合使用而设计。
- **⚡ 高性能**: 利用 Go 的并发特性，即使在高连接数下也能保持低延迟。

## 🏗️ 系统架构

```
[ chat-client 1 ]  \
[ chat-client 2 ]  ---> [ Go TCP Server :12345 ] <---> [ Web Dashboard :8080 ]
[ chat-client ... ] /
```

## 🚦 快速开始

### 前提条件

- **Go 1.19+** 已安装并配置好环境。
- 准备使用配套的 [`chat-client`](https://github.com/你的用户名/chat-client) 客户端。

### 安装与运行

1.  **克隆仓库**
    ```bash
    git clone https://github.com/你的用户名/your-tcp-server.git
    cd your-tcp-server
    ```

2.  **下载依赖并运行**
    ```bash
    go mod tidy
    go run main.go
    ```

3.  **验证服务**
    当服务器成功启动后，您将在终端看到类似以下信息：
    ```
    🚀 TCP 服务器正在监听 [::]:12345
    🌐 Web 仪表盘已启动，请访问 http://localhost:8443
    ```

## 📖 使用方法

### 1. 启动 TCP 服务器与 Web 面板

按照上述 **“安装与运行”** 步骤启动服务。

### 2. 连接 chat-client

使用配套的 [`chat-client`](https://github.com/little-heep/chat-client) 应用程序连接到该服务器。

**在客户端配置中，请确保将服务器地址设置为：**
```
主机: localhost
端口: 12345
```

### 3. 访问 Web 仪表盘

在您的浏览器中打开： **`http://localhost:8443`**


## 🔮 未来规划

- [ ] 实现向特定客户端或全体客户端发送广播消息的功能。
- [ ] 记录详细的连接/断开日志。
- [ ] 提供简单的客户端认证机制。
- [ ] 将连接数据持久化到数据库。

## 🤝 贡献

我们欢迎任何形式的贡献！请随时提交 **Pull Request** 或创建 **Issue** 来报告错误、提出新功能建议。

1. Fork 本仓库
2. 创建您的特性分支 (`git checkout -b feature/AmazingFeature`)
3. 提交您的更改 (`git commit -m 'Add some AmazingFeature'`)
4. 推送到分支 (`git push origin feature/AmazingFeature`)
5. 开启一个 Pull Request


## 💬 获取帮助

如果您在使用中遇到任何问题：

1. 请先查阅 [`chat-client`](https://github.com/little-heep/chat-client) 仓库的文档，确保客户端配置正确。
2. 如果问题仍未解决，请创建一个新的 Issue 并详细描述问题。

---

**享受实时监控的乐趣吧！** 🎉
