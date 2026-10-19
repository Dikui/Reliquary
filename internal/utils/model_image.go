package utils

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"math"

	_ "golang.org/x/image/bmp"
	"golang.org/x/image/draw"
	_ "golang.org/x/image/tiff"
	_ "golang.org/x/image/webp"
)

const (
	MaxImagePixels       = 200_000_000
	maxModelImagePixels  = 8_000_000
	maxModelImageSide    = 4096
	maxModelImageBytes   = 8 << 20
	maxImageDecodeMemory = 1536 << 20
)

var modelImageSlot = make(chan struct{}, 1)

type ModelImage struct {
	Data       []byte
	SourceHash string
}

type ImageInputError struct{ Reason string }

func (e *ImageInputError) Error() string { return e.Reason }

func IsPermanentImageError(err error) bool {
	var target *ImageInputError
	return errors.As(err, &target)
}

// ValidateIngestImage checks the original bytes without decoding pixel buffers.
func ValidateIngestImage(data []byte) error {
	_, _, err := modelImageConfig(data)
	return err
}

func modelImageConfig(data []byte) (image.Config, string, error) {
	invalid := func(reason string) (image.Config, string, error) {
		return image.Config{}, "", &ImageInputError{Reason: reason}
	}
	if bytes.HasPrefix(data, []byte("GIF87a")) || bytes.HasPrefix(data, []byte("GIF89a")) {
		return invalid("不支持 GIF 图片，请转换为 PNG、JPEG 或 WebP 后重新提交")
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return invalid("图片格式不支持或图片已损坏：" + err.Error())
	}
	if config.Width <= 0 || config.Height <= 0 || config.Width > MaxImagePixels/config.Height {
		return invalid("图片像素超过 2 亿上限，请缩小图片后重新提交")
	}
	pixels := int64(config.Width) * int64(config.Height)
	bytesPerPixel := int64(32)
	if format == "jpeg" && isSequentialJPEG(data) {
		// Cover decoded planes plus RGB/CMYK conversion; progressive scans retain extra coefficients.
		bytesPerPixel = 8
	} else if format == "png" && len(data) >= 29 {
		bytesPerPixel = 4
		if data[24] == 16 {
			bytesPerPixel = 8
		}
		// Adam7 allocates a full image plus the decoded passes.
		if data[28] == 1 {
			bytesPerPixel *= 2
		}
	}
	// Reserve space for the output image, encoding buffers and decoder overhead.
	if pixels*bytesPerPixel+int64(len(data))+(128<<20) > maxImageDecodeMemory {
		return invalid("图片解码预计超过 1.5 GiB 内存预算，请缩小图片后重新提交")
	}
	return config, format, nil
}

func isSequentialJPEG(data []byte) bool {
	for offset := 2; offset+4 <= len(data); {
		if data[offset] != 0xff {
			return false
		}
		marker := data[offset+1]
		if marker == 0xff {
			offset++
			continue
		}
		switch marker {
		case 0xc0, 0xc1:
			return true
		case 0xc2, 0xda, 0xd9:
			return false
		}
		length := int(data[offset+2])<<8 | int(data[offset+3])
		if length < 2 || length > len(data)-offset-2 {
			return false
		}
		offset += 2 + length
	}
	return false
}

// PrepareModelImage keeps the original hash while producing bounded model input.
func PrepareModelImage(ctx context.Context, reader io.Reader) (*ModelImage, error) {
	select {
	case modelImageSlot <- struct{}{}:
		defer func() { <-modelImageSlot }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	limit := GetMaxFileSize()
	data, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, &ImageInputError{Reason: "图片文件超过上传大小上限，请缩小图片后重新提交"}
	}
	config, format, err := modelImageConfig(data)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	source, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, &ImageInputError{Reason: "图片无法解码，可能已损坏：" + err.Error()}
	}
	result := &ModelImage{SourceHash: fmt.Sprintf("%x", sha256.Sum256(data))}
	scale := math.Min(1, math.Min(float64(maxModelImageSide)/float64(max(config.Width, config.Height)),
		math.Sqrt(float64(maxModelImagePixels)/(float64(config.Width)*float64(config.Height)))))
	if scale == 1 && len(data) <= maxModelImageBytes && (format == "jpeg" || format == "png" || format == "webp") {
		result.Data = data
		return result, nil
	}
	width, height := max(1, int(float64(config.Width)*scale)), max(1, int(float64(config.Height)*scale))
	output := image.NewRGBA(image.Rect(0, 0, width, height))
	// ApproxBiLinear avoids the source-height-sized intermediate buffer of kernels.
	draw.ApproxBiLinear.Scale(output, output.Bounds(), source, source.Bounds(), draw.Src, nil)
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, output); err != nil {
		return nil, err
	}
	if encoded.Len() <= maxModelImageBytes {
		result.Data = bytes.Clone(encoded.Bytes())
		return result, nil
	}
	// JPEG has no alpha channel; composite transparency onto white before encoding.
	flattened := image.NewRGBA(output.Bounds())
	draw.Draw(flattened, flattened.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	draw.Draw(flattened, flattened.Bounds(), output, image.Point{}, draw.Over)
	for attempt := 0; attempt < 4; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		encoded.Reset()
		if err := jpeg.Encode(&encoded, flattened, &jpeg.Options{Quality: 85}); err != nil {
			return nil, err
		}
		if encoded.Len() <= maxModelImageBytes {
			result.Data = bytes.Clone(encoded.Bytes())
			return result, nil
		}
		width, height = max(1, width*3/4), max(1, height*3/4)
		smaller := image.NewRGBA(image.Rect(0, 0, width, height))
		draw.ApproxBiLinear.Scale(smaller, smaller.Bounds(), flattened, flattened.Bounds(), draw.Src, nil)
		flattened = smaller
	}
	return nil, &ImageInputError{Reason: "图片压缩后仍超过 8 MiB，请缩小图片后重新提交"}
}
