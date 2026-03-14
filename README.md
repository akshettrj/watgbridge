# WhatsApp-Telegram-Bridge

Despite the name, its not exactly a "bridge". It forwards messages from WhatsApp to Telegram and you can reply to them
from Telegram.

<a href="https://t.me/PropheCProjects">
  <img src="https://img.shields.io/badge/Updates_Channel-2CA5E0?style=for-the-badge&logo=telegram&logoColor=white"></img>
</a>&nbsp; &nbsp;
<a href="https://t.me/WaTgBridge">
  <img src="https://img.shields.io/badge/Discussion_Group-2CA5E0?style=for-the-badge&logo=telegram&logoColor=white"></img>
</a>&nbsp; &nbsp;
<a href="https://youtu.be/xc75XLoTmA4">
  <img src="https://img.shields.io/badge/YouTube-FF0000?style=for-the-badge&logo=youtube&logoColor=white"</img>
</a>

# DISCLAIMER !!!

This project is in no way affiliated with WhatsApp or Telegram. Using this can also lead to your account getting banned by WhatsApp so use at your own risk.

## Sample Screenshots

<p align="center">
  <img src="./assets/telegram_side_sample.png" width="350" alt="Telegram Side">
  <img src="./assets/whatsapp_side_sample.jpg" width="350" alt="WhatsApp Side">
</p>

## Features and Design Choices

- All messages from various chats (on WhatsApp) are sent to different topics/threads within the same target group (on Telegram)
- Configuration options available to disable different types of updates from WhatsApp
- Can reply and send new messages from Telegram
- Can tag all people using @all or @everyone. Others can also use this in group chats which you specify in configuration file
- Can react to messages by replying with single instance of the desired emoji
- Supports static stickers from both ends
- Can send Animated (TGS) stickers from Telegram
- Video stickers from Telegram side are supported
- Video stickers from WhatsApp side are currently forwarded as GIFs to Telegram

## Bugs and TODO

- Document naming is messed up and not consistent on Telegram, have to find a way to always send same names

PRs are welcome :)


## Installation

- Make a supergroup (enable message history for new members) with topics enabled
- Add your bot in the group, make it an admin with permissions to `Manage topics`
- Install `git`, `gcc` and `golang`, `ffmpeg` , `imagemagick` (optional), on your system
- Clone this repository anywhere and navigate to the cloned directory
- Run `go build`
- Copy `sample_config.yaml` to `config.yaml` and fill the values, there are comments to help you.
- Execute the binary by running `./watgbridge`
- On first run, it will show QR code for logging into WhatsApp that can by scanned by the WhatsApp app in `Linked devices`
- It is recommended to restart the bot after every few hours becuase WhatsApp likes to disconnect a lot. So a sample Systemd service file has been provided (`watgbridge.service.sample`). Edit the `User` and `ExecStart` according to your setup:
    - If you do not have local bot API server, remove `tgbotapi.service` from the `After` key in `Unit` section.
    - This service file will restart the bot every 24 hours

## Docker Compose

Run with Docker Compose; credentials are passed via environment variables (file or CLI).

1. Copy env example and set required vars:
   ```bash
   cp .env.example .env
   # Edit .env: TELEGRAM_BOT_TOKEN, TELEGRAM_OWNER_ID, TELEGRAM_TARGET_CHAT_ID
   ```

2. Launch:
   ```bash
   docker compose up -d
   ```

3. Pass creds via CLI (overrides `.env`):
   ```bash
   TELEGRAM_BOT_TOKEN=xxx TELEGRAM_OWNER_ID=123 TELEGRAM_TARGET_CHAT_ID=-1001234567890 docker compose up -d
   ```
   Or with a custom env file:
   ```bash
   docker compose --env-file .env.production up -d
   ```

Config is generated at runtime from env; session and config are stored in the `sqlite_data` volume.

## Code structure

