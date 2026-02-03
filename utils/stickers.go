package utils

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"image"
	// "image/color"
	"image/draw"
	"os"
	"os/exec"

	"watgbridge/state"

	"github.com/watgbridge/tgsconverter/libtgsconverter"
	"github.com/watgbridge/webp"
	"go.uber.org/zap"
)

func TGSConvertToWebp(tgsStickerData []byte, updateId int64) ([]byte, error) {
	logger := state.State.Logger
	defer logger.Sync()
	opt := libtgsconverter.NewConverterOptions()
	opt.SetExtension("webp")
	var (
		quality float32 = 100
		fps     uint    = 30
	)
	for quality > 2 && fps > 5 {
		logger.Debug("trying to convert tgs to webp",
			zap.Int64("updateId", updateId),
			zap.Float32("quality", quality),
			zap.Uint("fps", fps),
		)
		opt.SetFPS(fps)
		opt.SetWebpQuality(quality)
		webpStickerData, err := libtgsconverter.ImportFromData(tgsStickerData, opt)
		if err != nil {
			return nil, err
		} else if len(webpStickerData) < 1024*1024 {
			if outputDataWithExif, err := WebpWriteExifData(webpStickerData); err == nil {
				return outputDataWithExif, nil
			}
			return webpStickerData, nil
		}
		quality /= 2
		fps = uint(float32(fps) / 1.5)
	}
	return nil, fmt.Errorf("sticker has a lot of data which cannot be handled by WhatsApp")
}

func WebmConvertToWebp(webmStickerData []byte, scale, pad string, updateId int64) ([]byte, error) {
	logger := state.State.Logger
	defer logger.Sync()

	cmd := exec.Command(state.State.Config.FfmpegExecutable,
		"-i", "-",
		"-fs", "800000",
		"-compression_level", "6",
		"-vf", fmt.Sprintf("fps=15,format=rgba,scale=%s,pad=%s:color=#00000000", scale, pad),
		"-f", "webp",
		"-",
	)

	var outputBuf, stderr bytes.Buffer
	cmd.Stdin = bytes.NewReader(webmStickerData)
	cmd.Stdout = &outputBuf
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		logger.Debug("ffmpeg command failed",
			zap.Error(err),
			zap.String("stderr", stderr.String()),
		)
		return nil, err
	}

	if outputDataWithExif, err := WebpWriteExifData(outputBuf.Bytes()); err == nil {
		return outputDataWithExif, nil
	}

	return outputBuf.Bytes(), nil
}

func WebpImagePad(inputData []byte, wPad, hPad int, updateId int64) ([]byte, error) {
	inputImage, err := webp.DecodeRGBA(inputData)
	if err != nil {
		return nil, fmt.Errorf("failed to decode web image: %w", err)
	}

	var (
		wOffset = wPad / 2
		hOffset = hPad / 2
	)

	outputWidth := inputImage.Bounds().Dx() + wPad
	outputHeight := inputImage.Bounds().Dy() + hPad

	outputImage := image.NewRGBA(image.Rect(0, 0, outputWidth, outputHeight))
	draw.Draw(outputImage, image.Rect(wOffset, hOffset, outputWidth-wOffset, outputHeight-hOffset), inputImage, image.Point{}, draw.Src)

	outputBytes, err := webp.EncodeRGBA(outputImage, 100)
	if err != nil {
		return nil, fmt.Errorf("failed to encode padded data into Webp: %w", err)
	}

	if outputData, err := WebpWriteExifData(outputBytes); err == nil {
		return outputData, nil
	}

	return outputBytes, nil
}

func AnimatedWebpConvertToGif(inputData []byte, updateId string) ([]byte, error) {
	logger := state.State.Logger
	defer logger.Sync()

	cmd := exec.Command("convert",
		"webp:-",
		"-loop", "0",
		"-dispose", "previous",
		"gif:-",
	)

	var outputBuf, stderr bytes.Buffer
	cmd.Stdin = bytes.NewReader(inputData)
	cmd.Stdout = &outputBuf
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		logger.Debug("convert command failed",
			zap.Error(err),
			zap.String("stderr", stderr.String()),
		)
		return nil, err
	}

	return outputBuf.Bytes(), nil
}

func WebpWriteExifData(inputData []byte) ([]byte, error) {
	var (
		cfg           = state.State.Config
		logger        = state.State.Logger
		startingBytes = []byte{0x49, 0x49, 0x2A, 0x00, 0x08, 0x00, 0x00, 0x00, 0x01, 0x00, 0x41, 0x57, 0x07, 0x00}
		endingBytes   = []byte{0x16, 0x00, 0x00, 0x00}
		b             bytes.Buffer
	)
	defer logger.Sync()

	exifFile, err := os.CreateTemp("", "raw*.exif")
	if err != nil {
		return nil, fmt.Errorf("failed to create exif file: %w", err)
	}
	defer os.Remove(exifFile.Name())

	inputFile, err := os.CreateTemp("", "input")
	if err != nil {
		return nil, fmt.Errorf("failed to create input data file: %w", err)
	}
	defer os.Remove(inputFile.Name())

	if _, err := b.Write(startingBytes); err != nil {
		return nil, err
	}

	jsonData := map[string]any{
		"sticker-pack-id":        "watgbridge.akshettrj.com.github.",
		"sticker-pack-name":      cfg.WhatsApp.StickerMetadata.PackName,
		"sticker-pack-publisher": cfg.WhatsApp.StickerMetadata.AuthorName,
		"emojis":                 []string{"😀"},
	}
	jsonBytes, err := json.Marshal(jsonData)
	if err != nil {
		return nil, err
	}

	jsonLength := (uint32)(len(jsonBytes))
	lenBuffer := make([]byte, 4)
	binary.LittleEndian.PutUint32(lenBuffer, jsonLength)

	if _, err := b.Write(lenBuffer); err != nil {
		return nil, err
	}
	if _, err := b.Write(endingBytes); err != nil {
		return nil, err
	}
	if _, err := b.Write(jsonBytes); err != nil {
		return nil, err
	}

	if _, err := exifFile.Write(b.Bytes()); err != nil {
		return nil, err
	}
	exifFile.Close()

	if _, err := inputFile.Write(inputData); err != nil {
		return nil, err
	}
	inputFile.Close()

	cmd := exec.Command("webpmux",
		"-set", "exif", exifFile.Name(),
		inputFile.Name(),
		"-o", "-",
	)

	var outputBuf, stderr bytes.Buffer
	cmd.Stdout = &outputBuf
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		logger.Debug("failed to run webpmux command",
			zap.Error(err),
			zap.String("stderr", stderr.String()),
		)
		return nil, err
	}

	return outputBuf.Bytes(), nil
}
