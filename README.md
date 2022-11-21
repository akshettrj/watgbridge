# wa-tg-bridge

Despite the name, its not exactly a "bridge". It forwards messages from WhatsApp to Telegram and you can reply to them
from Telegram.

## Features and Design Choices

- Can bridge messages from WhatsApp to Telegram
- All messages from various chats (WhatsApp) are sent to the same target chat (Telegram)
- By default all chats are bridged. You can specify which chats NOT to bridge in configuration file
- Only those contacts' statuses are bridged which are listed in configuration file
- Can reply to forwarded messages from Telegram
- Can tag all people using @all or @everyone. Others can also use this in group chats which you specify in configuration file
- Can react to messages by replying with desired emoji

## Bugs and TODO

- Animated stickers are not supported both to and from WhatsApp

PRs are welcome :)


## Installation

- Clone this repository in `$GOPATH/src`
- Navigate into the cloned directory
- Run `go build`
- Copy `sample_config.yaml` to `config.yaml` and fill the values:
    - You can leave `api_url` in `telegram` section empty if you don't have any local Bot API server (recommended to have one for large files)
    - Uncomment one of the `database` sections according to your preferences
    - In the `whatsapp` section:
        - `tag_all_allowed_groups`: these are the groups in which others can use @all/@everyone to tag everyone. The values that you need to fill in this section can be found by sending `/getwagroups` to the bot after running it once. You have to fill in the value before @ character in the JID.
        - `ignore_chats`: these are the groups which will not be bridged to telegram. The values have to be filled in the same way as `tag_all_allowed_groups`.
        - `status_allowed_chats`: these are the contacts (along with the country code) whose statuses will be bridged to Telegram. DO NOT TRY TO REPLY TO THESE BRIDGED STATUSES.
- It is recommended to restart the bot after every few hours becuase WhatsApp likes to disconnect a lot. So a Systemd service file has been provided. Edit the `User` and `ExecStart` according to your setup:
    - If you do not have local bot API server, remove `tgbotapi.service` from the `After` key in `Unit` section.
    - This service file will restart the bot every 24 hours