```
watgbridge/
├── main.go                 # Entry: flags, viper config, logger, DB, Telegram, WhatsApp, cron, Idle()
├── state/                  # Global state + config
├── database/               # GORM + chat/msg/contact persistence
├── telegram/               # Telegram bot client + commands + startup check
├── whatsapp/               # WhatsApp (whatsmeow) client + event handling
├── utils/                  # Shared helpers (TG, WA, stickers, net)
├── modules/                # Pluggable handlers (Telegram + WA)
├── docker/                 # Docker entrypoint + config template
├── docker-compose.yml
├── run.sh
├── sample_config.yaml
└── .github/workflows/       # CI (build, release, nix cache)
```

| Package | Role |
|--------|------|
| **main** | Parses flags (pflag), loads config via **state.InitConfig** (viper: file → env → flags), builds logger, connects DB, starts **telegram.NewTelegramClient()**, runs **telegram.CheckTargetGroupPermissions()**, starts **whatsapp.NewWhatsAppClient()**, registers handlers, optional startup message, then **updater.Idle()**. |
| **state** | Single **State** (config, logger, DB, Telegram bot/dispatcher/updater, WhatsApp client, modules list, start time, location). **config.go**: Config struct, Load/Save YAML, SetDefaults. **viper.go**: InitConfig (defaults → file → env → flag bindings), unmarshal into State.Config. **state.go**: State struct + init. |
| **database** | **connect.go**: Connect() by type (postgres/sqlite/mysql). **types.go**: GORM models + AutoMigrate. **helpers.go**: MsgId pairs (WA↔TG), ChatThread pairs (WA chat ↔ TG topic), contacts, ephemeral settings. |
| **telegram** | **client.go**: NewTelegramClient() — create bot, middlewares, dispatcher, updater, StartPolling. **handlers.go**: All bot commands (start, getwagroups, findcontact, settargetgroupchat, settargetprivatechat, unlinkthread, send, help, updateandrestart, etc.). **check_target.go**: CheckTargetGroupPermissions() — GetChat (forum?), GetChatMember (admin + Manage topics), log + send to target group and owner on failure. **middlewares/**: rate limit, parse HTML, disable preview, send without reply. **constants.go**: handler group IDs. |
| **whatsapp** | **client.go**: NewWhatsAppClient() (whatsmeow, sqlstore, QR if needed), AddEventHandler(WhatsAppEventHandler). **handlers.go**: One big **WhatsAppEventHandler** that switches on event type (messages, status, reactions, presence, etc.), resolves WA chat → TG topic via DB, calls **utils.TgSendToWhatsApp** for replies and **utils** for sending to WA. |
| **utils** | **telegram.go**: TgGetOrMakeThreadFromWa, TgReplyTextByContext, TgSendErrorById, **TgSendToWhatsApp** (main “reply from TG to WA” flow), revoke keyboard, etc. **whatsapp.go**: WaParseJID, WaFuzzyFindContacts, WaGetContactName, WaSendText, WaTagAll. **stickers.go**: TGS→WebP, WebM→WebP, animated WebP→GIF. **net.go**: Download by URL. **go.go**: SubString. |
| **modules** | **load.go**: Registers optional Telegram and WhatsApp handlers from **TelegramHandlers** map and **WhatsAppHandlers** slice; called after telegram/whatsapp handlers so modules can extend both sides. Default build has no modules (empty maps). |

**Data flow**

- **Config**: Viper (defaults → YAML → env → flags) → **state.State.Config**.
- **WA → TG**: WhatsApp event in **whatsapp/handlers.go** → DB (ChatThreadGetTgFromWa, MsgIdAddNewPair, etc.) → **utils** (TgSend*, CreateForumTopic) → send to **State.Config.Telegram.TargetChatID** in the right topic.
- **TG → WA**: User reply in Telegram → **telegram/handlers** (SendToWhatsAppHandler) → DB (MsgIdGetWaFromTg, ChatThreadGetWaFromTg) → **utils.TgSendToWhatsApp** → whatsmeow send.

**Docker / deploy**

- **docker/entrypoint.sh**: Envsubst from **config.yaml.tpl** → `/data/config.yaml`, then `./watgbridge /data/config.yaml`.
- **docker-compose.yml**: `db` (Alpine, volume) + `watgbridge` (build ., env vars, shared volume `sqlite_data` at `/data`).
- **run.sh**: Parses named args, exports env, runs `docker compose up -d` (optionally `--build`).
