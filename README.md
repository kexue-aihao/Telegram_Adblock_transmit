# Telegram 群组广告拦截机器人

这是一个使用 Go 编写的 Telegram 群组广告拦截机器人。它通过 Telegram Bot API 接收群组消息，使用 Go RE2 正则表达式检查文本和媒体说明，命中规则后删除消息，并在原来的话题中发送提示。

项目适合部署在自己的 Linux 服务器上。推荐使用 1Panel 管理 Docker Compose、PostgreSQL、域名证书和反向代理。机器人本身没有网页管理后台，也不监听网站 HTTP 端口；需要域名时，域名应指向独立的 Telegram Bot API Server，而不是 bot 容器。

## 1. 程序介绍

### 主要功能

- 按 Telegram 群组隔离广告规则。
- 检查新消息和编辑消息的 `text`、媒体 `caption`。
- 命中规则后删除消息，并记录删除成功或失败的审计记录。
- 在 Forum Topics 群组中沿用原消息的 `message_thread_id` 发送提示。
- 管理员可以在线新增、删除、启用、停用和测试规则。
- 审计日志保留 30 天，内容摘要最多 120 个字符，并保存 SHA-256 摘要。
- Poller 会处理进程停止期间积压的更新；业务处理失败会重试，超过次数后跳过该更新并继续处理后续消息。

### 不包含的功能

- 不提供网页控制台或用户登录页面。
- 不扫描历史消息、贴纸、文件名、OCR、语音或视频语音。
- 不自动禁言、踢出或封禁成员。
- 不自动安装或启动 Telegram Bot API Server，该服务需要单独部署。

### 工作方式

~~~text
Telegram 群组
      │ Long Polling
      ▼
telegram-adblock-transmit ───── PostgreSQL
      │
      ├─ 官方 Bot API（默认）
      └─ 自建 Bot API Server（可选，建议通过 HTTPS 域名）
~~~

容器包括：

- `bot`：本项目的 Go 服务，启动时自动执行数据库迁移，然后开始 Long Polling。
- `postgres`：保存规则、群组信息和审计日志，数据位于 `postgres_data` 卷。

同一个 Bot Token 只能运行一个 Long Polling 实例。不要通过增加 bot 副本来扩容，否则会出现更新争抢或重复处理。

## 2. 部署前准备

### Telegram Bot

