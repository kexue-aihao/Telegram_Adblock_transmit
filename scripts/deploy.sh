#!/usr/bin/env bash
#
# 一键部署 Telegram 群组广告拦截机器人（Linux + Docker / 1Panel）
#
# 做什么:
#   1. 创建部署目录（默认 /opt/telegram-adblock-transmit，可用 DEPLOY_DIR 覆盖）
#   2. 下载远端 docker-compose.pull.yml 与 .env.example 模板
#   3. 引导填写 BOT_TOKEN、自动生成数据库密码，可选启用 Web 管理面板
#   4. 拉取镜像并启动 postgres 与 bot，等待健康就绪
#   之后只需要在 1Panel 里为 Web 面板配置反向代理即可使用。
#
# 用法（在服务器终端或 1Panel 终端中执行）:
#   bash <(curl -fsSL https://raw.githubusercontent.com/kexue-aihao/telegram-adblock-transmit/master/scripts/deploy.sh)
#
# 非交互（环境变量优先，跳过对应交互提示）:
#   BOT_TOKEN=... POSTGRES_PASSWORD=... WEBUI_ENABLE=1 \
#     WEBUI_USERNAME=admin WEBUI_PASSWORD=... \
#     bash <(curl -fsSL https://raw.githubusercontent.com/kexue-aihao/telegram-adblock-transmit/master/scripts/deploy.sh)
#
# 可选环境变量: DEPLOY_DIR RAW_BASE BOT_IMAGE WEBUI_ENABLE WEBUI_ADDR
#               WEBUI_USERNAME WEBUI_PASSWORD WEBUI_SESSION_SECRET

set -euo pipefail

DEPLOY_DIR="${DEPLOY_DIR:-/opt/telegram-adblock-transmit}"
RAW_BASE="${RAW_BASE:-https://raw.githubusercontent.com/kexue-aihao/telegram-adblock-transmit/master}"

say() { printf '\033[1;32m[deploy]\033[0m %s\n' "$*"; }
err() { printf '\033[1;31m[deploy]\033[0m %s\n' "$*" >&2; }

# have_key <KEY>: .env 中是否已有非占位符的可用值
have_key() {
  local k="$1" v
  v="$(sed -n "s/^${k}=//p" "$DEPLOY_DIR/.env" | tail -n 1)"
  [[ -n "$v" ]] || return 1
  case "$v" in
    replace-with-*|change-this-*) return 1 ;;
    *) return 0 ;;
  esac
}

# set_key <KEY> <VALUE>: 移除旧生效行后追加，避免 compose 读到重复定义
set_key() {
  sed -i "/^${1}=/d" "$DEPLOY_DIR/.env"
  printf '%s=%s\n' "$1" "$2" >> "$DEPLOY_DIR/.env"
}

# 随机 hex 生成器（openssl 优先，降级用 /dev/urandom）
rand_hex() {
  local n="${1:-16}"
  if command -v openssl >/dev/null 2>&1; then
    openssl rand -hex "$n"
  else
    head -c "$n" /dev/urandom | od -An -tx1 | tr -d ' \n'
  fi
}

# ── 0. 环境检查 ────────────────────────────────────────────────
command -v docker >/dev/null 2>&1 || { err "未检测到 docker，请先在 1Panel 容器设置中安装 Docker。"; exit 1; }
if docker compose version >/dev/null 2>&1; then
  DC=(docker compose)
elif docker-compose version >/dev/null 2>&1; then
  DC=(docker-compose)
else
  err "未检测到 docker compose 插件，请安装 Compose v2（或传统 docker-compose）。"; exit 1
fi

# ── 1. 目录与模板文件 ──────────────────────────────────────────
mkdir -p "$DEPLOY_DIR" || { err "无法创建 $DEPLOY_DIR（请以 root 或具备 sudo 权限的用户执行）。"; exit 1; }
cd "$DEPLOY_DIR"

say "下载远端模板到 $DEPLOY_DIR ..."
curl -fsSL -o docker-compose.pull.yml "$RAW_BASE/docker-compose.pull.yml"
curl -fsSL -o .env.example "$RAW_BASE/.env.example"

