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
	"path"
	"strconv"

	"watgbridge/state"

	"github.com/Benau/tgsconverter/libtgsconverter"
	"github.com/kolesa-team/go-webp/decoder"
	"github.com/kolesa-team/go-webp/encoder"
	"github.com/kolesa-team/go-webp/webp"
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
			if outputDataWithExif, err := WebpWriteExifData(webpStickerData, updateId); err == nil {
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

	var (
		currTime   = strconv.FormatInt(updateId, 10)
		currPath   = path.Join("downloads", currTime)
		inputPath  = path.Join(currPath, "input.webm")
		outputPath = path.Join(currPath, "output.webp")
	)

	if err := os.MkdirAll(currPath, os.ModePerm); err != nil {
		return nil, err
	}
	defer os.RemoveAll(currPath)

	if err := os.WriteFile(inputPath, webmStickerData, os.ModePerm); err != nil {
		return nil, err
	}

	cmd := exec.Command(state.State.Config.FfmpegExecutable,
		"-i", inputPath,
		"-fs", "800000",
		"-vf", fmt.Sprintf("fps=15,scale=%s,format=rgba,pad=%s:color=#00000000", scale, pad),
		outputPath,
	)

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("failed to execute ffmpeg command: %s", err)
	}

	outputData, err := os.ReadFile(outputPath)
	if err != nil {
		return nil, err
	}

	if outputDataWithExif, err := WebpWriteExifData(outputData, updateId); err == nil {
		return outputDataWithExif, nil
	}

	return outputData, nil
}

func WebpImagePad(inputData []byte, wPad, hPad int, updateId int64) ([]byte, error) {
	webpDecoder, err := decoder.NewDecoder(bytes.NewBuffer(inputData), &decoder.Options{NoFancyUpsampling: true})
	if err != nil {
		return nil, fmt.Errorf("failed to create a webp decoder: %s", err)
	}

	inputImage, err := webpDecoder.Decode()
	if err != nil {
		return nil, fmt.Errorf("failed to decode webp image: %s", err)
	}

	var (
		wOffset = wPad / 2
		hOffset = hPad / 2
	)

	outputWidth := inputImage.Bounds().Dx() + wPad
	outputHeight := inputImage.Bounds().Dy() + hPad

	outputImage := image.NewRGBA(image.Rect(0, 0, outputWidth, outputHeight))
	draw.Draw(outputImage, image.Rect(wOffset, hOffset, outputWidth-wOffset, outputHeight-hOffset), inputImage, image.Point{}, draw.Src)

	encoderOptions, err := encoder.NewLossyEncoderOptions(encoder.PresetDefault, 100)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize encoder options: %s", err)
	}

	var outputBuffer bytes.Buffer
	if err = webp.Encode(&outputBuffer, outputImage, encoderOptions); err != nil {
		return nil, fmt.Errorf("failed to encode into webp: %s", err)
	}

	if outputData, err := WebpWriteExifData(outputBuffer.Bytes(), updateId); err == nil {
		return outputData, nil
	}

	return outputBuffer.Bytes(), nil
}

func AnimatedWebpConvertToGif(inputData []byte, updateId string) ([]byte, error) {
	var (
		logger = state.State.Logger

		currPath   = path.Join("downloads", updateId)
		inputPath  = path.Join(currPath, "input.webp")
		outputPath = path.Join(currPath, "output.gif")
	)
	defer logger.Sync()

	if err := os.MkdirAll(currPath, os.ModePerm); err != nil {
		return nil, err
	}
	defer os.RemoveAll(currPath)

	if err := os.WriteFile(inputPath, inputData, os.ModePerm); err != nil {
		return nil, err
	}

	cmd := exec.Command("convert",
		inputPath,
		"-loop", "0",
		"-dispose", "previous",
		outputPath,
	)

	if err := cmd.Run(); err != nil {
		logger.Debug("failed to run convert command",
			zap.Error(err),
		)
		return nil, err
	}

	return os.ReadFile(outputPath)
}

func WebpWriteExifData(inputData []byte, updateId int64) ([]byte, error) {
	var (
		cfg           = state.State.Config
		logger        = state.State.Logger
		startingBytes = []byte{0x49, 0x49, 0x2A, 0x00, 0x08, 0x00, 0x00, 0x00, 0x01, 0x00, 0x41, 0x57, 0x07, 0x00}
		endingBytes   = []byte{0x16, 0x00, 0x00, 0x00}
		b             bytes.Buffer

		currUpdateId = strconv.FormatInt(updateId, 10)
		currPath     = path.Join("downloads", currUpdateId)
		inputPath    = path.Join(currPath, "input_exif.webm")
		outputPath   = path.Join(currPath, "output_exif.webp")
		exifDataPath = path.Join(currPath, "raw.exif")
	)
	defer logger.Sync()

	if _, err := b.Write(startingBytes); err != nil {
		return nil, err
	}

	jsonData := map[string]interface{}{
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

	if err := os.MkdirAll(currPath, os.ModePerm); err != nil {
		return nil, err
	}
	defer os.RemoveAll(currPath)

	if err := os.WriteFile(inputPath, inputData, os.ModePerm); err != nil {
		return nil, err
	}
	if err := os.WriteFile(exifDataPath, b.Bytes(), os.ModePerm); err != nil {
		return nil, err
	}

	cmd := exec.Command("webpmux",
		"-set", "exif",
		exifDataPath, inputPath,
		"-o", outputPath,
	)

	if err := cmd.Run(); err != nil {
		logger.Debug("failed to run webpmux command",
			zap.Error(err),
		)
		return nil, err
	}

	return os.ReadFile(outputPath)
}
