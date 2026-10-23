# 18081 首页整理、Calibre 与漫画目录改进

2026-10-23 在外盘 `reader-mainline-20261021/FolioSpaceReader` 完成。基于 `codex/release-1.03`、HEAD `46770f51fe4d1cef1e69437faade8e9a70922964` 的既有脏工作区，未提交或推送。未修改内盘副本、Apple 客户端、其他工作区或 NAS 18080。

## 最终行为

- 首页资源目录管理移至设置，空库保留管理入口。作品集点击进入详情，可返回目录，收藏和喜欢入口保留。
- 视频首页、文件列表与电影墙统一优先显示手动标题、已匹配作品标题、清洗标题；保留原始文件名和标识。无年份时支持从发布名补充年份，数字片名不会被直接当作年份。
- 继续阅读排除明确已读或整书完成的项目。EPUB 的单章 100% 不代表整书结束；未完成的四舍五入显示最高为 99%。
- 修复首页数量、就绪状态、主要任务状态等漏翻，空游戏字段隐藏。
- Calibre 通过书库内 `metadata.db` 的路径与格式精确匹配，导入书名、作者、简介、标签和封面；源库不写入。活跃 WAL/事务日志、损坏数据库或不可用记录可回退 `metadata.opf`；新书仍可按原扫描路径入库。已有记录保留 ID、私人阅读状态和手动集合关系，未变元数据避免重写。
- 封面检查文件大小、像素数量及完整 JPEG 解码，损坏封面回退内嵌封面。缓存版本随元数据/封面变化更新。
- 漫画默认按一层目录浏览，叶目录直接打开作品集，父目录与子目录不在同一层重复罗列。封面缩略图代替无意义缩写；原始路径可展开查看。
- 设置中按漫画库配置标题正则，支持预览、保存和恢复默认。默认去除末尾 `-115` 及括号前的两位以上数字排序前缀，保留作者和卷号；规则只改显示名称，不重命名文件、不合并 ID。

## 验证

以下日志与合成资料位于外盘 `../outputs/current/`：

- `logs/batch1-go.log`、`batch1-web.log`、`batch1-build.log`：第一批通过后才开展第二批。
- `logs/final-go.log`：`go test ./...` 全部通过，包含既有热修复回归。
- `logs/final-race.log`：bookmeta、scanner、store、service、httpapi 五个包 `go test -race` 全部通过。
- `logs/final-vet.log`：`go vet ./...` 通过。
- `logs/final-web.log`：49 个前端测试通过；TypeScript 检查通过。
- `logs/final-web-build.log`：Vite 构建通过；仍有大于 500 kB 的既有分包提示。
- `qa/api-check.json`：最终二进制在 18082 合成库验证 Calibre 中文书名/作者/简介/标签/封面、漫画层级与 ID、EPUB 章节进度、已读过滤、视频统一标题与 1991 年份，所有源文件哈希未变。
- Chrome 检查首页、设置/规则预览、漫画目录、作品详情及收藏切换。截图 `qa/home-final.png`、`settings-final.png`、`comic-detail.png`。截图内容为合成验证资料。
- `qa/hotfix-preservation.json`：原 15 份热修复文件均保留，10 份字节不变，5 份集成修改；原 EPUB viewport、分页聚合、请求计时及视频海报缓存失效逻辑保留。基线与补丁仍在 `ux-metadata-baseline/`。

## NAS 部署与回滚

访问：<http://192.168.10.175:18081/>。

- 沿用测试容器 `foliospace-ludoteca-test`，ID `1a63e43ca2ac66b4312876f965bc3179e7daf86f0e937abea1b9b662f0611ebb`，原端口、配置和只读媒体挂载保持不变；部署阶段认证不变，随后按用户明确授权仅重置 18081 的访问令牌。
- Linux amd64 程序 SHA-256：`f7938d98d392490cd1216c6ec174408a3b1d5bced4cbf8ed9236795d545b0f84`。静态资源 13 个逐项经 HTTP 获取和 SHA-256 验证。首页 HTTP 200，未认证客户端首页 HTTP 401。
- SQLite `quick_check` 为 `ok`，15 张资料/档案/阅读/私人状态/手动集合相关表的数量及内容指纹前后相同。无活动扫描任务。现有测试库只有 1 本书、3 个游戏、2 个资源库；未把本地合成资料写入 NAS。
- 生产容器 `foliospace-library` 的 ID、镜像、启动时间、端口和按路径归一化的挂载均未变。未停止或写入 18080。
- 服务器发布及完整回滚备份：`/volume2/docker/foliospace-ludoteca-test/releases/ux-metadata-20261023-final/`。`rollback.sh` 可恢复旧程序和网页；数据库全量快照另存 `rollback/config.tar.gz`，仅在需要恢复数据时停服后成套恢复 DB/WAL/SHM。
- 本次更新现有测试容器的可写层。重启容器保留更新；若从旧镜像重新创建容器，应重新安装已留存的 `new/` 运行文件。没有把旧镜像标签伪称为新镜像。
- 首次部署因 Docker 挂载列表顺序变化触发误报，已自动恢复旧程序和数据库，并验证旧程序及首页哈希、资料指纹。比较改为按挂载路径排序后第二次部署通过。首次尝试保留简要证据，重复构建与备份已清理，完整回滚保留一份。
- 本地收据：`qa/nas-deployment-receipt.json`。源码哈希、构建命令、前端文件哈希见 `build/release-manifest.json`，重新打包脚本 `qa/package-release.py`。

