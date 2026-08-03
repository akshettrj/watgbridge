# Project TODO & Issues Tracker

This list tracks the current bugs, pending improvements, and feature tasks for the WhatsApp-Telegram bridge.

## Active Bugs & Issues

- **Document Naming Inconsistency:** Document names are not always consistent when sent to Telegram; need to implement a uniform naming convention.
- **Live Location Updates:** Live locations from Telegram are currently forwarded as static messages. Real-time updates need to be implemented.

## Feature Enhancements

- **Status / Stories Handling:** Improve the sync and rendering of status updates from WhatsApp to Telegram.
- **Inline Commands & Keyboards:** Implement Telegram inline keyboards to facilitate quick actions (e.g., block user, mute chat, get profile picture) directly from the bridged thread.
- **Automatic DB Maintenance:** Add automated commands or schedules to clean up old message pairs history to keep database sizes optimal.

## Code Quality & Refactoring

- **Database Helper Unit Tests:** Expand unit test coverage for package database helpers to ensure stability across database upgrades.
- **Utility Decoupling:** Refactor helper functions in `utils/` to decouple Telegram and WhatsApp business logic, making unit testing simpler.