if [[ ! -f .env ]]; then
  cp .env.example .env
  chmod 600 .env
  say ".env 不存在，已从模板生成"
fi
chmod 600 .env

# ── 2. BOT_TOKEN ──────────────────────────────────────────────
if have_key BOT_TOKEN; then
  say "BOT_TOKEN 已配置，跳过"
else
  if [[ -z ${BOT_TOKEN:-} ]]; then
    read -r -p "[deploy] 请粘贴 BOT_TOKEN（@BotFather 创建时获取）: " BOT_TOKEN
  fi
  [[ -n "$BOT_TOKEN" ]] || { err "BOT_TOKEN 不能为空。"; exit 1; }
  set_key BOT_TOKEN "$BOT_TOKEN"
  say "BOT_TOKEN 已写入 .env"
fi

# ── 3. POSTGRES_PASSWORD ──────────────────────────────────────
if have_key POSTGRES_PASSWORD; then
  say "POSTGRES_PASSWORD 已配置，跳过"
else
  DB_DEFAULT="$(rand_hex 16)"
  if [[ -z ${POSTGRES_PASSWORD:-} ]]; then
    read -r -p "[deploy] 数据库密码（直接回车自动生成随机密码）: " pw
    POSTGRES_PASSWORD="${pw:-$DB_DEFAULT}"
  fi
  [[ -n "$POSTGRES_PASSWORD" ]] || POSTGRES_PASSWORD="$DB_DEFAULT"
  set_key POSTGRES_PASSWORD "$POSTGRES_PASSWORD"
  say "数据库密码已写入 .env"
  if ! [[ "$POSTGRES_PASSWORD" =~ ^[A-Za-z0-9._-]+$ ]]; then
    err "警告: 当前密码含 URL 特殊字符（@ : / # 等），将拼进连接串；务必确认已 URL 编码或用 字母数字 `._-` 重写。"
  fi
fi

# ── 4. Web 管理面板（可选）─────────────────────────────────────
if [[ -z ${WEBUI_ENABLE:-} ]]; then
  read -r -p "[deploy] 是否启用 Web 管理面板？（需要反向代理访问，输入 y 启用）[y/N] " yn
  [[ "$yn" =~ ^[Yy] ]] && WEBUI_ENABLE=1 || WEBUI_ENABLE=0
fi

PANEL_ENABLED=0
if [[ "$WEBUI_ENABLE" == "1" ]]; then
  PANEL_ENABLED=1

  set_key WEBUI_ADDR "${WEBUI_ADDR:-0.0.0.0:8080}"
  say "WEBUI_ADDR 已设置为 ${WEBUI_ADDR:-0.0.0.0:8080}"

  if ! have_key WEBUI_USERNAME; then
    if [[ -z ${WEBUI_USERNAME:-} ]]; then
      read -r -p "[deploy] 面板用户名[默认 admin]（字母、数字、_ . -，1-64 字符）: " u
      WEBUI_USERNAME="${u:-admin}"
    fi
    set_key WEBUI_USERNAME "$WEBUI_USERNAME"
  fi
  if ! have_key WEBUI_PASSWORD; then
    PANEL_PASS_DEFAULT="$(rand_hex 16)"
    if [[ -z ${WEBUI_PASSWORD:-} ]]; then
      read -r -p "[deploy] 面板密码（直接回车自动生成: $PANEL_PASS_DEFAULT）: " p
      WEBUI_PASSWORD="${p:-$PANEL_PASS_DEFAULT}"
    fi
    set_key WEBUI_PASSWORD "$WEBUI_PASSWORD"
  fi
  if ! have_key WEBUI_SESSION_SECRET; then
    if [[ -z ${WEBUI_SESSION_SECRET:-} ]]; then
      WEBUI_SESSION_SECRET="$(rand_hex 32)"
    fi
    set_key WEBUI_SESSION_SECRET "$WEBUI_SESSION_SECRET"
    say "已生成 WEBUI_SESSION_SECRET（重启面板仍保持登录）"
  fi
