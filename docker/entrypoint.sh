#!/bin/sh
set -e

CONFIG_PATH="${CONFIG_PATH:-/data/config.yaml}"
mkdir -p "$(dirname "$CONFIG_PATH")"

# Export defaults for optional vars so envsubst can substitute
export TIME_ZONE="${TIME_ZONE:-UTC}"
export TIME_FORMAT="${TIME_FORMAT:-02 Jan, 2006 - Mon @ 15:04}"
export DEBUG_MODE="${DEBUG_MODE:-false}"
export TELEGRAM_SKIP_SETTING_COMMANDS="${TELEGRAM_SKIP_SETTING_COMMANDS:-false}"
export TELEGRAM_SKIP_STARTUP_MESSAGE="${TELEGRAM_SKIP_STARTUP_MESSAGE:-false}"
export TELEGRAM_SILENT_CONFIRMATION="${TELEGRAM_SILENT_CONFIRMATION:-true}"
export TELEGRAM_CONFIRMATION_TYPE="${TELEGRAM_CONFIRMATION_TYPE:-emoji}"
export WHATSAPP_SESSION_NAME="${WHATSAPP_SESSION_NAME:-watgbridge}"

if [ -z "$TELEGRAM_BOT_TOKEN" ] || [ -z "$TELEGRAM_OWNER_ID" ] || [ -z "$TELEGRAM_TARGET_CHAT_ID" ]; then
  echo "Error: TELEGRAM_BOT_TOKEN, TELEGRAM_OWNER_ID and TELEGRAM_TARGET_CHAT_ID must be set." >&2
  exit 1
fi

if [ -f /docker/config.yaml.tpl ]; then
  envsubst < /docker/config.yaml.tpl > "$CONFIG_PATH"
else
  echo "Error: /docker/config.yaml.tpl not found." >&2
  exit 1
fi

exec ./watgbridge "$CONFIG_PATH"
