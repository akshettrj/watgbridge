mode: ${MODE}
time_zone: ${TIME_ZONE}
time_format: ${TIME_FORMAT}
debug_mode: ${DEBUG_MODE}

telegram:
  main_bot_token: "${TELEGRAM_MAIN_BOT_TOKEN}"
  bot_token: "${TELEGRAM_BOT_TOKEN}"
  owner_id: ${TELEGRAM_OWNER_ID}
  target_chat_id: ${TELEGRAM_TARGET_CHAT_ID}
  sudo_users_id:
    - ${TELEGRAM_OWNER_ID}
  skip_setting_commands: ${TELEGRAM_SKIP_SETTING_COMMANDS}
  skip_startup_message: ${TELEGRAM_SKIP_STARTUP_MESSAGE}
  silent_confirmation: ${TELEGRAM_SILENT_CONFIRMATION}

whatsapp:
  session_name: ${WHATSAPP_SESSION_NAME}
  login_database:
    type: sqlite3
    url: file:/data/wawebstore.db?foreign_keys=on
  skip_documents: false
  skip_images: false
  skip_gifs: false
  skip_videos: false
  skip_voice_notes: false
  skip_audios: false
  skip_stickers: false
  skip_status: ${WHATSAPP_SKIP_STATUS}
  skip_contacts: false
  skip_locations: false
  skip_profile_picture_updates: false
  skip_group_settings_updates: false
  skip_chat_details: true
  sticker_metadata:
    pack_name: WaTgBridge
    author_name: WaTgBridge

database:
  type: sqlite
  path: /data/gobot.sqlite.db