fi

# 可选：固定镜像版本
if [[ -n ${BOT_IMAGE:-} ]] && ! have_key BOT_IMAGE; then
  set_key BOT_IMAGE "$BOT_IMAGE"
fi

# ── 5. 拉取并启动 ──────────────────────────────────────────────
say "拉取镜像……"
"${DC[@]}" -f docker-compose.pull.yml --env-file .env pull

say "启动容器……"
"${DC[@]}" -f docker-compose.pull.yml --env-file .env up -d

# ── 6. 等待健康 ────────────────────────────────────────────────
say "等待 PostgreSQL 健康与 bot 就绪（最长约 3 分钟）……"
READY=0
for _ in $(seq 1 60); do
  PG="$(docker inspect -f '{{.State.Health.Status}}' "$("${DC[@]}" -f docker-compose.pull.yml --env-file .env ps -q postgres 2>/dev/null)" 2>/dev/null || true)"
  BOT="$(docker inspect -f '{{.State.Status}}' "$("${DC[@]}" -f docker-compose.pull.yml --env-file .env ps -q bot 2>/dev/null)" 2>/dev/null || true)"
  if [[ "$PG" == "healthy" && "$BOT" == "running" ]]; then
    READY=1
    break
  fi
  printf '\r  等待就绪 postgres=%s bot=%s   ' "$PG" "$BOT"
  sleep 3
 done
printf '\n'

if [[ "$READY" != "1" ]]; then
  err "服务未在预期时间内就绪。请检查:"
  err "  ${DC[*]} -f $DEPLOY_DIR/docker-compose.pull.yml --env-file $DEPLOY_DIR/.env ps --all"
  err "  ${DC[*]} -f $DEPLOY_DIR/docker-compose.pull.yml --env-file $DEPLOY_DIR/.env logs --tail=80 bot"
  exit 1
fi

# ── 7. 结果 ────────────────────────────────────────────────────
say "✓ 部署完成。bot 与 postgres 均已运行。"
say "状态检查:"
say "  cd $DEPLOY_DIR && ${DC[*]} -f docker-compose.pull.yml --env-file .env ps"
say "日志:"
say "  cd $DEPLOY_DIR && ${DC[*]} -f docker-compose.pull.yml --env-file .env logs --tail=50 bot"

if [[ "$PANEL_ENABLED" == "1" ]]; then
  say ""
  say "Web 管理面板已启用，接下来只需配置反向代理:"
  say "  1. 在 1Panel「网站 -> 创建网站 -> 反向代理」新建面板域名（需与 Bot 无关的独立域名）。"
  say "  2. 上游地址填 http://bot:8080（1Panel 代理容器在 Docker 网络时），申请并启用 SSL 证书。"
  say "  3. 访问 https://<面板域名> 登录。若登录失效，secret 已固定在 .env 的 WEBUI_SESSION_SECRET。"
  say "面板地址: http://<服务器IP>:8080（仅供本机/内网临时访问，生产请走上面的 HTTPS 反向代理）"
else
  say ""
  say "Web 管理面板未启用。如需启用，编辑 $DEPLOY_DIR/.env 填入 WEBUI_ADDR / WEBUI_USERNAME / WEBUI_PASSWORD，然后执行:"
  say "  cd $DEPLOY_DIR && ${DC[*]} -f docker-compose.pull.yml --env-file .env up -d --force-recreate bot"
fi

say ""
say "首次使用: 将 Bot 加入群组并设为管理员，在群内执行 /rule_add <正则>、/rule_list 等命令（详见 README 第 4 节）。"
if grep -q '^BOT_IMAGE=.*latest' "$DEPLOY_DIR/.env" 2>/dev/null; then
  say "提示: .env 中 BOT_IMAGE 当前为 latest，生产建议固定为具体版本（如 v1.2.0）以便回滚。"
fi
say "部署目录: $DEPLOY_DIR（.env 为 600 权限，包含机密，请勿外传）"