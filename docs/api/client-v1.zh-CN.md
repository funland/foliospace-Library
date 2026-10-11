# FolioSpace Library Client API v1 接入摘要

[English API reference](client-v1.md) · [产品与下载](../../README.zh-CN.md)

本摘要对应 1.03，包含完整 P0/P1/P2 影片库。欢迎独立客户端接入；完整字段、请求及响应示例以 [英文 API 参考](client-v1.md) 为准。旧路由和响应保持兼容，新增可选字段不能当成旧服务器必有字段，未知字段应忽略。实际互通、格式及运行时兼容性需要客户端自行验证。

## 最短接入流程

1. 由服务所有者完成首次设置，提供服务 origin 和 token。接入客户端无需执行 setup、扫描或修改资料库配置。
2. GET `/api/auth/status`。非空环境 token 或首次设置保存的 token 哈希均可启用认证，环境 token 优先；仅两者都不存在时关闭认证。保护接口使用 `Authorization: Bearer <token>`。
3. GET `/api/client/info`，按 capability flags 决定功能。GET `/api/profiles` 后，在状态请求中一致携带 `X-FolioSpace-Profile-Id`（或 `profileId` query）。旧阅读/游戏接口仍可默认回退；新视频进度接口仅省略时用默认 profile，明确传入无效/不存在的 profile 会拒绝，避免串档案。
4. GET `/api/client/home?limit=12&includeCollections=false` 获取有限首页 shelves。完整目录分别分页 GET `/api/client/books`、`/api/client/games`、`/api/client/videos`；游戏过滤器可用 facets/platforms。
5. 打开返回的 `manifestUrl`，按媒体类型获取文件或资源。所有相对 URL 用 URL parser 相对服务 origin 解析，保留 query、编码及逻辑文件名，媒体子请求也要认证。
6. 同一 profile 同步阅读进度、book private-state、preferences；游戏有 private-state PUT、play-stats 和 save-sync/archive。1.03 视频使用独立的 playback-state/events 协议，不复用游戏接口，也没有通用 video private-state API。
7. 先检查 HTTP status 和 Content-Type，再解码 JSON；对暂时失败做有界退避，不绕过 resolver 冲突。

