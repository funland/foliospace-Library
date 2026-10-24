# FolioSpace Library

FolioSpace Library is an optional self-hosted catalog for your own books, comics, PDFs, games and movies. It runs with Docker/Compose on a Mac, Windows or Linux computer, or a compatible NAS. **A NAS is not required, and you do not need to write code.** You do need Docker, persistent storage, media folders, a service URL and an access token.

Library connects [FolioSpace Reader](https://reader.foliospace.app/), [FolioLume Player](https://foliolume.app/) and [SpatialEMU](https://spatialemu.com/). Each client can also use supported local files or sources independently. Other clients are welcome through the [Client API](https://github.com/funland/foliospace-Library/blob/main/docs/api/client-v1.md#external-client-quick-start).

This is not a hosted cloud service, a native macOS server app or a third-party media catalog. No books, movies, ROMs, BIOS or firmware are bundled or supplied. Only index files you have the right to use. Book/game/video DTOs omit source paths; legacy collection and administration responses can still carry directory paths.

## 1.10: clearer libraries and more reliable reading

- Switch between **ebooks and comics**, filter EPUB/PDF, and browse nested comic collections without duplicate parent/child listings.
- Import Calibre titles, authors and covers; clean generated book/comic/video labels while preserving manual corrections.
- Move library administration into settings, use consistent video titles across home surfaces, and keep finished books out of Continue Reading.
- Restore EPUB arrow-key navigation, improve two-column width and progress restoration, and correct chapter labels, character images and footnote links.
- Preserve item identities, progress, favorites and manual metadata during rescans, collection ownership reconciliation and game analysis. Mixed libraries no longer trigger the legacy game analyzer accidentally.
- Label confirmed empty, zero-filled or structurally damaged book files; hide them from Recently Added while retaining the record, error reason and original path. Repaired files can recover without losing error history.

The server primarily indexes and delivers original files; dedicated clients decode them. Existing optional HLS is retained, not replaced by new always-on whole-library transcoding. Features require capability discovery and a compatible client build; this server release does not imply an App Store update or completed four-platform device acceptance.

**All previous reading and game features remain included:** EPUB/PDF/comic reading, progress and profiles, CIA installation delivery, MAME/FBNeo compatibility, PC-98 per-game fonts, Triforce routing, launch manifests and resumable Range downloads. See the [full 1.10 release notes](https://github.com/funland/foliospace-Library/releases/tag/v1.10) and [version history](https://github.com/funland/foliospace-Library/releases). Old release-specific policies are historical, not replacements for the current contract.

## Quick Start

Use the [Compose quick start](https://github.com/funland/foliospace-Library/blob/v1.10/COMPOSE-QUICKSTART.md) for configurable media folders and resource limits. The default web/API port is **8080**.

For a minimal new installation, replace the example host paths below with your own existing directories. Keep the config directory writable and media read-only:

```sh
docker pull funland/foliospace-library:1.10
docker run -d --name foliospace-library --restart unless-stopped \
  -p 8080:8080 \
  -v /absolute/path/to/config:/config \
  -v /absolute/path/to/media:/library:ro \
  funland/foliospace-library:1.10
```

Open `http://localhost:8080` on the Docker host, or `http://<host-address>:8080` from another device. Complete first-run setup to create your access token and select the mounted container path `/library`. In a compatible client, enter that service URL and the same token, then test the connection.

The SpatialEMU Mac app is a client, not this Docker server. [Beginner guide](https://github.com/funland/foliospace-Library/blob/main/docs/spatialemu-quickstart.md) · [简体中文](https://github.com/funland/foliospace-Library/blob/main/docs/spatialemu-quickstart.zh-CN.md) · [SpatialEMU connection guide](https://spatialemu.com/guides/foliospace-connection/).

## TMDB: bring your own token

TMDB matching is **disabled by default**. Apply through your own TMDB account and configure your own **API Read Access Token** on the server. We supply no shared developer key. This is not the shorter v3 API key or your FolioSpace connection token.

The 1.10 Compose template passes these optional settings from a private `.env`:

```dotenv
FOLIOSPACE_VIDEO_METADATA_PROVIDER=tmdb
FOLIOSPACE_TMDB_TOKEN=YOUR_OWN_TMDB_API_READ_ACCESS_TOKEN
FOLIOSPACE_TMDB_LANGUAGE=en-US
```

The token above is a placeholder. Keep real credentials out of Git, Docker images, URLs, logs, screenshots and shared Compose output. Use `docker compose config --quiet` for validation. Older custom Compose files need the environment mappings too; `.env` alone is not passed to a container.

**A personal credential or this project's software license does not automatically grant commercial permission.** Review TMDB's current terms, applicable commercial licensing and attribution requirements. The web About screen and video footer provide attribution; independent clients must address their own use.

[TMDB setup, privacy and official sources](https://github.com/funland/foliospace-Library/blob/v1.10/docs/tmdb-setup.md) · [中文配置说明](https://github.com/funland/foliospace-Library/blob/v1.10/docs/tmdb-setup.zh-CN.md).

TMDB supplies metadata/artwork, not movie files. Candidate confirmation, artwork retrieval and season refresh are explicit operations, not automatic whole-library scraping. Local NFO, artwork and manual metadata remain usable without it. MetaTube/DMM adapters and complex multi-episode file handling are not delivered in 1.10.

## Upgrading and persistent data

Tags `1.10` and `latest` support **Linux AMD64 and ARM64**. Pin a version for repeatable deployments.

Before upgrading, back up the persistent `/config` directory and preserve your existing access token, media mounts and custom environment. The metadata/playback tables are additive. Retain a prior image and stopped-state backup; do not restore an old database over new user progress without reconciliation. Keep `FOLIOSPACE_PRESERVE_GAME_CATALOG_ON_START=1` if your existing installation uses it.

Publishing or pulling an image does not replace an existing container automatically. After your deliberate upgrade, verify `/api/client/info`, your normal reading/game/video workflows and the original-file download path.

| Container path/setting | Purpose |
| --- | --- |
| `/config` | Writable SQLite database, metadata, thumbnails and runtime cache; persist and back up |
| `/library` | Default read-only media mount |
| `/books`, `/games`, `/videos` | Additional read-only mounts in the Compose template |
| `FOLIOSPACE_DIRECTORY_ROOTS` | Comma-separated container-visible media roots |
| `FOLIOSPACE_API_TOKEN` | Optional fixed token; otherwise first-run setup can save a token hash |
| `FOLIOSPACE_SCAN_WORKERS` | Scan concurrency; Compose default is 2 |
| `FOLIOSPACE_MEMORY_LIMIT_MB` | Go memory limit; Compose default is 768 MiB |

An empty API-token environment variable does **not** disable a token previously saved during setup. Use HTTPS across untrusted networks. Profiles partition state within a shared-token household; they are not separate authenticated accounts.

## 简体中文

1.10 增加 Calibre 元数据、电子书/漫画与格式筛选，整理首页与漫画层级，修复 EPUB 双栏、键盘翻页、进度恢复和脚注。重扫与游戏分析保留人工修正；确认损坏或空文件会明确标记并从最近添加隐藏，同时保留源文件、条目与错误历史。此前影片库、阅读和游戏功能继续保留。

无需 NAS，也无需写代码，但要准备 Docker、持久配置目录、媒体目录、服务地址和 token。默认端口 8080；首次打开网页完成设置，再让客户端填写相同 URL/token。升级前备份 `/config`，保留原挂载和口令。发布 Docker 不代表现有容器或商店客户端已自动更新。

TMDB 可选且默认关闭，需部署者自行申请并配置读取令牌；没有共享开发者密钥。自备不等于商业授权，须遵守来源条款和署名要求。FolioSpace 不提供或下载影片、ROM、BIOS 等内容。

## Links

- [Project and English introduction](https://github.com/funland/foliospace-Library)
- [中文项目介绍](https://github.com/funland/foliospace-Library/blob/main/README.zh-CN.md)
- [Release notes and upgrade cautions](https://github.com/funland/foliospace-Library/blob/v1.10/docs/releases/1.10.md)
- [Movie library API](https://github.com/funland/foliospace-Library/blob/v1.10/docs/api/video-library-v1.md)
- [Seasons, progress and next-up contract](https://github.com/funland/foliospace-Library/blob/v1.10/docs/api/video-library-p1-p2.md)
- [MCP documentation](https://github.com/funland/foliospace-Library/blob/main/docs/mcp/usage.md)
