# wa-tg-bridge

A bridge between Telegram and WhatsApp but in Go

- [X] Can bridge messages from WhatsApp to Telegram
- [X] Only one target chat (in Telegram) for all chats (in WhatsApp)
- [X] Can specify which chats to NOT bridge
- [X] Ignores stories/statuses
- [X] Can reply to forwarded messages in Telegram to send messages in WhatsApp
- [X] Ability to tag all the people in chat using @all or @everyone
- [X] Others can also use this tag feature in only those chats which you allow (see sample config)

## Bugs ?
- Replying with Audio/Voice Notes doesn't send them on WhatsApp and shows no errors
- Doesn't "reply" to the original message, sends a new independent message
