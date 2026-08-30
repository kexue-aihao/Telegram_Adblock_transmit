# Telegram 群组广告拦截机器人（Go）

这是一个面向 Telegram 群组和启用话题（Forum Topics）超级群的审核机器人。它使用 Go 1.24、Telegram Bot API、PostgreSQL 和 Go 标准库 RE2 正则引擎。机器人收到消息后会尽快删除命中广告规则的文字或媒体说明，并在原话题内发送提示。

> Telegram Bot API 无法在成员客户端按下发送按钮前拦截消息。机器人必须被授予群管理员的“删除消息”权限。

## 功能

- 规则按 `chat_id` 隔离，启用/停用后立即刷新内存缓存。
- 检查新消息和编辑消息的 `text`、媒体 `caption`。
- 命中所有启用规则后只删除一次，并保留删除成功或失败的审计记录。
- 话题群的提示携带原 `message_thread_id`，不会发送到其他话题。
- 审计记录保存规则 ID、用户/消息 ID、SHA-256 和最多 120 个字符的摘要，超过 30 天自动清理。
- 不自动禁言、踢出或封禁成员；不扫描历史消息、贴纸、文件名、OCR、语音或视频语音。

## 前置配置

1. 在 [@BotFather](https://t.me/BotFather) 创建机器人并获取 Token。
2. 在 BotFather 执行 `/setprivacy`，选择 `Disable`，否则机器人收不到群成员普通消息。
3. 把机器人加入目标群组并提升为管理员，至少授予“删除消息”权限。
4. 复制配置并设置强密码：

   ```powershell
   Copy-Item .env.example .env
   ```

   编辑 `.env`，填写 `BOT_TOKEN`、`POSTGRES_PASSWORD` 和可选的 `LOG_LEVEL`。如果在同一台机器上运行 Telegram Bot API Server，可设置 `TELEGRAM_API_ENDPOINT`，例如 `http://telegram-bot-api:8081/bot%s/%s`；必须保留两个 `%s` 占位符，分别用于 Token 和 API 方法。Compose 会把数据库密码拼入 URL，因此密码请使用 URL 安全字符；若使用特殊字符，请对密码进行 URL 编码。

## Docker 部署（推荐）

```powershell
docker compose up --build -d
docker compose logs -f bot
```

Compose 会先等待 PostgreSQL 健康检查通过；Go 服务启动时执行 `migrations/*.sql` 迁移，然后开始 long polling。请勿为同一个 Bot Token 运行多个 polling 实例。停止服务：

```powershell
docker compose down
```

数据库数据保存在 `postgres_data` 卷中。迁移只创建/更新表，不会自动 `DROP TABLE` 或清空旧 Python 数据；若要采用全新数据库，请在确认备份后使用新的数据库或明确删除该卷。

### 一键拉取已发布镜像

GitHub Actions 会在 `master` 分支和 `v*.*.*` 标签构建并发布多架构镜像到 GitHub Container Registry（GHCR）。服务器上首次部署：

```powershell
Copy-Item .env.example .env
# 编辑 .env，填写 BOT_TOKEN、POSTGRES_PASSWORD 等值
docker login ghcr.io
docker compose -f docker-compose.pull.yml --env-file .env pull
docker compose -f docker-compose.pull.yml --env-file .env up -d
docker compose -f docker-compose.pull.yml --env-file .env logs -f bot
```

也可以使用一键脚本（PowerShell 7+）：

```powershell
Copy-Item .env.example .env
# 编辑 .env 后执行
.\scripts\deploy.ps1
```

脚本会拉取 GHCR 镜像、启动 PostgreSQL 和机器人，并输出服务状态；它不会删除数据库卷。

建议生产环境固定版本，例如 `BOT_IMAGE=ghcr.io/kexue-aihao/telegram-adblock-transmit:v1.0.0`，升级时执行 `pull` 后再执行 `up -d`。GHCR 包若设为私有，`docker login ghcr.io` 使用具有 `read:packages` 权限的 Personal Access Token；公开包无需登录。

### 反向代理与域名访问

本机器人使用 Telegram long polling，没有网页 HTTP 接口，不能直接通过 Caddy/Nginx 代理成网站。反向代理应放在独立的 Telegram Bot API Server 前面；机器人通过 `TELEGRAM_API_ENDPOINT` 调用该地址：

```env
TELEGRAM_API_ENDPOINT=https://telegram-api.example.com/bot%s/%s
```

Caddy 示例见 [deploy/Caddyfile.example](deploy/Caddyfile.example)，Nginx 示例见 [deploy/nginx.telegram-api.conf.example](deploy/nginx.telegram-api.conf.example)。将反代服务与 Bot API Server 放在同一 Docker 网络，或把 upstream 改为服务器可访问的内网地址；公网只开放 443，Bot API Server 的 8081 端口不要直接暴露。

自建 Bot API Server 需要单独准备 Telegram API ID、API Hash、Bot Token、持久化目录和运行镜像。本项目不会自动安装或启动该服务。反向代理只负责 HTTPS、域名和转发，不能替代 Telegram Bot API Server。

## 本地开发

需要 Go 1.24+ 和 PostgreSQL。设置标准 PostgreSQL DSN（不是 SQLAlchemy URL）：

```powershell
$env:BOT_TOKEN = "..."
$env:DATABASE_URL = "postgres://telegram_bot:password@localhost:5432/telegram_adblock?sslmode=disable"
$env:LOG_LEVEL = "INFO"
# 可选：自建 Bot API Server；省略则使用官方 API
# $env:TELEGRAM_API_ENDPOINT = "http://127.0.0.1:8081/bot%s/%s"
go run ./cmd/bot
```

程序通过 `TELEGRAM_API_ENDPOINT` 连接自建 Telegram Bot API Server，支持 HTTP 或 HTTPS。Bot API Server 本身需要单独部署并配置 Telegram API ID、API Hash、Bot Token 和持久化数据目录；本项目只负责调用它，不会自动下载或启动该服务。使用 Docker Compose 时，端点中的主机名应填写同一 Compose 网络内可解析的服务名。

运行测试和静态检查：

```powershell
go test ./...
go test -race ./...
go vet ./...
gofmt -l .
```

## 管理命令

下列命令只能由所在群组管理员执行，规则作用于该群的全部话题：

| 命令 | 说明 |
| --- | --- |
| `/rule_add <regex>` | 校验并新增一条启用的 Go RE2 正则。 |
| `/rule_list` | 列出本群规则和启停状态。 |
| `/rule_remove <规则ID>` | 删除本群规则。 |
| `/rule_enable <规则ID>` | 启用本群规则。 |
| `/rule_disable <规则ID>` | 停用本群规则。 |
| `/rule_test <文本>` | 测试文本命中的规则，不删除测试命令。 |
| `/adlog [1-20]` | 查看本群最近命中审计记录，默认 10 条。 |

示例：

```text
/rule_add 免费.*领取
/rule_add https?://\S+\.example
/rule_test 免费领取 https://spam.example
```

规则默认忽略大小写。Go 使用 RE2 引擎，保证正则匹配线性时间；不支持 Python/PCRE 的反向引用、条件表达式及 lookaround（如 `(?=...)`、`(?<=...)`）。保存时会返回明确错误。单条规则最长 512 个 Unicode 字符。

## 数据与隐私

表结构位于 [migrations/0001_initial.sql](migrations/0001_initial.sql)，包含 `chat_groups`、`moderation_rules` 和 `moderation_audit_logs`。审计摘要仅保留截断文本，`/adlog` 只应在受信任的管理员群内使用。不要提交 `.env` 或 Bot Token。

## 性能基准

`bench/rules_benchmark_test.go` 提供单线程、并发缓存匹配和规则编译基准：

```powershell
go test ./bench -bench=. -benchmem
```

审核热路径只读预编译规则缓存，不查询 PostgreSQL；规则编译仅在启动和管理命令刷新时发生。

## PostgreSQL 集成测试

Docker Desktop 启动后，可使用隔离项目运行 PostgreSQL repository 集成测试。脚本会创建临时数据库，测试结束后删除该数据库、容器和卷，不会连接默认部署卷：

```powershell
.\scripts\integration.ps1
```
