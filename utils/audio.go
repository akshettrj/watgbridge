package utils

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path"
	"strconv"
	"time"

	"watgbridge/state"

	"go.uber.org/zap"
)

// ConvertAudioToWhatsAppFormat converts Telegram audio to a WhatsApp-compatible format.
// It uses OGG Opus 16kHz, which provides good compression and full WhatsApp support.
func ConvertAudioToWhatsAppFormat(audioData []byte, updateId int64) ([]byte, error) {
	logger := state.State.Logger
	defer logger.Sync()

	var (
		currTime   = strconv.FormatInt(updateId, 10)
		currPath   = path.Join("downloads", currTime+"_audio")
		inputPath  = path.Join(currPath, "input.audio")
		outputPath = path.Join(currPath, "output.ogg")
	)

	// Create temporary directory
	if err := os.MkdirAll(currPath, os.ModePerm); err != nil {
		logger.Error("failed to create directory for audio conversion",
			zap.String("path", currPath),
			zap.Error(err),
		)
		return nil, err
	}
	defer os.RemoveAll(currPath)

	// Write input file
	if err := os.WriteFile(inputPath, audioData, os.ModePerm); err != nil {
		logger.Error("failed to write input audio file",
			zap.String("path", inputPath),
			zap.Error(err),
		)
		return nil, err
	}

	logger.Debug("starting audio conversion to WhatsApp format",
		zap.Int64("updateId", updateId),
		zap.String("inputPath", inputPath),
		zap.String("outputPath", outputPath),
	)

	// Run ffmpeg to convert to OGG Opus
	// -i: input file
	// -c:a libopus: audio codec (Opus)
	// -ar 16000: 16kHz sample rate (WhatsApp standard)
	// -b:a 32k: 32k bitrate (good quality/size balance)
	// -vbr on: enable Variable Bit Rate for better compression
	cmd := exec.Command(state.State.Config.FfmpegExecutable,
		"-i", inputPath,
		"-c:a", "libopus",
		"-ar", "16000",
		"-b:a", "32k",
		"-vbr", "on",
		outputPath,
	)

	// Capture stderr for logs
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		logger.Error("failed to execute ffmpeg for audio conversion",
			zap.String("inputPath", inputPath),
			zap.String("outputPath", outputPath),
			zap.String("stderr", stderr.String()),
			zap.Error(err),
		)
		return nil, fmt.Errorf("failed to convert audio with ffmpeg: %w", err)
	}

	// Read output file
	outputData, err := os.ReadFile(outputPath)
	if err != nil {
		logger.Error("failed to read converted audio file",
			zap.String("path", outputPath),
			zap.Error(err),
		)
		return nil, err
	}

	logger.Debug("audio conversion completed successfully",
		zap.Int64("updateId", updateId),
		zap.Int("inputSize", len(audioData)),
		zap.Int("outputSize", len(outputData)),
	)

	return outputData, nil
}

// GetAudioDuration extracts duration from converted audio.
// This helper can be used if duration needs to be refreshed after conversion.
func GetAudioDuration(audioData []byte, updateId int64) (uint32, error) {
	logger := state.State.Logger
	defer logger.Sync()

	var (
		currTime  = strconv.FormatInt(time.Now().UnixNano(), 10)
		currPath  = path.Join("downloads", currTime+"_duration")
		inputPath = path.Join(currPath, "input.audio")
	)

	// Create temporary directory
	if err := os.MkdirAll(currPath, os.ModePerm); err != nil {
		return 0, err
	}
	defer os.RemoveAll(currPath)

	// Write input file
	if err := os.WriteFile(inputPath, audioData, os.ModePerm); err != nil {
		return 0, err
	}

	// Use ffprobe to extract duration (if available)
	cmd := exec.Command("ffprobe",
		"-v", "error",
		"-show_entries", "format=duration",
		"-of", "default=noprint_wrappers=1:nokey=1:noprint_sections=1",
		inputPath,
	)

	var stdout bytes.Buffer
	cmd.Stdout = &stdout

	if err := cmd.Run(); err != nil {
		logger.Debug("ffprobe not available, returning 0 duration",
			zap.Error(err),
		)
		return 0, nil
	}

	// Parse duration
	var duration float64
	_, err := fmt.Sscanf(stdout.String(), "%f", &duration)
	if err != nil {
		logger.Debug("failed to parse duration from ffprobe",
			zap.Error(err),
		)
		return 0, nil
	}

	return uint32(duration), nil
}
