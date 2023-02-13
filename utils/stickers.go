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

	"github.com/Benau/tgsconverter/libtgsconverter"
)

func TGSConvertToWebp(tgsStickerData []byte) ([]byte, error) {
	opt := libtgsconverter.NewConverterOptions()
	opt.SetExtension("webp")
	var (
		quality float32 = 100
		fps     uint    = 30
	)
	for quality > 2 && fps > 5 {
		opt.SetFPS(fps)
		opt.SetWebpQuality(quality)
		webpStickerData, err := libtgsconverter.ImportFromData(tgsStickerData, opt)
		if err != nil {
			return nil, err
		} else if len(webpStickerData) < 1024*1024 {
			return webpStickerData, nil
		}
		quality /= 2
		fps = uint(float32(fps) / 1.5)
	}
	return nil, fmt.Errorf("sticker has a lot of data which cannot be handled by WhatsApp")
}

func WebmConvertToGif(webmStickerData []byte) ([]byte, error) {

	var (
		currTime   = strconv.FormatInt(time.Now().Unix(), 10)
		currPath   = path.Join("downloads", currTime)
		inputPath  = path.Join(currPath, "input.webm")
		outputPath = path.Join(currPath, "output.gif")
	)

	os.MkdirAll(currPath, os.ModePerm)
	defer os.RemoveAll(currPath)

	os.WriteFile(inputPath, bytes.Clone(webmStickerData), os.ModePerm)

	cmd := exec.Command(state.State.Config.FfmpegExecutable,
		"-i", inputPath,
		"-fs", "512000",
		"-vf", "scale=w=480:-1:force_original_aspect_ratio=increase",
		"-q:v", "55",
		"-pix_fmt", "rgb24",
		"-r", "30",
		outputPath,
	)

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("failed to execute ffmpeg command: %s", err)
	}

	return os.ReadFile(outputPath)
}
