# Telegram 群组广告拦截机器人

这是一个使用 Go 编写的 Telegram 群组广告拦截机器人。它通过 Telegram Bot API 接收群组消息，使用 Go RE2 正则表达式检查文本和媒体说明，命中规则后删除消息，并在原来的话题中发送提示。

项目适合部署在自己的 Linux 服务器上。推荐使用 1Panel 管理 Docker Compose、PostgreSQL、域名证书和反向代理。机器人提供可选的 Web 管理面板（默认关闭，见[第 6 节](#6-web-管理面板可选)）；需要域名时，Bot API 域名应指向独立的 Telegram Bot API Server，而面板域名应指向 `bot` 容器。

## 1. 程序介绍

### 主要功能

- 广告规则全局共享，处罚计次按群组和用户隔离。
- 检查新消息和编辑消息的 `text`、媒体 `caption`、隐藏链接以及内联按钮的文字和链接。
- 命中规则后删除消息，并记录删除成功或失败的审计记录。删除成功后机器人回复命中原因：内置广告库命中回复“该信息因匹配内置广告库已删除。”，仅自定义规则命中回复“该消息因匹配广告规则已删除。”；提示默认在 10 秒后由机器人自动删除（`NOTICE_TTL` 调整，设为 `0` 保留提示）。（内置库优先于自定义规则判定，因此两者都命中时按内置库提示。）
- 内置广告库 2.3（默认开启）：22 项离线组合检测，重点识别洗钱洗资、跑分资金通道（马车、红包车、人头号、押车等车队黑话与汇率点位、善后、保司法、卸货无忧等承诺）、催情迷情药品交易，同时覆盖博彩、诈骗招募（含挂机、短剧项目与日入、一天赚等收益承诺）、实名素材采集招募（拍照兼职、拍照采集、手持证件、人脸采集）、上门招嫖暗语（空降、上门、快餐、包夜加色情或价格标记）、群资源和账号号源买卖、广告代发与引流服务（代发、群发、站群、引流加支付结算或 VCC 虚拟卡）以及水货走私数码（水果机、港版美版、华强北加只要、特价、拿货）等广告。支持精选繁体、零宽字符、插符号、全角字符等变体；歧义词必须同时出现另一类独立信号并结合交易或招揽证据，维修、保洁等普通上门服务、工地日结用工和二手手机转让不受影响。邀请链接、短链、机器人和频道转发也需推广证据才删除。面板支持分类、总开关、逐项启停和无副作用的文本测试；审计保存库版本与简短命中原因。详细条件和能力边界见[内置广告库管理](docs/builtin-management.md)。
- 简介辅助检测（默认关闭）：开启后，对“看我主页”“点我头像”等主动引流消息尝试查询发送者简介；简介独立命中内置广告规则才删消息并计次。读取失败时跳过，审计标明“证据来源：用户简介”。可在面板「设置 → 运行设置」或群里 `/settings bio_check on|off` 随时开关，无需重启。
- 规则全局共享：任何群添加/修改的规则对所有群组即时生效（不再按群隔离）。面板「规则管理」页可一键导出全部规则为 JSON 备份。
- 广告三次封禁：同一用户在同一群组 24 小时内有 3 条不同消息命中广告（自定义规则和内置库均计入），自动永久封禁并踢出。同一消息编辑或重复投递在窗口内最多计一次；阈值由 `SPAM_STRIKE_LIMIT`/`SPAM_STRIKE_WINDOW` 调整。
- 在 Forum Topics 群组中沿用原消息的 `message_thread_id` 发送提示。
- 管理员可以在线新增、删除、启用、停用和测试规则。
- 审计日志保留 30 天，内容摘要最多 120 个字符，并保存 SHA-256 摘要。
- Poller 会处理进程停止期间积压的更新；业务处理失败会重试，超过次数后跳过该更新并继续处理后续消息。

### 不包含的功能

- 面板默认关闭，需要显式启用（见[第 6 节](#6-web-管理面板可选)）。
- 不扫描历史消息、贴纸、文件名、图像/OCR、语音或视频语音，也不合并多条消息判断广告。简介检测仅在显式开启后用于主动引流消息，不扫描全部群成员或处理入群申请。
- 不自动禁言；封禁使用上述可配置的广告计次策略，管理员不会因累计次数被自动封禁。
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
BOT_IMAGE=ghcr.io/kexue-aihao/telegram-adblock-transmit:v1.10.0
# 或：
# BOT_IMAGE=ghcr.io/kexue-aihao/telegram-adblock-transmit@sha256:<digest>
~~~

如果 GHCR 包是私有的，在 1Panel 的容器镜像仓库中添加 GHCR 凭据，或执行 `docker login ghcr.io`。Personal Access Token 至少需要 `read:packages` 权限。

## 3. 使用 1Panel 安装

以下菜单名称以常见 1Panel 版本为例。不同版本可能把“容器 -> 编排”显示为“容器 -> Compose”，功能相同。

### 第一步：一键部署（推荐）

在服务器终端或 1Panel「终端」中执行下面这一行命令。脚本自动识别部署目录中的本项目服务：首次运行执行安装，已有部署则保留配置并升级到 GitHub 最新正式版，不会一直停留在 `.env` 中记录的旧官方镜像版本：

~~~bash
bash <(curl -fsSL https://raw.githubusercontent.com/kexue-aihao/Telegram_Adblock_transmit/master/scripts/deploy.sh)
~~~

首次安装会依次询问 `BOT_TOKEN`、数据库密码（直接回车自动生成随机密码）以及是否启用 Web 管理面板（需要面板时输入 `y`，再设置面板用户名和密码）。升级时沿用原凭据、面板开关和监听地址，不再重复询问已配置项。也可以用环境变量跳过首次安装的交互：

~~~bash
cd /opt
BOT_TOKEN=替换为BotFather生成的Token \
POSTGRES_PASSWORD=替换为数据库密码 \
WEBUI_ENABLE=1 \
WEBUI_USERNAME=admin \
WEBUI_PASSWORD=替换为面板密码 \
bash <(curl -fsSL https://raw.githubusercontent.com/kexue-aihao/Telegram_Adblock_transmit/master/scripts/deploy.sh)
~~~

常用可选变量：`DEPLOY_DIR`（默认 `/opt/telegram-adblock-transmit`，已有部署应使用原目录）、`RELEASE_VERSION`（例如 `v1.10.0`）、`BOT_IMAGE`（完整镜像引用，优先级最高）、`WEBUI_ENABLE`（`1` 启用、`0` 关闭；升级时不传则保留）、`WEBUI_ADDR`（首次启用默认 `0.0.0.0:8080`）、`BIO_CHECK_ENABLED`（`true` / `false`，不传则保留；首次默认关闭）。

版本选择顺序：本次显式传入的 `BOT_IMAGE` → `RELEASE_VERSION` → 已配置的自定义镜像 → GitHub 最新正式版。默认运行会将旧的官方版本标签、摘要或 `latest` 替换成最新正式版固定标签，后续手动执行 Compose 仍使用该标签。需要保持某个版本或回滚时，每次运行一键脚本显式传入目标，例如：

~~~bash
RELEASE_VERSION=v1.10.0 bash <(curl -fsSL https://raw.githubusercontent.com/kexue-aihao/Telegram_Adblock_transmit/master/scripts/deploy.sh)
~~~

脚本检查本目录的 Compose 标签（包含已停止容器）及数据库卷，沿用已有项目名，避免创建另一套数据库。保留 `.env` 中的凭据和其他配置，不删除数据库卷、不执行 `down -v`；只有显式指定的面板/简介选项、镜像目标和项目名会更新。发现旧服务或数据库卷但原 `.env` 丢失时会停止，需先恢复原配置。

部署使用目标版本的 Compose 和环境变量模板；可以通过 `RAW_BASE` 显式指定模板来源。模板下载、配置校验及镜像拉取先在临时目录完成，失败时原文件和服务保持不变。更新前将原 `.env`、Compose 文件和模板备份到部署目录下的 `.backups/时间戳.随机后缀/`（仅当前用户可读）。这是配置备份，不包含 PostgreSQL 数据备份；自定义过网络、端口或挂载的部署应核对目标模板与原文件的差异。启动后检查 PostgreSQL 健康、bot 持续运行及实际镜像是否为目标版本。

脚本启动完成后：如果启用了面板，剩下的唯一工作就是配置 1Panel 反向代理（见[第 6 节](#6-web-管理面板可选)第 6.2 小节）；如果没启用面板，Bot 已经可以直接使用。以下第二步至第六步是等效的手工流程，供自定义部署或排查问题时参考。

### 第二步：确认 Docker

1. 登录 1Panel。
2. 打开“容器”，确认 Docker 服务状态为运行中。
3. 如果未安装 Docker，先在 1Panel 的容器设置或安装向导中安装 Docker Engine 和 Docker Compose。
4. 打开“主机 -> 防火墙”，只放行实际需要的端口。bot 容器不需要对外开放端口。

### 第三步：创建项目目录和文件

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
curl -fL -o docker-compose.pull.yml https://raw.githubusercontent.com/kexue-aihao/Telegram_Adblock_transmit/master/docker-compose.pull.yml
curl -fL -o .env.example https://raw.githubusercontent.com/kexue-aihao/Telegram_Adblock_transmit/master/.env.example
cp .env.example .env
chmod 600 .env
~~~

如果要部署指定版本，请把下载 URL 中的 `master` 换成对应的发布标签。

### 第四步：填写环境变量

在 1Panel 文件编辑器中打开 `/opt/telegram-adblock-transmit/.env`，至少填写：

~~~env
BOT_TOKEN=替换为BotFather生成的Token
POSTGRES_PASSWORD=生成一个足够长的随机密码
BOT_IMAGE=ghcr.io/kexue-aihao/telegram-adblock-transmit:v1.10.0
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

### 第五步：创建 1Panel Compose 编排

1. 打开“容器 -> 编排”（或“容器 -> Compose”）。
2. 点击“创建编排”。
3. 编排名称填写 `telegram-adblock-transmit`。
4. Compose 文件选择 `/opt/telegram-adblock-transmit/docker-compose.pull.yml`，或将文件内容粘贴到编辑器。
5. 环境文件选择 `/opt/telegram-adblock-transmit/.env`。如果当前 1Panel 没有单独的环境文件选项，把 `.env` 放在 Compose 文件同目录，Compose 会自动读取。
6. 保存并启动编排。

`docker-compose.pull.yml` 会拉取 bot 镜像、创建 PostgreSQL、创建持久化卷，并等待 PostgreSQL 健康检查通过。生产部署使用这个文件；`docker-compose.yml` 是源码构建配置，不要把两者同时作为同一个编排启动。

### 第六步：验证启动

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
| `/rule_regex` | 回复一条广告消息，自动转换为规则并加入规则库（别名 `/rule_regax`） |
| `/rule_list` | 列出本群规则和启停状态 |
| `/rule_remove <规则ID>` | 删除本群规则 |
| `/rule_enable <规则ID>` | 启用本群规则 |
| `/rule_disable <规则ID>` | 停用本群规则 |
| `/rule_test <文本>` | 测试文本命中的规则，不删除测试命令 |
| `/settings [项] [on\|off]` | 查看运行设置；`bio_check`、`builtin` 开关仅机器人所有者可改 |
| `/adlog [1-20]` | 查看最近的广告命中审计记录，默认 10 条 |

示例：

~~~text
/rule_add 免费.*领取
/rule_add https?://\S+\.example
/rule_test 免费领取 https://spam.example
~~~

`/rule_regex` 用于把一条漏检的广告快速变成规则：先**回复**那条广告消息，再发送 `/rule_regex`。机器人会按下列方式转换被回复消息，把生成的规则写入本群规则库并立即启用，同时在群里显示规则 ID 和完整正则：

- 保留原文措辞和顺序，去掉表情、标点等装饰，词与词之间允许少量插入字符，因此换个表情或加个分隔符仍能命中；
- 数字统一变成 `\p{Nd}+`（含全角数字），中文金额（三百、一万）变成数字类，金额和期数变化不影响命中；「千万」「万一」这类词保持原样；
- 链接替换为通用链接匹配，换域名或换短链仍能命中；
- `@用户名` 原样保留：把它泛化会让「联系 @某人」变成删除所有留联系方式的消息；
- 可匹配文字不足 4 个字符（例如只有链接、纯数字或表情）时拒绝生成，请改用 `/rule_add`；超过 512 字符的长广告只保留前半部分并在群里说明。

生成的规则和手工规则一样，可用 `/rule_test` 验证、`/rule_disable <ID>` 停用、`/rule_remove <ID>` 删除。

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

本小节的 Bot API 域名反向代理目标是自建 Bot API Server，不是 `bot` 容器（Web 面板的代理目标见[第 6 节](#6-web-管理面板可选)）：

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

## 6. Web 管理面板（可选）

面板与机器人运行在同一进程、同一容器内，共享数据库连接和内存规则缓存，因此通过面板修改规则后立即生效，无需重启机器人。

### 6.1 启用

面板默认关闭。在 `.env` 中设置以下变量后重建 bot 容器：

~~~env
WEBUI_ADDR=0.0.0.0:8080
WEBUI_USERNAME=admin
WEBUI_PASSWORD=replace-with-a-long-random-password
# 可选：固定会话签名密钥，让已登录会话跨重启保持；不设则每次重启后需重新登录。
# WEBUI_SESSION_SECRET=replace-with-32-plus-random-bytes
~~~

`WEBUI_ADDR` 留空则禁用面板（默认）。启用时必须同时设置 `WEBUI_USERNAME` 和 `WEBUI_PASSWORD`，否则进程启动报错。compose 默认把面板端口发布到宿主机回环地址 `127.0.0.1:8080`（仅本机可达，不暴露公网），供 1Panel 反向代理访问。

### 6.2 访问与 HTTPS

生产环境建议用 1Panel 反向代理以 HTTPS 访问面板，不要直接暴露 8080：

1. 在 DNS 中将 `panel.example.com` 的 A/AAAA 记录指向服务器（面板域名需与 Bot API 域名不同）。
2. 在 1Panel 打开“网站 -> 创建网站”，选择“反向代理”，上游地址填写 `http://127.0.0.1:8080`。填写后申请并启用 SSL。
3. 面板站点可以保留 access log，便于观察登录暴力尝试；面板请求 URI 中不含 Bot Token。

> **为什么是 `127.0.0.1` 而不是 `bot`**：1Panel 的 OpenResty 反代应用以 `network_mode: host` 运行（见 1Panel 应用商店 openresty 的 `docker-compose.yml`），nginx 共享宿主机网络栈：
> - host 网络下**无法解析 Docker 服务名**，上游填 `http://bot:8080` 会得到 `host not found in upstream`，导致面板 502；
> - compose 已默认把 bot 的 8080 发布到宿主机回环地址（`127.0.0.1:8080:8080`，仅本机可达），因此上游指向宿主机自己的 `http://127.0.0.1:8080` 即可。
> 如果不使用本机回环发布，等效替代：改用 `0.0.0.0:8080:8080` 发布并放行防火墙，上游填 `http://<服务器局域网IP>:8080`；或直接填 bot 容器在桥接网络的容器 IP（`docker inspect <容器ID> --format '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}'`，注意容器重建后 IP 会变化）。

仓库中的代理模板可直接参考：

- [Nginx 配置示例](deploy/nginx.panel.conf.example)
- [Caddy 配置示例](deploy/Caddyfile.panel.example)

不使用反向代理时，请将 `WEBUI_ADDR` 绑定为 `127.0.0.1:8080`，或确保防火墙只允许可信主机访问 8080。

### 6.3 功能

- **仪表盘**：今日命中、删除成功/失败、近 7 日和累计统计；支持 7/30/90 天趋势图与数据表，突出显示删除失败并链接到对应日志。日期统一按 UTC 统计。
- **内置广告库**：在「规则管理」页切换到「内置广告库」，按分类查看库版本、组合检测条件和当前生效数量，操作总开关或单项开关，并测试文本的命中项与原因。设置保存到数据库并即时生效，重启后保留；内置逻辑随程序版本维护，自定义正则仍在「自定义规则」中管理。管理 API 见 [内置广告库接口](docs/builtin-management.md)。
- **规则管理**：全局规则搜索、状态筛选、排序和分页；支持新增、编辑、启停、删除和 JSON 导出。测试匹配及保存均由服务端校验 RE2 正则；编辑未保存时提示确认，操作失败显示原因。通过面板新建的规则创建者为 `-1`。
- **审计日志**：按群组、日期、删除结果和命中规则筛选，提供日期快捷范围、分页和每页条数选择；支持摘要展开、复制、失败详情及内置库命中解释。数据库保留最多 120 个字符的原文摘要、原文哈希和简短证据标签，不新增完整消息或联系人存储；历史日志缺少详细解释时仍显示原命中 ID。
- **界面体验**：手机布局、浅色/深色主题、键盘导航、弹窗焦点管理、加载和重试状态；规则及日志筛选条件保留在 URL 中。
- **运行设置**：在「设置」页开关简介辅助检测、跨群管理权限，并维护机器人所有者用户 ID；与群里 `/settings` 共用同一份配置，保存在 `bot_settings` 表。
- **账号设置**：在「设置」页修改面板登录用户名和密码。凭据持久化到数据库 `panel_settings` 表（仅存 SHA-256 摘要，不存明文），一旦修改即优先于 `.env` 中的 `WEBUI_USERNAME` / `WEBUI_PASSWORD`；改密后所有已登录会话会退出，需重新登录。
- **缓存同步**：规则写入后会尝试刷新进程内缓存；新增、修改和启停接口返回缓存失败告警时，面板会提示规则可能尚未生效。规则页刷新按钮重新读取列表；全量缓存重载接口为经过认证的 `POST /api/cache/reload`。

本地样例预览和浏览器回归方法见 [WebUI 开发验证](docs/webui-development.md)。

健康检查（可配置到 1Panel 或外部探活，容器内无 shell）：

~~~bash
curl -fsS http://127.0.0.1:8080/healthz   # 返回 ok
~~~

## 7. 配置参考

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
| `WEBUI_ADDR` | 空（面板关闭） | 否 | Web 管理面板监听地址，如 `0.0.0.0:8080`；非空时面板启用 |
| `WEBUI_USERNAME` | 无 | 面板启用时必需 | 面板登录用户名，仅允许字母、数字、`_ . -`，1-64 字符 |
| `WEBUI_PASSWORD` | 无 | 面板启用时必需 | 面板登录密码，请使用长随机值 |
| `WEBUI_SESSION_SECRET` | 无 | 否 | 会话签名密钥；固定后重启不登出，不设则每次重启需重新登录 |
| `ADFILTER_ENABLED` | `true` | 否 | 内置广告库初始总开关；面板保存的数据库配置优先，关闭内置库不影响自定义规则 |
| `BIO_CHECK_ENABLED` | `false` | 否 | 简介辅助检测的初始值；面板或 `/settings` 保存后以数据库配置为准，无需重启 |
| `BOT_OWNER_IDS` | 空 | 否 | 机器人所有者用户 ID（逗号或空格分隔）；所有者无需是群管理员即可管理机器人并执行 `/settings`，也可在面板中维护 |
| `NOTICE_TTL` | `10s` | 否 | 删除提示在群里的留存时间，到期由机器人自动删除；设为 `0` 保留提示不自动删除 |
| `SPAM_STRIKE_LIMIT` | `3` | 否 | 同用户同群在窗口内命中广告次数达到该值即永久封禁踢出；<1 代表关闭 |
| `SPAM_STRIKE_WINDOW` | `24h` | 否 | 封禁计数的滚动时间窗口 |

端点安全规则：HTTPS 默认允许；HTTP 必须设置 `TELEGRAM_ALLOW_INSECURE_HTTP=true`，并且主机只能是回环地址、私网 IP、`localhost`、`host.docker.internal`、`gateway.docker.internal` 或单标签 Docker 服务名。

### 启用简介辅助检测

在面板「设置 → 运行设置」打开「简介辅助检测」，或在群里让机器人所有者发送 `/settings bio_check on`，保存后立即生效、无需重启。也可以用 `.env` 的 `BIO_CHECK_ENABLED=true` 设置初始值（面板或命令保存过配置后，以数据库中的值为准），确认内置库总开关及所需检测项已开启：

首次用环境变量启用时，需要重新创建 bot 容器（仅 `restart` 不会应用新的环境变量）：

~~~bash
docker compose -f docker-compose.pull.yml up -d --force-recreate bot
~~~

从源码构建的部署使用 `docker compose up -d --build bot`。1Panel 用户在编排中同步新增的环境变量并重新创建服务。直接运行 Go 程序时设置进程环境变量后重启。

先检查正文的内置库及自定义规则，未命中且正文或媒体说明包含本人资料引流时才查询简介。匿名管理员、频道身份、机器人和转发内容不参与简介辅助判断；引用、否定和反诈提醒不作为主动引流。普通网址、频道链接或正常发言不会仅因存在简介而被删除。

简介通过 Bot API `getChat(user_id)` 获取，接口可访问性及可选的 `bio` 字段决定覆盖范围，**不能保证读到所有群成员的简介**。每次查询最多 2 秒，全局每秒最多启动 1 次，超出配额跳过而不排队；遇到 `429` 遵循 `retry_after` 暂停查询。缓存按用户共享，最多 10,000 项，成功（含空简介）缓存 10 分钟，失败缓存 1 分钟。因此简介修改后可能最多延迟约 10 分钟被看到；缓存中的简介每次仍使用最新规则判断。

审计保存原消息摘要、规则版本和“消息主动引流／证据来源：用户简介”标签，不保存完整简介。正常日志记录启用状态及命中事件；临时使用 `LOG_LEVEL=DEBUG` 可观察查询的 `available`、`empty`、`unavailable`、`rate_limited`、`server_rate_limited` 状态，日志不输出完整简介。

建议先在测试部署启用，使用可读取简介的测试账号检查组合命中、普通简介不命中，以及无法读取简介时继续处理消息。停用时在面板关闭开关，或让所有者发送 `/settings bio_check off`；详细判定说明见[内置广告库管理](docs/builtin-management.md)。

### 机器人所有者与跨群管理

管理命令默认只对该群管理员开放。两种方式可以突破这一限制：

- **机器人所有者**：在 `BOT_OWNER_IDS` 或面板「设置 → 运行设置」中登记的用户 ID，无需是本群管理员即可管理机器人，并可在任意群执行 `/settings` 修改运行开关。Telegram 不提供查询机器人创建者的接口，因此机器人所有者必须显式配置；不确定自己的 ID 时，在群里发送任意管理命令，权限提示里会附带你的用户 ID。
- **跨群管理权限**：面板中的开关，打开后非本群管理员也可以执行管理命令。命令文本仍会先经过广告审核，因此不能用「命令前缀 + 广告正文」绕过删除；因为开启后任何群成员都能新增、停用或删除规则（包括写入 `.*` 这类宽泛规则），请只在完全信任群成员时打开，默认关闭。

## 8. 升级、回滚和备份

### 升级

1. 在 1Panel 中备份 `postgres_data` 卷，并保存 `.env` 的加密副本。
2. 可选项：如果你计划使用 Web 面板，先在 `.env` 设置 `WEBUI_ADDR`、`WEBUI_USERNAME`、`WEBUI_PASSWORD`（面板默认关闭，不设置不影响升级；启用后缺凭据会导致启动校验失败）。
3. 将 `BOT_IMAGE` 改为目标版本，例如 `v1.10.0`。也可重新运行一键部署脚本，自动选择最新正式版并完成升级。
4. 在编排详情中执行拉取镜像并重新创建/启动服务。
5. 查看 PostgreSQL 健康状态和 bot 日志，确认 bot 没有反复重启。

迁移在 bot 启动时自动执行，只会创建或更新所需结构，不会自动删除表或清空旧数据。升级前仍应保留数据库备份。

### 回滚

将 `BOT_IMAGE` 改回上一个已验证版本，重新拉取并重建 bot。不要把 `latest` 当作回滚版本。

### 备份

在 1Panel 的计划任务中配置：

- PostgreSQL 数据卷或数据库逻辑备份。
- `.env` 的加密备份，不要把明文 Token 上传到公共对象存储。
- 反向代理证书和站点配置。

恢复前先停止 bot，避免恢复期间产生新的写入。确认数据库恢复完成后再启动 bot。

## 9. 常见问题

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

这是正常的。该域名只用于转发 Telegram Bot API 请求，不提供网页。不要把 Bot API 域名的反向代理目标填写为 `bot` 容器；Web 管理面板使用独立的域名（见[第 6 节](#6-web-管理面板可选)）。

### 规则命令没有生效

只有群组管理员可以管理规则。规则使用 Go RE2 语法；不支持 lookaround、反向引用等 PCRE/Python 特性。规则数量和总长度达到上限时，命令会返回配额提示。

## 10. 源码运行和开发

本地运行需要 Go 1.26+ 和 PostgreSQL：

~~~bash
export BOT_TOKEN='...'
export DATABASE_URL='postgres://telegram_bot:password@127.0.0.1:5432/telegram_adblock?sslmode=disable'
export LOG_LEVEL=INFO
go run ./cmd/bot
~~~

本地开发默认关闭面板。如需在本地查看面板，额外设置：

~~~bash
export WEBUI_ADDR='127.0.0.1:8080'
export WEBUI_USERNAME='admin'
export WEBUI_PASSWORD='...'
# go run ./cmd/bot 后访问 http://127.0.0.1:8080
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

## 11. 安全清单

- Bot Token 只放在 1Panel 的 `.env` 或受控密钥存储中；会话密钥 `WEBUI_SESSION_SECRET` 同样只放在 `.env`。
- 生产环境固定 `BOT_IMAGE` 版本或摘要。
- 关闭 Bot API 反向代理的 access log，或确认已脱敏 URI。
- 不公开 PostgreSQL 5432 和 Bot API Server 8081。
- 面板通过 1Panel 反向代理以 HTTPS 访问，不要公开暴露 8080；不使用反向代理时绑定 `127.0.0.1` 或由防火墙限制。
- 为面板设置独立强密码，不要与服务器 SSH 或其他服务共用；面板用户名仅限字母数字 `_ . -`。
- 不设置 `WEBUI_SESSION_SECRET` 时，重启后旧会话全部失效（新会话需重新登录）。
- 同一 Bot Token 只运行一个 bot 编排实例。
- 定期备份 PostgreSQL 数据和加密后的 `.env`。
- 定期更新 1Panel、Docker、PostgreSQL 和镜像摘要。
- 发生 Token 泄露时，立即在 BotFather 重新生成 Token，并更新 `.env` 后重建 bot 容器。

## 12. 项目文件

- `docker-compose.yml`：本地源码构建配置。
- `docker-compose.pull.yml`：生产环境拉取 GHCR 镜像的配置。
- `.env.example`：环境变量模板。
- `scripts/deploy.ps1`：PowerShell 一键拉取和启动脚本，适合 Windows 管理机。
- `scripts/deploy.sh`：Linux 服务器（含 1Panel 终端）一键部署脚本，自动完成目录/模板/环境变量引导并启动容器（见第 3 节第一步）。
- `deploy/nginx.telegram-api.conf.example`：自建 Bot API Server 的 Nginx 反向代理模板。
- `deploy/Caddyfile.example`：Bot API Server 的 Caddy 反向代理模板。
- `deploy/nginx.panel.conf.example`：Web 管理面板的 Nginx 反向代理模板。
- `deploy/Caddyfile.panel.example`：Web 管理面板的 Caddy 反向代理模板。
- `internal/webui/`：Web 管理面板（HTTP 服务、认证、前端静态资源）。
- `migrations/`：数据库迁移，启动时自动执行。
