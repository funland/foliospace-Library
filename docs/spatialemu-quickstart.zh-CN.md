# SpatialEMU + FolioSpace 新手快速上手

三款产品介绍与下载见 [Library 中文介绍](../README.zh-CN.md)，其他客户端欢迎按 [API 接入摘要](api/client-v1.zh-CN.md) 集成。

[English](spatialemu-quickstart.md)

FolioSpace Library 是 SpatialEMU 的可选自托管目录。即使没有 FolioSpace，SpatialEMU 也可以直接打开支持的本地文件。

你不需要 NAS，也不需要写代码。你需要准备：

- Mac 或 Windows 电脑上的 Docker Desktop、Linux 上带 Compose 的 Docker Engine，或支持 Docker/Compose 的 AMD64/ARM64 NAS；
- 一个或多个存放自有且有权使用文件的目录；
- FolioSpace 服务 URL；
- 首次设置时由你创建的访问 token。

SpatialEMU macOS 应用是客户端，不是 FolioSpace 服务端。FolioSpace Library 目前通过已发布的 Linux Docker 镜像运行；目前没有独立的原生 macOS FolioSpace 服务端应用。

## 1. 准备 Compose 目录

下载或克隆本仓库，然后在包含 [`docker-compose.yml`](../docker-compose.yml) 和 [`.env.example`](../.env.example) 的目录中打开终端。

macOS 或 Linux：

```sh
cp .env.example .env
mkdir -p data/config data/library data/books data/games data/videos
```

Windows PowerShell：

```powershell
Copy-Item .env.example .env
New-Item -ItemType Directory -Force data/config, data/library, data/books, data/games, data/videos
```

默认目录足够完成第一次测试。把自有游戏文件放入 `data/games`，书籍放入 `data/books`，其他媒体依此类推。如果需要直接使用其他位置的现有目录，请用文本编辑器修改 `.env` 中对应的 `FOLIOSPACE_*_PATH`。

新手路径请把 `FOLIOSPACE_API_TOKEN` 留空，由 Web 设置页创建第一个 token。`/config` 挂载必须可写并持久保存；媒体目录以只读方式挂载。

## 2. 启动 FolioSpace Library

```sh
docker compose up -d
docker compose ps
```

默认主机端口是 `8080`。请打开：

- 浏览器与 Docker 在同一台电脑上时：`http://localhost:8080`；
- 同一可信内网中的其他设备：`http://<主机内网-IP>:8080`。

## 3. 完成首次设置

在 FolioSpace Web 页面中：

1. 创建至少 8 个字符的访问 token，并妥善保存。
2. 选择 `/games`、`/books`、`/library` 或 `/videos` 等容器内路径。
3. 为目录命名并选择媒体类型。
4. 完成设置。第一次连接时，高级游戏目录选项保持默认即可。

Web 页面使用的是容器内路径。如果没有看到宿主机媒体目录，请修改 `.env` 中的绑定路径，再运行 `docker compose up -d` 重新创建容器。

## 4. 连接 SpatialEMU

如果只想打开本地文件，请使用 SpatialEMU 普通的导入/打开流程，无需继续配置 FolioSpace。

如需使用自托管目录：

1. 打开 SpatialEMU 的 FolioSpace 连接界面。
2. 输入完整 FolioSpace URL，包括 `http://` 或 `https://`，以及需要的端口。
3. 输入在 FolioSpace Web 首次设置中创建的同一个访问 token。
4. 选择**连接**，确认预期目录条目能够加载后，再打开文件。

两个官网入口用途不同：

- [SpatialEMU 连接设置指南](https://spatialemu.com/zh-cn/guides/foliospace-connection/)——直接连接步骤。
- [了解 FolioSpace](https://spatialemu.com/zh-cn/foliospace/)——产品用途与责任边界。

## 5. 安全排障

```sh
docker compose logs -f foliospace-library
```

- 浏览器无法打开 FolioSpace 时，确认 `docker compose ps` 显示服务正在运行，并确认客户端设备能够访问 Docker 主机。
- 媒体目录缺失时，检查 `.env` 中对应的宿主机路径，再运行 `docker compose up -d`。
- SpatialEMU 连接失败时，核对完整 URL，并重新输入同一个 token。不要在截图或支持消息中公开私有地址或 token。
- 不要把 `8080` 端口直接暴露在公网。优先使用可信内网、Tailscale 等 VPN，或正确配置的 HTTPS 反向代理。

FolioSpace 和 SpatialEMU 都不包含、不提供、也不下载游戏、ROM、BIOS 文件、固件或第三方内容目录。用户必须自行提供有权使用的文件。
