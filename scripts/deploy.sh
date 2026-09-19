#!/usr/bin/env bash
# Install or upgrade this project's Compose deployment. Run again to upgrade
# official images to the latest stable GitHub Release; credentials and volumes
# are retained. RELEASE_VERSION or BOT_IMAGE explicitly selects another target.
# bash <(curl -fsSL https://raw.githubusercontent.com/kexue-aihao/Telegram_Adblock_transmit/master/scripts/deploy.sh)
set -euo pipefail
umask 077

DEPLOY_DIR="${DEPLOY_DIR:-/opt/telegram-adblock-transmit}"
REPOSITORY="kexue-aihao/Telegram_Adblock_transmit"
OFFICIAL_IMAGE="ghcr.io/kexue-aihao/telegram-adblock-transmit"
STAGE_DIR=""
say() { printf '\033[1;32m[deploy]\033[0m %s\n' "$*"; }
err() { printf '\033[1;31m[deploy]\033[0m %s\n' "$*" >&2; }
die() { err "$*"; exit 1; }
cleanup() { [[ -z "$STAGE_DIR" ]] || rm -rf -- "$STAGE_DIR"; }
trap cleanup EXIT

# Read values without executing .env as a shell script. Existing secret lines
# are copied verbatim; only values explicitly managed below are rewritten.
get_key() {
  local v
  [[ -f "$ENV_FILE" ]] || return 0
  v="$(sed -n "s/^[[:space:]]*${1}[[:space:]]*=[[:space:]]*//p" "$ENV_FILE" | tail -n 1)"
  v="${v%$'\r'}"
  if [[ "$v" == \"*\" || "$v" == \'*\' ]]; then v="${v:1:${#v}-2}"; fi
  printf '%s' "$v"
}
have_key() {
  local v
  v="$(get_key "$1")"
  [[ -n "$v" && "$v" != replace-with-* && "$v" != change-this-* ]]
}
set_key() {
  [[ "$2" != *$'\n'* && "$2" != *$'\r'* ]] || die "$1 不能包含换行。"
  sed -i "/^[[:space:]]*${1}[[:space:]]*=/d" "$ENV_FILE"
  printf '\n%s=%s\n' "$1" "$2" >> "$ENV_FILE"
}
rand_hex() {
  if command -v openssl >/dev/null 2>&1; then openssl rand -hex "${1:-16}"
  else head -c "${1:-16}" /dev/urandom | od -An -tx1 | tr -d ' \n'; fi
}

command -v docker >/dev/null 2>&1 || die "未检测到 docker，请先安装 Docker。"
command -v curl >/dev/null 2>&1 || die "未检测到 curl，请先安装 curl。"
docker info >/dev/null 2>&1 || die "无法连接 Docker，请确认服务已启动且当前用户有权限。"
if docker compose version >/dev/null 2>&1; then DC=(docker compose)
elif command -v docker-compose >/dev/null 2>&1 && docker-compose version >/dev/null 2>&1; then DC=(docker-compose)
else die "未检测到 Docker Compose，请先安装 Compose。"; fi

mkdir -p "$DEPLOY_DIR"
cd "$DEPLOY_DIR"
DEPLOY_DIR="$(pwd -P)"
ENV_FILE="$DEPLOY_DIR/.env"
PROJECT_NAME="${COMPOSE_PROJECT_NAME:-$(get_key COMPOSE_PROJECT_NAME)}"
UPGRADE_MODE=0

# Inspect stopped containers too. Only services labelled for this deployment
# directory belong to this stack; a different project's bot is never adopted.
CONTAINER_IDS="$(docker ps -aq --filter "label=com.docker.compose.project.working_dir=$DEPLOY_DIR")"
DETECTED_PROJECT=""
for cid in $CONTAINER_IDS; do
  service="$(docker inspect -f '{{index .Config.Labels "com.docker.compose.service"}}' "$cid")"
  [[ "$service" == bot || "$service" == postgres ]] || continue
  project="$(docker inspect -f '{{index .Config.Labels "com.docker.compose.project"}}' "$cid")"
  [[ -n "$project" && "$project" != '<no value>' ]] || die "已有容器缺少 Compose 项目标签，请检查原编排。"
  [[ -z "$DETECTED_PROJECT" || "$DETECTED_PROJECT" == "$project" ]] || die "该目录存在多个 Compose 项目，请使用各自的部署目录升级。"
  DETECTED_PROJECT="$project"
  UPGRADE_MODE=1
done
if [[ -n "$DETECTED_PROJECT" ]]; then
  [[ -z "$PROJECT_NAME" || "$PROJECT_NAME" == "$DETECTED_PROJECT" ]] || die "COMPOSE_PROJECT_NAME 与已有服务不同，停止以避免创建另一套数据库。"
  PROJECT_NAME="$DETECTED_PROJECT"
fi
if [[ -z "$PROJECT_NAME" ]]; then
  PROJECT_NAME="$(basename "$DEPLOY_DIR" | tr '[:upper:]' '[:lower:]' | tr -cd 'a-z0-9_-')"
  PROJECT_NAME="$(printf '%s' "$PROJECT_NAME" | sed 's/^[^a-z0-9]*//')"
fi
[[ "$PROJECT_NAME" =~ ^[a-z0-9][a-z0-9_-]*$ ]] || die "无效的 Compose 项目名，请设置 COMPOSE_PROJECT_NAME。"
VOLUMES="$(docker volume ls -q --filter "label=com.docker.compose.project=$PROJECT_NAME" --filter 'label=com.docker.compose.volume=postgres_data')"
[[ -z "$VOLUMES" ]] || UPGRADE_MODE=1
if [[ "$UPGRADE_MODE" == 1 && ! -f .env ]]; then
  die "检测到已有服务或数据库卷，但 .env 丢失。请先恢复原配置，脚本不会生成新密码覆盖旧部署。"
fi
# Retained configuration also identifies a deployment whose containers were removed.
if have_key BOT_TOKEN && have_key POSTGRES_PASSWORD; then UPGRADE_MODE=1; fi
if [[ "$UPGRADE_MODE" == 1 ]]; then
  have_key BOT_TOKEN || die "升级需要原 BOT_TOKEN，请先恢复 .env。"
  have_key POSTGRES_PASSWORD || die "升级需要原 POSTGRES_PASSWORD，请先恢复 .env。"
  say "检测到已有部署（项目 $PROJECT_NAME），执行升级并保留现有配置和数据库卷。"
else
  say "未检测到已有部署（项目 $PROJECT_NAME），执行初始安装。"
fi

# Explicit image > explicit release > existing custom image > latest official
# release. A failed version lookup stops before any deployment files change.
CURRENT_IMAGE="$(get_key BOT_IMAGE)"
TARGET_VERSION="${RELEASE_VERSION:-}"
if [[ -n "${BOT_IMAGE+x}" ]]; then
  [[ -n "$BOT_IMAGE" ]] || die "显式 BOT_IMAGE 不能为空。"
  TARGET_IMAGE="$BOT_IMAGE"
elif [[ -n "$TARGET_VERSION" ]]; then
  TARGET_IMAGE="$OFFICIAL_IMAGE:$TARGET_VERSION"
elif [[ -n "$CURRENT_IMAGE" && "$CURRENT_IMAGE" != "$OFFICIAL_IMAGE" && "$CURRENT_IMAGE" != "$OFFICIAL_IMAGE":* && "$CURRENT_IMAGE" != "$OFFICIAL_IMAGE"@* ]]; then
  TARGET_IMAGE="$CURRENT_IMAGE"
  say "保留自定义镜像；如需切换官方发布版，请指定 RELEASE_VERSION。"
else
  say "查询 GitHub 最新正式版本……"
  RELEASE_URL="$(curl -fsSL --retry 2 --connect-timeout 10 --max-time 60 -o /dev/null -w '%{url_effective}' "https://github.com/$REPOSITORY/releases/latest")" || die "查询最新正式版失败，原部署未更改；可用 RELEASE_VERSION=vX.Y.Z 指定版本重试。"
  [[ "$RELEASE_URL" == "https://github.com/$REPOSITORY/releases/tag/"* ]] || die "GitHub 未返回正式版本，请指定 RELEASE_VERSION。"
  TARGET_VERSION="${RELEASE_URL##*/}"
  TARGET_IMAGE="$OFFICIAL_IMAGE:$TARGET_VERSION"
fi
if [[ -n "$TARGET_VERSION" ]]; then
  [[ "$TARGET_VERSION" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]] || die "RELEASE_VERSION 必须是 vX.Y.Z 格式的正式版本。"
elif [[ "$TARGET_IMAGE" == "$OFFICIAL_IMAGE":v* ]]; then
  image_tag="${TARGET_IMAGE##*:}"
  if [[ "$image_tag" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]; then TARGET_VERSION="$image_tag"; fi
fi
[[ "$TARGET_IMAGE" =~ ^[a-zA-Z0-9][a-zA-Z0-9./:_@-]+$ ]] || die "BOT_IMAGE 不是有效的镜像引用。"
RAW_BASE="${RAW_BASE:-https://raw.githubusercontent.com/$REPOSITORY/${TARGET_VERSION:-master}}"
say "镜像目标：${CURRENT_IMAGE:-未配置} → $TARGET_IMAGE"

# Stage downloads/configuration. Failed downloads, validation or pulls leave the
# active .env and Compose file untouched. Backups contain secrets, so use 0700/0600.
STAGE_DIR="$(mktemp -d "$DEPLOY_DIR/.deploy.XXXXXX")"
say "下载目标版本部署模板……"
curl -fsSL --retry 2 --connect-timeout 10 --max-time 60 -o "$STAGE_DIR/docker-compose.pull.yml" "$RAW_BASE/docker-compose.pull.yml"
curl -fsSL --retry 2 --connect-timeout 10 --max-time 60 -o "$STAGE_DIR/.env.example" "$RAW_BASE/.env.example"
if [[ -f .env ]]; then cp .env "$STAGE_DIR/.env"; else cp "$STAGE_DIR/.env.example" "$STAGE_DIR/.env"; fi
ENV_FILE="$STAGE_DIR/.env"
chmod 600 "$ENV_FILE"
set_key BOT_IMAGE "$TARGET_IMAGE"
set_key COMPOSE_PROJECT_NAME "$PROJECT_NAME"

if have_key BOT_TOKEN; then say "保留已配置的 BOT_TOKEN"
else
  if [[ -z ${BOT_TOKEN:-} ]]; then read -r -p '[deploy] BOT_TOKEN: ' BOT_TOKEN; fi
  [[ -n "$BOT_TOKEN" ]] || die "BOT_TOKEN 不能为空。"
  set_key BOT_TOKEN "$BOT_TOKEN"
fi
if have_key POSTGRES_PASSWORD; then say "保留已配置的数据库密码"
else
  if [[ -z ${POSTGRES_PASSWORD:-} ]]; then
    read -r -p '[deploy] 数据库密码（回车自动生成）: ' POSTGRES_PASSWORD
    POSTGRES_PASSWORD="${POSTGRES_PASSWORD:-$(rand_hex 16)}"
  fi
  [[ "$POSTGRES_PASSWORD" =~ ^[A-Za-z0-9._-]+$ ]] || die "新数据库密码请使用字母、数字、点、下划线或连字符。"
  set_key POSTGRES_PASSWORD "$POSTGRES_PASSWORD"
fi

if [[ -z "${WEBUI_ENABLE+x}" ]]; then
  if [[ "$UPGRADE_MODE" == 1 ]]; then
    if have_key WEBUI_ADDR; then WEBUI_ENABLE=1; else WEBUI_ENABLE=0; fi
    say "保留现有 Web 管理面板开关和凭据"
  else
    read -r -p '[deploy] 是否启用 Web 管理面板？[y/N] ' yn
    if [[ "$yn" =~ ^[Yy] ]]; then WEBUI_ENABLE=1; else WEBUI_ENABLE=0; fi
  fi
fi
[[ "$WEBUI_ENABLE" == 0 || "$WEBUI_ENABLE" == 1 ]] || die "WEBUI_ENABLE 必须为 0 或 1。"
if [[ "$WEBUI_ENABLE" == 1 ]]; then
  PANEL_ADDR="${WEBUI_ADDR:-$(get_key WEBUI_ADDR)}"
  set_key WEBUI_ADDR "${PANEL_ADDR:-0.0.0.0:8080}"
  if ! have_key WEBUI_USERNAME; then
    if [[ -z ${WEBUI_USERNAME:-} ]]; then
      read -r -p '[deploy] 面板用户名 [admin]: ' WEBUI_USERNAME
      WEBUI_USERNAME="${WEBUI_USERNAME:-admin}"
    fi
    set_key WEBUI_USERNAME "$WEBUI_USERNAME"
  fi
  if ! have_key WEBUI_PASSWORD; then
    if [[ -z ${WEBUI_PASSWORD:-} ]]; then
      read -r -p '[deploy] 面板密码（回车自动生成并保存到 .env）: ' WEBUI_PASSWORD
      WEBUI_PASSWORD="${WEBUI_PASSWORD:-$(rand_hex 16)}"
    fi
    set_key WEBUI_PASSWORD "$WEBUI_PASSWORD"
  fi
  if ! have_key WEBUI_SESSION_SECRET; then set_key WEBUI_SESSION_SECRET "${WEBUI_SESSION_SECRET:-$(rand_hex 32)}"; fi
else
  set_key WEBUI_ADDR ""
fi
if [[ -n "${BIO_CHECK_ENABLED+x}" ]]; then
  [[ "$BIO_CHECK_ENABLED" == true || "$BIO_CHECK_ENABLED" == false ]] || die "BIO_CHECK_ENABLED 必须为 true 或 false。"
  set_key BIO_CHECK_ENABLED "$BIO_CHECK_ENABLED"
fi
if [[ -n "${BOT_OWNER_IDS+x}" ]]; then
  [[ "$BOT_OWNER_IDS" =~ ^[0-9]+([,;[:space:]]+[0-9]+)*$ ]] || die "BOT_OWNER_IDS 必须是逗号或空格分隔的正整数用户 ID。"
  set_key BOT_OWNER_IDS "$BOT_OWNER_IDS"
fi

# Credentials from an ambient shell must not override the retained .env during
# upgrades (especially a different database password). Explicit image/panel/bio
# choices above are already saved in the staged file.
compose() {
  env -u BOT_IMAGE -u BOT_TOKEN -u POSTGRES_PASSWORD -u WEBUI_ADDR -u WEBUI_USERNAME \
    -u WEBUI_PASSWORD -u WEBUI_SESSION_SECRET -u BIO_CHECK_ENABLED -u BOT_OWNER_IDS \
    "${DC[@]}" --project-directory "$DEPLOY_DIR" -p "$PROJECT_NAME" \
    -f "$COMPOSE_FILE" --env-file "$ENV_FILE" "$@"
}
COMPOSE_FILE="$STAGE_DIR/docker-compose.pull.yml"
compose config --quiet
say "拉取目标镜像（失败不会停止原服务）……"
compose pull

BACKUP_DIR=""
if [[ -f .env || -f docker-compose.pull.yml ]]; then
  mkdir -p .backups
  chmod 700 .backups
  BACKUP_DIR="$(mktemp -d "$DEPLOY_DIR/.backups/$(date +%Y%m%d-%H%M%S).XXXXXX")"
  for file in .env docker-compose.pull.yml .env.example; do
    if [[ -f "$file" ]]; then cp "$file" "$BACKUP_DIR/$file"; chmod 600 "$BACKUP_DIR/$file"; fi
  done
  say "原配置已备份到 $BACKUP_DIR（不含数据库数据备份）"
fi
mv "$STAGE_DIR/.env" .env
mv "$STAGE_DIR/docker-compose.pull.yml" docker-compose.pull.yml
mv "$STAGE_DIR/.env.example" .env.example
ENV_FILE="$DEPLOY_DIR/.env"
COMPOSE_FILE="$DEPLOY_DIR/docker-compose.pull.yml"

# Compose updates changed services in place; project name and volume identity
# remain stable. Never down -v, prune volumes, or force-recreate the database.
say "启动或升级容器……"
compose up -d
say "等待 PostgreSQL 健康与 bot 持续运行（最长约 3 分钟）……"
READY=0
STABLE=0
for ((attempt=0; attempt<60; attempt++)); do
  pg_id="$(compose ps -q postgres 2>/dev/null || true)"
  bot_id="$(compose ps -q bot 2>/dev/null || true)"
  PG="$(docker inspect -f '{{.State.Health.Status}}' "$pg_id" 2>/dev/null || true)"
  BOT="$(docker inspect -f '{{.State.Status}}' "$bot_id" 2>/dev/null || true)"
  BOT_IMAGE_RUNNING="$(docker inspect -f '{{.Config.Image}}' "$bot_id" 2>/dev/null || true)"
  if [[ "$PG" == healthy && "$BOT" == running && "$BOT_IMAGE_RUNNING" == "$TARGET_IMAGE" ]]; then
    STABLE=$((STABLE+1))
    if [[ "$STABLE" -ge 3 ]]; then READY=1; break; fi
  else STABLE=0; fi
  sleep 3
done
if [[ "$READY" != 1 ]]; then
  err "服务未就绪或运行镜像不是目标版本，请检查："
  err "  cd $DEPLOY_DIR && ${DC[*]} -p $PROJECT_NAME -f docker-compose.pull.yml --env-file .env logs --tail=80 bot"
  [[ -z "$BACKUP_DIR" ]] || err "原配置保存在 $BACKUP_DIR；脚本未删除数据库卷。"
  exit 1
fi
if [[ "$UPGRADE_MODE" == 1 ]]; then say "✓ 升级完成，已保留数据库卷及原有凭据。"
else say "✓ 初始安装完成。"; fi
say "运行镜像：$BOT_IMAGE_RUNNING"
say "状态检查：cd $DEPLOY_DIR && ${DC[*]} -p $PROJECT_NAME -f docker-compose.pull.yml --env-file .env ps"
say "日志：cd $DEPLOY_DIR && ${DC[*]} -p $PROJECT_NAME -f docker-compose.pull.yml --env-file .env logs --tail=50 bot"
if [[ "$WEBUI_ENABLE" == 1 ]]; then
  say "Web 面板已启用，原反向代理可继续使用；默认模板上游为 http://127.0.0.1:8080。"
  say "面板凭据位于 .env 或已保存的面板设置中。"
else say "Web 面板未启用；需要时可使用 WEBUI_ENABLE=1 再次运行此脚本。"; fi
BIO_STATUS="$(get_key BIO_CHECK_ENABLED)"
say "简介辅助检测：${BIO_STATUS:-false}（初始值；面板“运行设置”或 /settings 可随时修改）。"
OWNER_STATUS="$(get_key BOT_OWNER_IDS)"
say "机器人所有者：${OWNER_STATUS:-未设置}；可在面板“设置 → 运行设置”中维护。"
say "部署目录：$DEPLOY_DIR（.env 及备份包含机密，请勿外传）"
