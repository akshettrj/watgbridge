package utils

import (
	"archive/zip"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"watgbridge/state"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/robfig/cron/v3"
	"go.uber.org/zap"
)

type backupFile struct {
	DisplayName string
	Path        string
}

func normalizeBackupMode(mode string) string {
	normalized := strings.ToLower(strings.TrimSpace(mode))
	if normalized == "tread" {
		return "thread"
	}
	return normalized
}

func extractSQLitePath(sqliteURL string) (string, error) {
	parsedURL, err := url.Parse(sqliteURL)
	if err != nil {
		return "", err
	}

	if parsedURL.Scheme != "file" {
		return "", fmt.Errorf("unsupported sqlite url scheme: %s", parsedURL.Scheme)
	}

	if parsedURL.Opaque != "" {
		if parsedURL.Opaque == ":memory:" {
			return "", fmt.Errorf("in-memory sqlite database cannot be backed up")
		}
		return parsedURL.Opaque, nil
	}

	if parsedURL.Path == "" || parsedURL.Path == "/" {
		return "", fmt.Errorf("sqlite path is empty")
	}

	if parsedURL.Path == "/:memory:" {
		return "", fmt.Errorf("in-memory sqlite database cannot be backed up")
	}

	return parsedURL.Path, nil
}

func appendBackupArtifacts(filesMap map[string]backupFile, displayName, dbPath string) {
	if dbPath == "" {
		return
	}

	allCandidates := []struct {
		display string
		path    string
	}{
		{display: displayName, path: dbPath},
		{display: displayName + "-wal", path: dbPath + "-wal"},
		{display: displayName + "-shm", path: dbPath + "-shm"},
	}

	for _, candidate := range allCandidates {
		if _, err := os.Stat(candidate.path); err != nil {
			continue
		}
		absolutePath, err := filepath.Abs(candidate.path)
		if err != nil {
			continue
		}
		filesMap[absolutePath] = backupFile{DisplayName: candidate.display, Path: absolutePath}
	}
}

func collectDatabaseFiles() []backupFile {
	cfg := state.State.Config
	filesMap := map[string]backupFile{}

	dbType := strings.ToLower(strings.TrimSpace(cfg.Database["type"]))
	if dbType == "sqlite" {
		appendBackupArtifacts(filesMap, "main-database.db", strings.TrimSpace(cfg.Database["path"]))
	}

	if strings.EqualFold(strings.TrimSpace(cfg.WhatsApp.LoginDatabase.Type), "sqlite3") {
		if waDBPath, err := extractSQLitePath(strings.TrimSpace(cfg.WhatsApp.LoginDatabase.URL)); err == nil {
			appendBackupArtifacts(filesMap, "whatsapp-login.db", waDBPath)
		}
	}

	files := make([]backupFile, 0, len(filesMap))
	for _, backupItem := range filesMap {
		files = append(files, backupItem)
	}

	return files
}

func makeBackupZip(files []backupFile, now time.Time) (*os.File, string, error) {
	temporaryFile, err := os.CreateTemp("", "watgbridge-backup-*.zip")
	if err != nil {
		return nil, "", err
	}

	zipWriter := zip.NewWriter(temporaryFile)

	for _, backupItem := range files {
		archiveEntry, err := zipWriter.Create(backupItem.DisplayName)
		if err != nil {
			_ = zipWriter.Close()
			_ = temporaryFile.Close()
			_ = os.Remove(temporaryFile.Name())
			return nil, "", err
		}

		inputFile, err := os.Open(backupItem.Path)
		if err != nil {
			_ = zipWriter.Close()
			_ = temporaryFile.Close()
			_ = os.Remove(temporaryFile.Name())
			return nil, "", err
		}

		_, err = io.Copy(archiveEntry, inputFile)
		_ = inputFile.Close()
		if err != nil {
			_ = zipWriter.Close()
			_ = temporaryFile.Close()
			_ = os.Remove(temporaryFile.Name())
			return nil, "", err
		}
	}

	if err := zipWriter.Close(); err != nil {
		_ = temporaryFile.Close()
		_ = os.Remove(temporaryFile.Name())
		return nil, "", err
	}

	if _, err := temporaryFile.Seek(0, 0); err != nil {
		_ = temporaryFile.Close()
		_ = os.Remove(temporaryFile.Name())
		return nil, "", err
	}

	backupName := fmt.Sprintf("watgbridge-backup-%s.zip", now.Format("02-01-2006-150405"))
	return temporaryFile, backupName, nil
}