## 验证边界

用户随后明确授权仅重置 18081 测试令牌，保留资料且不改 18080。已备份原认证哈希及更新时间，仅更新 `app_settings` 中 `admin_token_sha256` 一行，无服务重启。新令牌认证 HTTP 200，15 张资料/档案/阅读/私人状态表的数量与指纹完全不变，两个容器的身份、挂载和启动时间不变。收据为 `qa/nas-auth-reset-receipt.json`、`qa/nas-auth-post-qa.json`。原哈希备份在 NAS 发布目录的 `auth-reset/previous-auth-setting.json`，权限 0600；恢复旧数据库快照也会恢复旧令牌。

新令牌保存在本机登录钥匙串，服务 `FolioSpace NAS 18081`，账户 `192.168.10.175:18081`。明文未写入源码、聊天或日志，浏览器传递所用临时文件已删除。由于同主机 Cookie 不区分端口，验收使用独立的内置浏览器，避免覆盖 Chrome 中 18080 的登录 Cookie。已解锁页面保留给用户。

NAS 登录后已验收：首页无管理表单，设置页显示原有两个库；原有一书三游戏与作品集仍可见，作品详情和返回导航正常；视频墙与全部文件均为正常空状态；任务状态中文正常；刷新保持登录。没有扫描、新增库、修改收藏或阅读进度。截图为 `qa/nas-home-authenticated.png`、`nas-settings-authenticated.png`、`nas-collection-authenticated.png`、`nas-jobs-authenticated.png`。

验收发现缩略图 Worker 的 `IDLE` 尚未翻译，已复用五语状态映射补齐 `idle/stopped`，前端 49 项测试、TypeScript 与构建再次通过。仅更新 18081 静态资源，无重启；13 个文件经 HTTP 哈希验证，页面已显示“空闲”。证据为 `logs/worker-locale-web.log`、`worker-locale-build.log`、`qa/nas-worker-locale-receipt.json`；最新资源及源码哈希已更新到 `build/release-manifest.json`。

截图复查发现 783px 窄窗口仍使用旧主网格的 380px+620px 最小列宽，且无私人书架时空列占位。已将主网格改为可收缩单列，空书架分组不占位；宽屏有两个分组时保留双列，窄屏纵向排列。单书封面不再撑满整列，设置页长路径和按钮允许换行。49 项前端测试、类型检查和构建再次通过；本地 1388px 双组首页、NAS 783px 首页/设置/作品详情的页面宽度均等于视口宽度。最新静态资源再次逐项验证，后台程序与运行时间均未变。日志为 `logs/homepage-layout-web.log`、`homepage-layout-build.log`，收据为 `qa/nas-homepage-layout-receipt.json`。

该 NAS 测试库没有 Calibre、漫画或视频样本，也无已有阅读进度，因此这些有内容场景仍以此前本地合成库验证为依据，不能宣称已用 NAS 真实资料验证。

## 重建与保留

Go 1.24.7 工具链、依赖下载、最新 macOS/Linux 二进制与前端 dist 均保留在 `../outputs/current/`。本地验证服务 18082 已停止。过期哈希静态文件、Go 编译缓存和错误输出位置的本任务临时产物已清理，原 `web/dist` 未动。为前端重建可临时把 `web/node_modules` 链接到 `../../outputs/current/build/web-dependencies/node_modules`，然后使用保留的 Node 运行 TypeScript/Vite；结束后移除该临时链接。

本地清理标称文件大小 889,854,460 字节，实际可用空间变化 +914,927,616 字节；两者口径不同。详细删除清单和 NAS 清理数据在 `qa/cleanup-receipt.json`、`qa/nas-cleanup-receipt.json`。

后续令牌与页面验收产生的旧前端副本和临时上传已清理，最新构建及原完整回滚保留。该次本地清理标称 690,156 字节、实际可用空间 +675,840 字节；详见 `qa/auth-ui-cleanup.json`、`qa/nas-auth-ui-cleanup.json`。

首页布局收尾的构建清理标称 755,463 字节，实际可用空间 +737,280 字节；NAS 临时副本标称 42,633,919 字节，观测实际可用空间变化 0。清理均先检查打开文件及进程，最新构建和完整回滚保留；详见 `qa/homepage-layout-cleanup.json`、`qa/nas-homepage-layout-cleanup.json`。

## 后续真实样本验收

2026-10-23 用户追加授权后，已在独立目录用 1 本 Calibre EPUB、2 卷真实漫画和 1 段真实视频补验收，并在独立档案完成已读过滤边界验证。结果及现存显示细节见 `test-18081-real-samples-2026-10-23.md`；此前“NAS 没有真实样本”的描述是追加验收前的阶段性边界。
