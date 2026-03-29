#!/usr/bin/env bash
set -e

# From repo root: generates config.yaml from docker/config.yaml.tpl, then docker compose up -d
#
# Single bridge (legacy):
#   ./run.sh --telegram-bot-token x --telegram-owner-id 123 --telegram-target-chat-id -100… [--build]
#
# Multi-tenant (main control bot only; users add bridges via /bridge_add):
#   MODE=multi ./run.sh --mode multi --telegram-main-bot-token 'MAIN_BOT_TOKEN' [--build]
#   # Per-bridge bot_token / owner / target are not used by the main process (placeholders in config).

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

usage() {
  cat <<EOF
Usage: $0 [OPTIONS] [--build]

Mode (env MODE or --mode): single (default) | multi

Single mode — required:
  --telegram-bot-token TOKEN
  --telegram-owner-id ID
  --telegram-target-chat-id CHAT_ID

Multi mode — required:
  --telegram-main-bot-token TOKEN   (or env TELEGRAM_MAIN_BOT_TOKEN)

Optional:
  --mode single|multi
  --telegram-main-bot-token TOKEN
  --telegram-skip-setting-commands true|false
  --telegram-skip-startup-message true|false
  --telegram-silent-confirmation true|false
  --telegram-confirmation-type TYPE
  --whatsapp-session-name NAME
  --time-zone ZONE
  --time-format FORMAT
  --debug true|false
  --config-path PATH
  --version VERSION    Version string for BotMeta topic (default: git describe / short HEAD)

  --build    docker compose up -d --build
  --help     This help
EOF
  exit 0
}

BUILD_FLAG=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    --mode)
      export MODE="$2"
      shift 2
      ;;
    --telegram-main-bot-token)
      export TELEGRAM_MAIN_BOT_TOKEN="$2"
      shift 2
      ;;
    --telegram-bot-token)
      export TELEGRAM_BOT_TOKEN="$2"
      shift 2
      ;;
    --telegram-owner-id)
      export TELEGRAM_OWNER_ID="$2"
      shift 2
      ;;
    --telegram-target-chat-id)
      export TELEGRAM_TARGET_CHAT_ID="$2"
      shift 2
      ;;
    --telegram-skip-setting-commands)
      export TELEGRAM_SKIP_SETTING_COMMANDS="$2"
      shift 2
      ;;
    --telegram-skip-startup-message)
      export TELEGRAM_SKIP_STARTUP_MESSAGE="$2"
      shift 2
      ;;
    --telegram-silent-confirmation)
      export TELEGRAM_SILENT_CONFIRMATION="$2"
      shift 2
      ;;
    --telegram-confirmation-type)
      export TELEGRAM_CONFIRMATION_TYPE="$2"
      shift 2
      ;;
    --whatsapp-session-name)
      export WHATSAPP_SESSION_NAME="$2"
      shift 2
      ;;
    --time-zone)
      export TIME_ZONE="$2"
      shift 2
      ;;
    --time-format)
      export TIME_FORMAT="$2"
      shift 2
      ;;
    --debug)
      export DEBUG_MODE="$2"
      shift 2
      ;;
    --config-path)
      export CONFIG_PATH="$2"
      shift 2
      ;;
    --version)
      export WATGBRIDGE_VERSION="$2"
      shift 2
      ;;
    --build)
      BUILD_FLAG="--build"
      shift
      ;;
    --help|-h)
      usage
      ;;
    *)
      echo "Unknown option: $1" >&2
      echo "Run with --help for usage." >&2
      exit 1
      ;;
  esac
done

export MODE="${MODE:-single}"

if [[ "$MODE" == "multi" ]]; then
  if [[ -z "${TELEGRAM_MAIN_BOT_TOKEN:-}" ]]; then
    echo "Error: multi mode requires TELEGRAM_MAIN_BOT_TOKEN or --telegram-main-bot-token." >&2
    exit 1
  fi
  export TELEGRAM_BOT_TOKEN="${TELEGRAM_BOT_TOKEN:-}"
  export TELEGRAM_OWNER_ID="${TELEGRAM_OWNER_ID:-0}"
  export TELEGRAM_TARGET_CHAT_ID="${TELEGRAM_TARGET_CHAT_ID:-0}"
else
  if [[ -z "${TELEGRAM_BOT_TOKEN:-}" || -z "${TELEGRAM_OWNER_ID:-}" || -z "${TELEGRAM_TARGET_CHAT_ID:-}" ]]; then
    echo "Error: single mode requires --telegram-bot-token, --telegram-owner-id and --telegram-target-chat-id." >&2
    exit 1
  fi
fi

if [[ -z "$WATGBRIDGE_VERSION" ]]; then
  export WATGBRIDGE_VERSION
  WATGBRIDGE_VERSION=$(git describe --tags --always 2>/dev/null || git rev-parse --short HEAD 2>/dev/null || echo "unknown")
fi

export WHATSAPP_SKIP_STATUS="${WHATSAPP_SKIP_STATUS:-true}"
export TIME_ZONE="${TIME_ZONE:-UTC}"
export TIME_FORMAT="${TIME_FORMAT:-02 Jan, 2006 - Mon @ 15:04}"
export DEBUG_MODE="${DEBUG_MODE:-false}"
export TELEGRAM_SKIP_SETTING_COMMANDS="${TELEGRAM_SKIP_SETTING_COMMANDS:-false}"
export TELEGRAM_SKIP_STARTUP_MESSAGE="${TELEGRAM_SKIP_STARTUP_MESSAGE:-false}"
export TELEGRAM_SILENT_CONFIRMATION="${TELEGRAM_SILENT_CONFIRMATION:-true}"
export WHATSAPP_SESSION_NAME="${WHATSAPP_SESSION_NAME:-watgbridge}"
export TELEGRAM_MAIN_BOT_TOKEN="${TELEGRAM_MAIN_BOT_TOKEN:-}"

TEMPLATE="./docker/config.yaml.tpl"
OUT="./config.yaml"

if ! command -v envsubst >/dev/null 2>&1; then
  echo "Error: envsubst not found. Install gettext (e.g. brew install gettext)." >&2
  exit 1
fi

if [[ ! -f "$TEMPLATE" ]]; then
  echo "Error: template $TEMPLATE not found." >&2
  exit 1
fi

echo "Generating $OUT from $TEMPLATE (mode=$MODE) ..."
envsubst < "$TEMPLATE" > "$OUT"
echo "Wrote $OUT"

exec docker compose up -d $BUILD_FLAG