func sendBackupArchive(mode string) error {
	cfg := state.State.Config
	tgBot := state.State.TelegramBot

	files := collectDatabaseFiles()
	if len(files) == 0 {
		return fmt.Errorf("no sqlite database files were found to back up")
	}

	now := time.Now().UTC()
	backupZip, backupName, err := makeBackupZip(files, now)
	if err != nil {
		return err
	}
	defer func() {
		_ = backupZip.Close()
		_ = os.Remove(backupZip.Name())
	}()

	fileToSend := gotgbot.FileReader{
		Name: backupName,
		Data: backupZip,
	}

	sendOpts := &gotgbot.SendDocumentOpts{
		Caption: fmt.Sprintf("Database backup (%s UTC)", now.Format("02-01-2006 15:04:05")),
	}

	targetChatId := cfg.Telegram.OwnerID

	if mode == "thread" {
		targetChatId = cfg.Telegram.TargetChatID
		threadName := strings.TrimSpace(cfg.Backup.ThreadName)
		if threadName == "" {
			threadName = "Database Backups"
		}

		threadId, err := TgGetOrMakeThreadFromWa_String("database_backups", targetChatId, threadName)
		if err != nil {
			return err
		}

		_, _ = tgBot.ReopenForumTopic(targetChatId, threadId, &gotgbot.ReopenForumTopicOpts{})
		sendOpts.MessageThreadId = threadId
	}

	_, err = tgBot.SendDocument(targetChatId, &fileToSend, sendOpts)
	if err != nil {
		return err
	}

	if mode == "thread" {
		threadId := sendOpts.MessageThreadId
		_, closeErr := tgBot.CloseForumTopic(targetChatId, threadId, &gotgbot.CloseForumTopicOpts{})
		if closeErr != nil {
			state.State.Logger.Warn("failed to close backup thread", zap.Error(closeErr), zap.Int64("thread_id", threadId))
		}
	}

	return nil
}

func RunDatabaseBackupOnce() error {
	mode := normalizeBackupMode(state.State.Config.Backup.Mode)
	if mode == "" || mode == "none" {
		return nil
	}
	if mode != "private" && mode != "thread" {
		return fmt.Errorf("invalid backup mode '%s' (valid: none, private, thread)", state.State.Config.Backup.Mode)
	}
	return sendBackupArchive(mode)
}

func resolveBackupSchedule() string {
	cfg := state.State.Config

	cronSchedule := strings.TrimSpace(cfg.Backup.CronSchedule)
	if cronSchedule != "" {
		return cronSchedule
	}

	return "0 0 * * *"
}

func StartAutomaticDatabaseBackups() {
	cfg := state.State.Config
	logger := state.State.Logger

	mode := normalizeBackupMode(cfg.Backup.Mode)
	if mode == "" || mode == "none" {
		logger.Info("automatic database backup is disabled")
		return
	}

	if mode != "private" && mode != "thread" {
		logger.Error("invalid backup mode configured", zap.String("mode", cfg.Backup.Mode))
		return
	}

	schedule := resolveBackupSchedule()
	cronParser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)
	if _, err := cronParser.Parse(schedule); err != nil {
		logger.Error("invalid backup cron schedule",
			zap.String("cron_schedule", schedule),
			zap.Error(err),
		)
		return
	}

	logger.Info("automatic database backup enabled",
		zap.String("mode", mode),
		zap.String("cron_schedule", schedule),
	)

	go func() {
		cronScheduler := cron.New(cron.WithLocation(state.State.LocalLocation))

		_, err := cronScheduler.AddFunc(schedule, func() {
			if err := RunDatabaseBackupOnce(); err != nil {
				logger.Error("failed to run automatic database backup", zap.Error(err))
			} else {
				logger.Info("automatic database backup sent successfully")
			}
		})
		if err != nil {
			logger.Error("failed to register backup cron schedule",
				zap.String("cron_schedule", schedule),
				zap.Error(err),
			)
			return
		}

		cronScheduler.Start()
	}()
}
