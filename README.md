# wa-tg-bridge

Despite the name, its not exactly a "bridge". It forwards messages from WhatsApp to Telegram and you can reply to them
from Telegram.


## Sample Screenshots

<p align="center">
  <img src="./assets/telegram_side_sample.png" width="350" alt="Telegram Side">
  <img src="./assets/whatsapp_side_sample.jpg" width="350" alt="WhatsApp Side">
</p>

## Features and Design Choices

- Can bridge messages from WhatsApp to Telegram
- All messages from various chats (WhatsApp) are sent to different topics withing the same target chat (Telegram)
- By default all the statuses are bridged, you can specify which contacts' statuses not to bridge
- Can reply to forwarded messages from Telegram
- Can tag all people using @all or @everyone. Others can also use this in group chats which you specify in configuration file
- Can react to messages by replying with desired emoji
- Supports static stickers from both ends
- Can send Animated (TGS) stickers from Telegram

## Bugs and TODO

- Animated stickers are not supported from WhatsApp
- Document naming is messed up and not consistent on Telegram, have to find a way to always send sane names

PRs are welcome :)


## Installation

- Make a supergroup with topics enabled
- Add your bot in the group, make it an admin with permissions to `Manage topics`
- Install `git`, `gcc` and `golang` on your system
- Clone this repository in `$GOPATH/src`
- Navigate into the cloned directory
- Run `go build`
- Copy `sample_config.yaml` to `config.yaml` and fill the values:
    - You can leave `api_url` in `telegram` section empty if you don't have any local Bot API server (recommended to have one for large files)
    - Uncomment one of the `database` sections according to your preferences
    - In the `whatsapp` section:
        - `tag_all_allowed_groups`: these are the groups in which others can use @all/@everyone to tag everyone. The values that you need to fill in this section can be found by sending `/getwagroups` to the bot after running it once. You have to fill in the value before @ character in the JID.
        - `status_ignored_chats`: these are the contacts (along with the country code) whose statuses will not be bridged to Telegram.
- Execute the binary by running `./watgbridge`
- On first run, it will show QR code for logging into WhatsApp that can by scanned by the WhatsApp app in `Linked devices`
- It is recommended to restart the bot after every few hours becuase WhatsApp likes to disconnect a lot. So a Systemd service file has been provided. Edit the `User` and `ExecStart` according to your setup:
    - If you do not have local bot API server, remove `tgbotapi.service` from the `After` key in `Unit` section.
    - This service file will restart the bot every 24 hours


- A small guide can also be found in <a href="https://youtu.be/xc75XLoTmA4">this YouTube video</a>
