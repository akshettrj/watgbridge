package utils

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"os"
	"os/exec"
	"path"
	"strconv"
	"time"

	"watgbridge/state"

	"github.com/Benau/tgsconverter/libtgsconverter"
	"github.com/kolesa-team/go-webp/decoder"
	"github.com/kolesa-team/go-webp/encoder"
	"github.com/kolesa-team/go-webp/webp"
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

func WebpImagePad(inputData []byte, wPad, hPad int) ([]byte, error) {
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
	draw.Draw(outputImage, outputImage.Bounds(), &image.Uniform{color.Black}, image.Point{}, draw.Src)
	draw.Draw(outputImage, image.Rect(wOffset, hOffset, outputWidth-wOffset, outputHeight-hOffset), inputImage, image.Point{}, draw.Src)

	encoderOptions, err := encoder.NewLossyEncoderOptions(encoder.PresetDefault, 100)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize encoder options: %s", err)
	}

	var outputBuffer bytes.Buffer
	if err = webp.Encode(&outputBuffer, outputImage, encoderOptions); err != nil {
		return nil, fmt.Errorf("failed to encode into webp: %s", err)
	}

	return outputBuffer.Bytes(), nil
}