token 应保存在系统安全存储中，不写入截图、共享 URL 或日志。跨不可信网络使用 HTTPS，只向配置的可信服务发送 token，不向跨域 redirect 或其他 origin 转发。只读 curl 示例见 [英文 quick start](client-v1.md#external-client-quick-start)，交互输入并经 stdin 传 header，避免 token 出现在命令参数中。

## 认证、profile 与浏览器边界

公开 bootstrap/auth 路由为 `/api/auth/status`、`/api/auth/check`、`/api/auth/logout`、`/api/setup/status`、`/api/setup/initialize`、`/api/config/directory-roots`。initialize 自行检查已有 token；已初始化返回 409。其他 `/api/*` 在认证启用时受保护。

浏览器可通过 POST `/api/auth/check` 验证 token 并获得 HttpOnly、SameSite=Lax cookie；当前 cookie 未标记 Secure，应通过 HTTPS 保护。实现也接受 `access_token` query，但优先用 Bearer/cookie，URL token 会进入历史或日志。代码没有内置跨域 CORS/OPTIONS 中间件；同源浏览器接入或另行配置并验证代理。

profile 只是共用 token 服务中的状态分区，不是账户或权限隔离；有效服务 token 也可访问管理接口。book/game/video DTO 隐藏源文件路径；collection（包括 home collections）仍返回 legacy `directoryPath`，管理 API 和 legacy collection volumes/assets 也可能包含源路径。不要用这些路径下载文件，公开响应或日志前脱敏。

## 分页与能力

| 接口 | 默认 limit | 最大值 | 返回 |
| --- | --- | --- | --- |
| `/api/client/books` | 60 | 200 | `{items,total,limit,offset,hasMore}` |
| `/api/client/games` | 50 | 200 | 同上 |
| `/api/client/videos` | 50 | 200 | 同上 |
| `/api/collections` 带 limit 或 offset | 60 | 200 | 同上；旧版不分页调用返回数组 |

offset 从 0 开始，按返回 limit 递增，hasMore 为 false 或空页时停止。不是快照 cursor，扫描期间目录可变化。home 默认 limit 12、最大 50，并不包含完整目录。

`gamePlatformCatalog` 表示 platforms 接口存在；supportedFormats 是服务端收录格式，不是客户端播放保证。`bearerTokenAuth` 反映实际认证状态；`videoHls` 只表示接口，不保证 ffmpeg/编码可用。

正常程序经 `config.Load()` 默认 `FOLIOSPACE_DISABLE_GAME_LAUNCH_RESOLVER=true`，空/无效值也回退为 true。Dockerfile、Compose 均未覆盖；直接 `httpapi.New` 或零值 Options 的 bool 为 false，反而启用 resolver。两个 capability `gameLaunchResolver`、`stableRuntimeIdentityV1` 均取 disable 的反值；禁用时 resolve 返回 404，CIA install 独立可用。以运行实例的 info 为准。1.01 MAME 内容等价、1.02 PC-98 字体和 Triforce 路由没有单独 flags，按可选字段与 manifest 检查。

## 书籍与私人状态

GET `/api/client/books/{id}/manifest` 返回 CBZ/ZIP 页 URL、EPUB spine/TOC/resourceBaseUrl 或 PDF page-0 流。图片页的 displayUrl/maxWidth 用于降采样；PDF page-0 支持 Range。GET/HEAD `/api/client/books/{id}/file` 下载原文件，支持 Range，离线 contentHash/contentRevision 可能为空，应接受后台尚未完成哈希的情况。

GET/PUT `/api/books/{id}/progress` 同步 pageIndex、locator、progressFraction；条漫优先 GET `/reading-position` 与 PUT `/reading-position/webtoon`。GET/PUT `/api/client/books/{id}/private-state` 同步状态、收藏、评分、标签和笔记，GET/PUT `/api/client/preferences` 保存界面语言与阅读偏好。具体值域、合并/归一化规则见英文对应章节。

## 游戏 manifest 与运行时

manifest 的有序 `files[]` 是下载集合，要保留 name、role、顺序及 checksum；多文件描述符与 tracks/dependencies 必须一起处理。canonical manifest 的部分 checksum 可能尚未生成；resolver 需要已校验的完整 package。DOS 保持原 archive 字节，entryFile 指向内部程序或为空，不能把命令交给宿主 shell。CIA 使用独立 install action，客户端负责最终校验与安装。

独立客户端无需冒充官方 app 来浏览目录或读 manifest；resolver 必须报告真实 client/runtime inventory，并获得对应支持和审计 profile。409 不允许回退绕过；仅 resolver 不可用的 404/405/501 允许 legacy manifest fallback。

MAME 优先 exact profile，1.01 起可信 listxml 证据可允许跨版本等价复用，保留 profile ID/revision，返回请求 runtime 和可选 `contentAudit`。审计含 method、policy、sourceRuntime、两个 listxml SHA-256 及 definition SHA-256。变更源字节返回 409 `content-checksum-mismatch`，缺依赖为 `dependency-missing`，未知/不等价证据为 `content-set-mismatch`。内容等价不代表实机游玩验收，见 [MAME 指引](../operations/mame-content-equivalence.md)。官方支持的 FBNeo coreBuildId/coreSha256 为诊断字段，其他 client、runtime、ROM 和依赖规则仍适用。

PC-98 1.02 字体选择顺序是完整 ROM 文件名 `.font.json`（`{"file":"MyFont.bmp"}`）、ROM stem `.font.bmp`、`FONT.bmp`、`PC98_CN.bmp`。只从同目录选择一个有效字体，manifest 逻辑名 `FONT.bmp`、role `font`，有独立 size/SHA-1。仅 ROM hash 不能判断字体更新，需 targeted scan 后刷新 manifest/package。限制与回退见 [字体指引](../operations/pc98-per-game-font.md)。

Triforce ISO/GCM/RVZ 在精确 Triforce 目录/library 名下按 `ngc`/`dolphin` 路由，保留 `romSetName: "Triforce"` 及原 entry 名和字节。它不是新增 triforce 平台，也不承诺 BIOS、模拟器或游戏兼容。

## 视频与 HLS 重试

1.03 新增 `videoLibraryV1`、`videoSeasonsV1`、`videoPlaybackStateV1`、`videoNextUpV1`、`videoEpisodeAvailabilityV1` 能力。
作品目录 `/api/client/video-works` 支持资料库/类型/搜索/最近排序与最多100条的分页；作品下有 files、seasons、episodes、availability，提供海报与稳定季集/版本身份，旧视频 ID 不变。

`GET /api/client/videos/{id}/playback-state`、`POST .../playback-events` 按 profile/文件保存进度，使用 revision、mediaKey、session/sequence/eventID 实现幂等与冲突保护。409 返回 `{code,state}`，不得静默重放到新 revision。`/api/client/video-playback?kind=continue|next-up` 提供列表。能力缺失时保留旧文件浏览和本地进度。
详细字段见 [P1/P2 合约](video-library-p1-p2.md)，本地元数据与海报见 [影片库协议](video-library-v1.md)，在线刮削见 [自备 TMDB 配置](../tmdb-setup.zh-CN.md)。旧 thumbnail 不自动代理新 poster，客户端应使用返回的海报 URL。

GET `/api/client/videos/{id}/manifest` 返回 direct fileUrl、hlsUrl、transcodeStatusUrl。直接文件 GET 支持 Range。只有 GET index.m3u8 会启动/复用转码，manifest 和 status 轮询不启动任务。

进程只有一个转码 slot，输出第一视频流和可选第一音轨的 H.264/AAC stereo，宽度不超过 1920，约 6 秒 TS segments。不承诺多音轨、字幕、蓝光菜单或所有设备解码。官方镜像含 ffmpeg；自定义运行环境需自行提供。

另一视频占用 slot 时 playlist 请求返回 409，status 的 queued 只是观察状态，不是真正持久队列。客户端退避轮询，slot 空闲后必须重新请求 playlist。首次请求等最多 15 秒，尚无 playlist 时当前返回 500 `video transcode is starting`，不是 202；应检查 status 后延迟重试。缺少 ffmpeg 或处理失败也可能 500，不能把所有 500 当成启动中。

ready 优先检查 playlist 是否存在，即使仍在转码或只留下失败的部分输出也可能 ready。它不是完成信号；segmentCount 不是百分比。完整 seek 要结合实际 playlist 结束标记和媒体可用性。failed 根据日志模式推断，不保证覆盖所有失败。

## 错误与稳定兼容

常见错误为 `{"error":"..."}`；resolver/install 冲突为 `{code,message,details?}`，router/文件错误可能纯文本，405 可能空 body。常见状态：400 输入无效、401 缺/错认证、404 无资源/路由、405 方法不支持、409 冲突/兼容性拒绝、416 Range 无效、500 处理失败/转码初始等待超时。501 是兼容 fallback 约定，不表示当前 resolver 会生成该状态。

原书籍和直接视频下载支持 GET/HEAD；其他游戏、HLS、PDF/pages、EPUB 资源按 GET 使用，不泛化 HEAD。Range 成功为 206/Content-Range，也需处理完整返回 200 和无效范围 416。保持 URL query、可选字段 fallback 和 stable v1 兼容，但不承诺未经验证的设备互通。

MCP 实现见 [usage](../mcp/usage.md)；英文文档末尾 MCP Opportunities 是设计建议，MCP 一句话下载播放仍是规划。兼容 FolioLume 开发版已接入新影片接口，不等于公开商店版已更新。
