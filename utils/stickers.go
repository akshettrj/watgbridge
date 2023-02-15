package utils

import (
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

func WebmConvertToWebp(webmStickerData []byte, scale, pad string) ([]byte, error) {

	var (
		currTime   = strconv.FormatInt(time.Now().Unix(), 10)
		currPath   = path.Join("downloads", currTime)
		inputPath  = path.Join(currPath, "input.webm")
		outputPath = path.Join(currPath, "output.webp")
	)

	os.MkdirAll(currPath, os.ModePerm)
	defer os.RemoveAll(currPath)

	os.WriteFile(inputPath, webmStickerData, os.ModePerm)

	cmd := exec.Command(state.State.Config.FfmpegExecutable,
		"-i", inputPath,
		"-fs", "800000",
		"-vf", fmt.Sprintf("fps=15,scale=%s,pad=%s", scale, pad),
		outputPath,
	)

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("failed to execute ffmpeg command: %s", err)
	}

	return os.ReadFile(outputPath)
}
