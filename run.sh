#!/usr/bin/env bash
set -e

# Run from repo root on the server: ./run.sh --telegram-bot-token xxx --telegram-owner-id 123 --telegram-target-chat-id -100…
# Launches the compose stack with named args passed as env vars to the app.

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

usage() {
  cat <<EOF
Usage: $0 [OPTIONS] [--build]

Required (app will fail without them):
  --telegram-bot-token TOKEN
  --telegram-owner-id ID
  --telegram-target-chat-id CHAT_ID

Optional:
  --telegram-skip-setting-commands true|false
  --telegram-skip-startup-message true|false
  --telegram-silent-confirmation true|false
  --telegram-confirmation-type TYPE
  --whatsapp-session-name NAME
  --time-zone ZONE
  --time-format FORMAT
  --debug true|false
  --config-path PATH

  --build    Run 'docker compose up -d --build' (rebuild image)
  --help     Show this help
EOF
  exit 0
}

BUILD_FLAG=""
while [[ $# -gt 0 ]]; do
  case "$1" in
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

if [[ -z "$TELEGRAM_BOT_TOKEN" || -z "$TELEGRAM_OWNER_ID" || -z "$TELEGRAM_TARGET_CHAT_ID" ]]; then
  echo "Error: --telegram-bot-token, --telegram-owner-id and --telegram-target-chat-id are required." >&2
  exit 1
fi

exec docker compose up -d $BUILD_FLAG
