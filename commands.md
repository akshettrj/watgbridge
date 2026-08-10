# Bot Commands Reference

This document lists all the Telegram commands available in the WhatsApp-Telegram-Bridge and explains their usage and function.

## Core Commands

### `/help`
- **Description:** Lists all available commands and their basic descriptions.
- **Usage:** `/help`

### `/start`
- **Description:** Verifies that the bot is running and checks its active status.
- **Usage:** `/start`

---

## Chat & Contact Discovery

### `/getwagroups`
- **Description:** Fetches all WhatsApp groups that the connected account is part of, returning their names and JIDs (Group IDs).
- **Usage:** `/getwagroups`

### `/findcontact <query>`
- **Description:** Performs a fuzzy find on your WhatsApp contacts by name to retrieve their JIDs.
- **Usage:** `/findcontact John Doe`

### `/findgroupmembers <group_jid>`
- **Description:** Lists all members in a WhatsApp group along with their phone numbers and JIDs.
- **Usage:** `/findgroupmembers 12036302839485@g.us`

### `/getprofilepicture <jid>`
- **Description:** Retrieves and sends the profile picture of a WhatsApp user or group using their JID.
- **Usage:** `/getprofilepicture 5511999999999@s.whatsapp.net`

---

## Thread Management & Mapping

### `/settargetgroupchat <group_jid>`
- **Description:** Links the current Telegram topic/thread to a specific WhatsApp group JID. Future messages in this topic will be forwarded to that group.
- **Usage:** `/settargetgroupchat 12036302839485@g.us`

### `/settargetprivatechat <contact_jid>`
- **Description:** Links the current Telegram topic/thread to a private WhatsApp contact JID.
- **Usage:** `/settargetprivatechat 5511999999999@s.whatsapp.net`

### `/unlinkthread`
- **Description:** Unlinks the current Telegram topic/thread from its mapped WhatsApp chat, stopping forwarding.
- **Usage:** `/unlinkthread`

### `/synctopicnames`
- **Description:** Automatically updates and synchronizes the names of all Telegram topics to match the current names of their corresponding WhatsApp chats.
- **Usage:** `/synctopicnames`

---

## Message Control

### `/send <jid> <message>`
- **Description:** Sends a direct text message to a specific WhatsApp JID.
- **Usage:** `/send 5511999999999@s.whatsapp.net Hello!`

### `/revoke`
- **Description:** Revokes (deletes) a message on WhatsApp. Must be sent as a reply to the bridged message you wish to delete.
- **Usage:** Reply to a message with `/revoke`

### `/info`
- **Description:** Displays detailed delivery and read receipts for a WhatsApp message. Must be sent as a reply to a bridged message.
- **Usage:** Reply to a message with `/info`

---

## Client & System Administration

### `/restartwa`
- **Description:** Restarts the WhatsApp client connection. Useful if messages are stuck or if the client disconnected.
- **Usage:** `/restartwa`

### `/synccontacts`
- **Description:** Forces a manual sync of the WhatsApp contacts list with the local database.
- **Usage:** `/synccontacts`

### `/clearpairhistory`
- **Description:** Purges the history of mapped message ID pairs from the database to save space.
- **Usage:** `/clearpairhistory`

### `/backup`
- **Description:** Generates a database backup immediately and sends it to the owner.
- **Usage:** `/backup`

### `/joininvitelink <url>`
- **Description:** Instructs the WhatsApp client to join a group using a standard WhatsApp invite link.
- **Usage:** `/joininvitelink https://chat.whatsapp.com/AbCdEfGhIjKlMn`

### `/block <jid>`
- **Description:** Blocks a contact JID in WhatsApp.
- **Usage:** `/block 5511999999999@s.whatsapp.net`

### `/unblock <jid>`
- **Description:** Unblocks a contact JID in WhatsApp.
- **Usage:** `/unblock 5511999999999@s.whatsapp.net`

### `/updateandrestart`
- **Description:** Fetches the latest updates from the Git repository, rebuilds the project, and restarts the bot.
- **Usage:** `/updateandrestart`

### `/setstatusmessage <text>` (ou `/setstatus <text>`)
- **Description:** Sets/updates your WhatsApp profile status message.
- **Usage:** `/setstatusmessage Available for calls only`

