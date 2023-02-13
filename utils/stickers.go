package utils

import (
	"bufio"
	"bytes"
	"fmt"
	"os/exec"

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

	cmd := exec.Command("ffmpeg",
		"-f", "webm",
		"-i", "-",
		"-vf", "scale=w=480:-1:force_original_aspect_ratio=increase",
		"-pix_fmt", "rgb24",
		"-r", "10",
		"-f", "gif",
		"-",
	)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to get ffmpeg's stdin pipe: %s", err)
	}

	var stdout bytes.Buffer
	cmd.Stdout = &stdout

	if err = cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start ffmpeg command: %s", err)
	}

	writer := bufio.NewWriter(stdin)
	_, err = writer.Write(webmStickerData)
	if err != nil {
		return nil, fmt.Errorf("failed to write to stdin: %s", err)
	}

	writer.Flush()
	stdin.Close()

	if err := cmd.Wait(); err != nil {
		return nil, fmt.Errorf("failed waiting for the command to finish: %s", err)
	}

	return stdout.Bytes(), nil
}
