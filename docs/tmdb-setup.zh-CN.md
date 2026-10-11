# 可选 TMDB 刮削：自行申请、自己配置

[English](tmdb-setup.md) · [影片接口](api/video-library-v1.md)

1.03 支持影片/剧集候选搜索、确认元数据和海报，以及按需刷新已确认剧集的单季目录。
TMDB 不提供影片文件。即使不启用它，本地文件、NFO、封面和人工元数据仍可使用。

## 最短配置流程

1. 用桌面浏览器登录**你自己的** TMDB 账号，从 [设置 → API](https://www.themoviedb.org/settings/api)
   申请，按真实用途填写并自行审阅条款。FolioSpace 不代申请、不代接受条款，也不提供共享开发者密钥。
2. 当前适配器需要 **API Read Access Token（Bearer 读取令牌）**，不是较短的 v3 API key，
   更不是客户端连接 FolioSpace 时使用的 token。
3. 首次使用 1.03 Compose 时，从 `.env.example` 复制出私有 `.env`；已有文件不要覆盖。
   在本地编辑器中填入：

   ```dotenv
   FOLIOSPACE_VIDEO_METADATA_PROVIDER=tmdb
   FOLIOSPACE_TMDB_TOKEN=YOUR_OWN_TMDB_API_READ_ACCESS_TOKEN
   FOLIOSPACE_TMDB_LANGUAGE=zh-CN
   ```

   上面的 token 是占位符。默认 provider/token 均为空，只有明确指定 `tmdb` 且令牌非空才启用。
   旧版自定义 Compose 还需补上当前 `docker-compose.yml` 中对应的 environment 映射，
   只写 `.env` 不会自动传入容器。
4. 备份持久 `/config` 后，用 `docker compose up -d` 应用你自己部署的配置。
   `docker run` 用户通过受保护的 `--env-file` 传入相同变量，不把密钥写在命令参数里。
5. 在视频页选择文件，打开元数据编辑，搜索标题、检查候选、确认匹配，再明确请求海报。
   服务故障不阻断本地播放。单季缺集目录需要已确认的 TMDB 剧集与兼容客户端/API，
   不会自动刮削整库。

## 密钥与数据注意事项

将 `.env` 权限设为仅本人可读写（如 `chmod 600 .env`），不要放共享目录。
仓库和 Docker 构建上下文排除了真实 `.env`，但不能保护你额外复制的备份；Docker 管理员也可查看容器环境变量。
不把密钥放入仓库、镜像、浏览器代码、客户端配置、URL、日志、截图或支持包。
检查 Compose 使用 `docker compose config --quiet`，不要公开包含真实值的完整渲染输出。

搜索向 TMDB 发送输入的标题/类型/语言，后续请求发送选中的来源标识；不会上传影片字节或库路径。
清除 TMDB 缓存保留原文件、本地附属文件和人工字段。终止使用时先禁用 provider，再清缓存，
历史备份中的来源数据由部署者另行处理。详细限制见[英文操作说明](tmdb-setup.md)。

## 自备密钥不等于商用许可

依据 TMDB [官方 FAQ](https://developer.themoviedb.org/docs/faq)，非商业用途与商业授权要求不同。
拥有个人令牌或 FolioSpace 开源许可，**不自动取得 TMDB 数据/图片的商业使用权**；
实际服务、收费客户端等用途需自行核对并按需联系 TMDB。本说明不是针对你的部署作出的法律结论。

使用 TMDB 数据/图片的应用应在 About/Credits 类入口保留官方许可的标志和规定声明，不能暗示 TMDB 背书。
服务器网页的关于页与影片页脚提供署名；独立客户端仍需履行自身义务。
请直接审阅 [API 条款](https://www.themoviedb.org/api-terms-of-use)、
[署名与标志要求](https://www.themoviedb.org/about/logos-attribution)和
[令牌文档](https://developer.themoviedb.org/docs/authentication-application)。