1. 在 [@BotFather](https://t.me/BotFather) 创建 Bot，保存 Token。
2. 在 BotFather 执行 `/setprivacy`，选择 `Disable`，否则 Bot 通常收不到群组普通消息。
3. 将 Bot 加入目标群组并设为管理员，至少授予“删除消息”权限。
4. 确认群组允许 Bot 发送消息；Forum Topics 群组需要允许 Bot 在相应话题中发言。

Token 只放在服务器的 `.env` 或 1Panel 密钥存储中，不要提交到 Git、工单或聊天工具。

### 服务器和网络

推荐配置为 2 vCPU、2 GB RAM、20 GB 可用磁盘空间。生产环境建议：

- 使用已安装 Docker 的 Linux 服务器，并安装 1Panel。
- 只开放 SSH、1Panel 管理端口和 HTTPS 443。
- PostgreSQL 5432 和 Bot API Server 8081 只允许内网访问，不要直接暴露公网。
- 如果使用 Let’s Encrypt HTTP-01 验证，申请证书时还需要允许 80 端口。

### 镜像

发布镜像位于 GitHub Container Registry：

~~~text
ghcr.io/kexue-aihao/telegram-adblock-transmit
~~~

生产环境建议固定版本或不可变摘要，不要长期使用 `latest`：

~~~env
BOT_IMAGE=ghcr.io/kexue-aihao/telegram-adblock-transmit:v1.0.0
# 或：
# BOT_IMAGE=ghcr.io/kexue-aihao/telegram-adblock-transmit@sha256:<digest>
~~~

如果 GHCR 包是私有的，在 1Panel 的容器镜像仓库中添加 GHCR 凭据，或执行 `docker login ghcr.io`。Personal Access Token 至少需要 `read:packages` 权限。

## 3. 使用 1Panel 安装

以下菜单名称以常见 1Panel 版本为例。不同版本可能把“容器 -> 编排”显示为“容器 -> Compose”，功能相同。

### 第一步：确认 Docker

1. 登录 1Panel。
2. 打开“容器”，确认 Docker 服务状态为运行中。
3. 如果未安装 Docker，先在 1Panel 的容器设置或安装向导中安装 Docker Engine 和 Docker Compose。
4. 打开“主机 -> 防火墙”，只放行实际需要的端口。bot 容器不需要对外开放端口。

### 第二步：创建项目目录和文件

在 1Panel 的“文件”中创建目录：

~~~text
/opt/telegram-adblock-transmit
~~~

将仓库中的以下文件上传到该目录：

- `docker-compose.pull.yml`
- `.env.example`

把 `.env.example` 复制或另存为同目录的 `.env`。也可以在 1Panel 的“终端”中执行：

~~~bash
mkdir -p /opt/telegram-adblock-transmit
cd /opt/telegram-adblock-transmit
curl -fL -o docker-compose.pull.yml https://raw.githubusercontent.com/kexue-aihao/telegram-adblock-transmit/master/docker-compose.pull.yml
curl -fL -o .env.example https://raw.githubusercontent.com/kexue-aihao/telegram-adblock-transmit/master/.env.example
cp .env.example .env
chmod 600 .env
~~~

如果要部署指定版本，请把下载 URL 中的 `master` 换成对应的发布标签。

### 第三步：填写环境变量

在 1Panel 文件编辑器中打开 `/opt/telegram-adblock-transmit/.env`，至少填写：

~~~env
BOT_TOKEN=替换为BotFather生成的Token
POSTGRES_PASSWORD=生成一个足够长的随机密码
BOT_IMAGE=ghcr.io/kexue-aihao/telegram-adblock-transmit:v1.0.0
LOG_LEVEL=INFO
~~~

`POSTGRES_PASSWORD` 会被 Compose 拼接到 `DATABASE_URL` 中。请使用 URL 安全字符（字母、数字、点、下划线、短横线），不要直接使用 `@`、`:`、`/`、`#` 等字符；如果必须使用特殊字符，需要先进行 URL 编码。

默认使用官方 Telegram Bot API，不需要设置 `TELEGRAM_API_ENDPOINT`。如果使用自建 Bot API Server，端点必须保留两个 `%s` 占位符：

~~~env
# 跨主机或公网：必须使用 HTTPS
TELEGRAM_API_ENDPOINT=https://telegram-api.example.com/bot%s/%s
TELEGRAM_ALLOW_INSECURE_HTTP=false

# 同一受控 Docker 私网：可使用 HTTP，但必须显式允许
# TELEGRAM_API_ENDPOINT=http://telegram-bot-api:8081/bot%s/%s
# TELEGRAM_ALLOW_INSECURE_HTTP=true

# 可选
TELEGRAM_HTTP_TIMEOUT=30s
~~~

### 第四步：创建 1Panel Compose 编排

1. 打开“容器 -> 编排”（或“容器 -> Compose”）。
2. 点击“创建编排”。
3. 编排名称填写 `telegram-adblock-transmit`。
4. Compose 文件选择 `/opt/telegram-adblock-transmit/docker-compose.pull.yml`，或将文件内容粘贴到编辑器。
5. 环境文件选择 `/opt/telegram-adblock-transmit/.env`。如果当前 1Panel 没有单独的环境文件选项，把 `.env` 放在 Compose 文件同目录，Compose 会自动读取。
6. 保存并启动编排。

`docker-compose.pull.yml` 会拉取 bot 镜像、创建 PostgreSQL、创建持久化卷，并等待 PostgreSQL 健康检查通过。生产部署使用这个文件；`docker-compose.yml` 是源码构建配置，不要把两者同时作为同一个编排启动。

### 第五步：验证启动

在 1Panel 的编排详情中确认：

- `postgres` 状态为运行中且健康检查为 `healthy`。
- `bot` 状态为运行中，没有持续重启。
- `postgres_data` 卷已经创建。

在 bot 日志中应能看到类似：

~~~text
telegram moderation bot started
~~~

也可以在 1Panel 终端执行只读检查：

~~~bash
cd /opt/telegram-adblock-transmit
docker compose -f docker-compose.pull.yml --env-file .env config --quiet
docker compose -f docker-compose.pull.yml --env-file .env ps
docker compose -f docker-compose.pull.yml --env-file .env logs --tail=100 bot
~~~

`config --quiet` 没有输出才表示 Compose 配置解析成功。不要把 `.env` 内容或包含 Token 的日志截图发到公共渠道。

## 4. 第一次使用

在目标群组中发送以下命令。命令只能由该群组管理员执行：

| 命令 | 作用 |
| --- | --- |
| `/rule_add <regex>` | 新增一条启用的正则规则 |
| `/rule_list` | 列出本群规则和启停状态 |
| `/rule_remove <规则ID>` | 删除本群规则 |
| `/rule_enable <规则ID>` | 启用本群规则 |
| `/rule_disable <规则ID>` | 停用本群规则 |
| `/rule_test <文本>` | 测试文本命中的规则，不删除测试命令 |
| `/adlog [1-20]` | 查看最近的广告命中审计记录，默认 10 条 |

示例：

~~~text
/rule_add 免费.*领取
/rule_add https?://\S+\.example
/rule_test 免费领取 https://spam.example
~~~

规则使用 Go RE2 引擎，默认忽略大小写，单条规则最多 512 个 Unicode 字符。RE2 不支持 Python/PCRE 的反向引用、条件表达式和 lookaround。每个群组最多 100 条规则，所有规则合计最多 32768 个字符。

带目标的命令只会由目标 Bot 处理，例如 `/rule_list@my_bot`。发给其他 Bot 的命令不会被本 Bot 当作管理命令执行。

## 5. 在 1Panel 配置自建 Bot API Server（可选）

官方 Bot API 已经足够大多数部署。只有在网络策略、出口代理或本地化需求明确时，才建议额外部署自建 Bot API Server。

### 5.1 准备自建服务

自建 Bot API Server 需要单独准备 Telegram API ID、API Hash、Bot Token、持久化目录和受信任的运行镜像。本项目不包含该服务，也不替你选择第三方镜像。请按所选镜像的官方文档完成初始化，并确认服务：

- 在容器内监听 `8081`。
- 服务名设置为 `telegram-bot-api`，或在下面的配置中替换成实际名称。
- 与 bot 容器处于同一个 Docker 网络，或者仅绑定到宿主机回环地址 `127.0.0.1:8081`。
- 不直接把 8081 发布到公网。

### 5.2 创建共享网络

在 1Panel 终端执行一次：

~~~bash
docker network create telegram-bot-api-net
~~~

如果网络已经存在，提示已存在即可。自建 Bot API Server 的 Compose 编排和 bot 编排都要加入这个 external 网络。

将下面内容保存为 `/opt/telegram-adblock-transmit/docker-compose.telegram-api-network.yml`，并在 1Panel 编排中作为附加 Compose 文件：

~~~yaml
services:
  bot:
    networks:
      - default
      - telegram_bot_api

networks:
  telegram_bot_api:
    external: true
    name: telegram-bot-api-net
~~~

自建 Bot API Server 的 Compose 文件也要声明相同的 external 网络，并把 `telegram-bot-api` 服务加入该网络。

### 5.3 配置 bot 端点

编辑 bot 编排的 `.env`：

~~~env
TELEGRAM_API_ENDPOINT=http://telegram-bot-api:8081/bot%s/%s
TELEGRAM_ALLOW_INSECURE_HTTP=true
~~~

这里的 HTTP 只在 Docker 私网内传输。跨主机、经过公网或经过不完全受信任的网络时，必须改成反向代理后的 HTTPS 域名：

~~~env
TELEGRAM_API_ENDPOINT=https://telegram-api.example.com/bot%s/%s
TELEGRAM_ALLOW_INSECURE_HTTP=false
~~~

修改后在 1Panel 中重新部署或重建 bot 容器。不要只重启 PostgreSQL。

### 5.4 使用 1Panel 网站反向代理

反向代理的目标是自建 Bot API Server，不是 `bot` 容器：

1. 在 DNS 中将 `telegram-api.example.com` 的 A/AAAA 记录指向服务器。
2. 在 1Panel 打开“网站 -> 创建网站”，选择“反向代理”，或先创建站点后添加反向代理。
3. 填写域名 `telegram-api.example.com`。
4. 上游地址根据网络拓扑选择：
   - 1Panel 网站 Nginx 运行在宿主机：Bot API Server 只绑定 `127.0.0.1:8081`，上游填写 `http://127.0.0.1:8081`。
   - 1Panel 网站代理运行在 Docker 网络中：把代理容器加入 `telegram-bot-api-net`，上游填写 `http://telegram-bot-api:8081`。
5. 在“SSL”中申请并启用 Let’s Encrypt 或已有证书。
6. 公网只开放 443；如果使用 HTTP-01 申请证书，临时或长期开放 80 供 ACME 使用。
7. 保存并重载网站配置。

Bot Token 位于 Bot API 请求路径 `/bot<TOKEN>/...` 中。请在 1Panel 网站日志设置中关闭该站点的 access log，或配置脱敏策略；不要让 Nginx、Caddy、WAF、CDN 或网关记录完整请求 URI。

仓库中的代理模板可直接参考：

- [Nginx 配置示例](deploy/nginx.telegram-api.conf.example)
- [Caddy 配置示例](deploy/Caddyfile.example)

两个示例都设置了 Long Polling 所需的超时，并关闭访问日志。反向代理只负责 HTTPS 和转发，不能替代 Bot API Server。

### 5.5 检查自建服务联通性

在 bot 容器所在的网络中检查服务名和端口：

~~~bash
docker run --rm --network telegram-bot-api-net curlimages/curl:8.10.1 \
  -fsS http://telegram-bot-api:8081/bot<YOUR_TOKEN>/getMe
~~~

如果使用 HTTPS 域名，则从服务器执行：

~~~bash
curl -fsS https://telegram-api.example.com/bot<YOUR_TOKEN>/getMe
~~~

命令输出中不要保留 Token，也不要把完整命令和输出复制到公共工单。

## 6. 配置参考

| 变量 | Compose 默认值 | 是否必需 | 说明 |
| --- | --- | --- | --- |
| `BOT_TOKEN` | 无 | 是 | BotFather 生成的 Token |
| `POSTGRES_PASSWORD` | 无 | 是 | Compose 创建 PostgreSQL 用户的密码 |
| `BOT_IMAGE` | `ghcr.io/kexue-aihao/telegram-adblock-transmit:latest` | 否 | 生产环境建议固定版本或摘要 |
| `TELEGRAM_API_ENDPOINT` | `https://api.telegram.org/bot%s/%s` | 否 | 必须包含且只能包含两个 `%s` |
| `TELEGRAM_ALLOW_INSECURE_HTTP` | `false` | 否 | 仅允许本地、私网或 Docker 服务名的 HTTP 端点 |
| `TELEGRAM_HTTP_TIMEOUT` | `30s` | 否 | Telegram API 请求超时，必须为正时长 |
| `LOG_LEVEL` | `INFO` | 否 | `DEBUG`、`INFO`、`WARN` 或 `ERROR` |
| `DATABASE_URL` | Compose 自动生成 | 本地运行时必需 | 标准 PostgreSQL DSN，不要使用 SQLAlchemy URL |

端点安全规则：HTTPS 默认允许；HTTP 必须设置 `TELEGRAM_ALLOW_INSECURE_HTTP=true`，并且主机只能是回环地址、私网 IP、`localhost`、`host.docker.internal`、`gateway.docker.internal` 或单标签 Docker 服务名。

## 7. 升级、回滚和备份

### 升级

1. 在 1Panel 中备份 `postgres_data` 卷，并保存 `.env` 的加密副本。
2. 将 `BOT_IMAGE` 改为目标版本，例如 `v1.1.0`。
3. 在编排详情中执行拉取镜像并重新创建/启动服务。
4. 查看 PostgreSQL 健康状态和 bot 日志，确认 bot 没有反复重启。

迁移在 bot 启动时自动执行，只会创建或更新所需结构，不会自动删除表或清空旧数据。升级前仍应保留数据库备份。

### 回滚

将 `BOT_IMAGE` 改回上一个已验证版本，重新拉取并重建 bot。不要把 `latest` 当作回滚版本。

### 备份

在 1Panel 的计划任务中配置：

- PostgreSQL 数据卷或数据库逻辑备份。
- `.env` 的加密备份，不要把明文 Token 上传到公共对象存储。
- 反向代理证书和站点配置。

恢复前先停止 bot，避免恢复期间产生新的写入。确认数据库恢复完成后再启动 bot。

## 8. 常见问题

### bot 容器反复重启

在 1Panel 编排详情打开 bot 日志。常见原因：

- `BOT_TOKEN` 为空或复制错误。
- `POSTGRES_PASSWORD` 为空，或密码含有未编码的 URL 特殊字符。
- 自建端点不是绝对 HTTP(S) URL，或缺少两个 `%s`。
- 使用 HTTP 端点但未设置 `TELEGRAM_ALLOW_INSECURE_HTTP=true`。
- `postgres` 尚未 healthy，或数据卷权限/磁盘空间不足。

### PostgreSQL 一直不健康

确认 1Panel 主机磁盘空间、内存和 `postgres_data` 卷状态。查看 `postgres` 日志，不要删除卷来“修复”问题；删除卷会丢失规则和审计数据。

### Bot 加群后收不到普通消息

检查 BotFather 的 `/setprivacy` 是否为 `Disable`，并确认 Bot 是管理员且拥有删除消息权限。还要确认同一个 Token 没有在另一台机器上运行 Polling。

### 自建 Bot API 返回 404、502 或连接超时

按顺序检查：

1. Bot API Server 是否监听 8081。
2. `telegram-bot-api` 服务是否和 bot/反向代理加入同一个网络。
3. 请求路径是否为 `/bot<TOKEN>/<method>`，端点模板是否保留两个 `%s`。
4. 反向代理是否关闭了过短的读取超时。
5. HTTPS 证书、DNS 和 1Panel 防火墙是否正常。

### 访问 Bot API 域名看不到网页

这是正常的。本项目没有网页端点；该域名只用于转发 Telegram Bot API 请求。不要把反向代理目标填写为 bot 容器。

### 规则命令没有生效

只有群组管理员可以管理规则。规则使用 Go RE2 语法；不支持 lookaround、反向引用等 PCRE/Python 特性。规则数量和总长度达到上限时，命令会返回配额提示。

## 9. 源码运行和开发

本地运行需要 Go 1.24+ 和 PostgreSQL：

~~~bash
export BOT_TOKEN='...'
export DATABASE_URL='postgres://telegram_bot:password@127.0.0.1:5432/telegram_adblock?sslmode=disable'
export LOG_LEVEL=INFO
go run ./cmd/bot
~~~

使用本地自建 Bot API 时：

~~~bash
export TELEGRAM_API_ENDPOINT='http://127.0.0.1:8081/bot%s/%s'
export TELEGRAM_ALLOW_INSECURE_HTTP=true
~~~

常用检查：

~~~bash
go test ./...
go test -race ./...
go vet ./...
gofmt -l .
~~~

GitHub Actions 会在 `master` 分支和 `v*.*.*` 标签上构建并发布多架构镜像，同时执行测试、依赖校验、Trivy 扫描、SBOM 和 provenance 生成。Pull Request 会执行测试和不发布镜像的 Docker 构建。

## 10. 安全清单

- Bot Token 只放在 1Panel 的 `.env` 或受控密钥存储中。
- 生产环境固定 `BOT_IMAGE` 版本或摘要。
- 关闭 Bot API 反向代理的 access log，或确认已脱敏 URI。
- 不公开 PostgreSQL 5432 和 Bot API Server 8081。
- 同一 Bot Token 只运行一个 bot 编排实例。
- 定期备份 PostgreSQL 数据和加密后的 `.env`。
- 定期更新 1Panel、Docker、PostgreSQL 和镜像摘要。
- 发生 Token 泄露时，立即在 BotFather 重新生成 Token，并更新 `.env` 后重建 bot 容器。

## 11. 项目文件

- `docker-compose.yml`：本地源码构建配置。
- `docker-compose.pull.yml`：生产环境拉取 GHCR 镜像的配置。
- `.env.example`：环境变量模板。
- `scripts/deploy.ps1`：PowerShell 一键拉取和启动脚本，适合 Windows 管理机。
- `deploy/nginx.telegram-api.conf.example`：Nginx 反向代理模板。
- `deploy/Caddyfile.example`：Caddy 反向代理模板。
- `migrations/`：数据库迁移，启动时自动执行。
