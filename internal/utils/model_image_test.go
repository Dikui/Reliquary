package utils

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"image/png"
	"strings"
	"testing"
)

// Regression: a 1456×5 image was repeatedly rejected by the vision embedder.
func TestPrepareModelImagePadsThinCopyAndKeepsOriginal(t *testing.T) {
	source := image.NewRGBA(image.Rect(0, 0, 1456, 5))
	ink := color.RGBA{R: 128, A: 255}
	draw.Draw(source, source.Bounds(), image.NewUniform(ink), image.Point{}, draw.Src)
	var original bytes.Buffer
	if err := png.Encode(&original, source); err != nil {
		t.Fatal(err)
	}
	before := append([]byte(nil), original.Bytes()...)
	prepared, err := PrepareModelImage(context.Background(), bytes.NewReader(original.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	output, _, err := image.Decode(bytes.NewReader(prepared.Data))
	if err != nil {
		t.Fatal(err)
	}
	if output.Bounds().Dx() != 1456 || output.Bounds().Dy() != 32 {
		t.Fatalf("expected padded 1456×32 copy, got %v", output.Bounds())
	}
	for y := 0; y < 32; y++ {
		want := color.RGBA{R: 255, G: 255, B: 255, A: 255}
		if y >= 13 && y < 18 {
			want = ink
		}
		for x := 0; x < 1456; x++ {
			if got := color.RGBAModel.Convert(output.At(x, y)); got != want {
				t.Fatalf("pixel (%d,%d): got %v, want %v", x, y, got, want)
			}
		}
	}
	if !bytes.Equal(before, original.Bytes()) || prepared.SourceHash != fmt.Sprintf("%x", sha256.Sum256(before)) {
		t.Fatal("padding must preserve the original bytes and hash")
	}
}

func TestPrepareModelImageShrinksCopyAndKeepsOriginalHash(t *testing.T) {
	var original bytes.Buffer
	if err := png.Encode(&original, image.NewNRGBA(image.Rect(0, 0, 5000, 40))); err != nil {
		t.Fatal(err)
	}
	before := append([]byte(nil), original.Bytes()...)
	prepared, err := PrepareModelImage(context.Background(), bytes.NewReader(original.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(prepared.Data))
	if err != nil || format != "png" || config.Width != 4096 || config.Height > 40 {
		t.Fatalf("unexpected derived image: %+v %s %v", config, format, err)
	}
	if !bytes.Equal(before, original.Bytes()) || bytes.Equal(prepared.Data, before) || prepared.SourceHash != fmt.Sprintf("%x", sha256.Sum256(before)) {
		t.Fatal("derived image must keep original bytes and original hash")
	}
}

func TestPrepareModelImageRejectsDecodeBudgetBeforePixelDecode(t *testing.T) {
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, image.NewNRGBA64(image.Rect(0, 0, 1, 1))); err != nil {
		t.Fatal(err)
	}
	data := encoded.Bytes()
	binary.BigEndian.PutUint32(data[16:20], 20000)
	binary.BigEndian.PutUint32(data[20:24], 10000)
	data[24] = 16
	binary.BigEndian.PutUint32(data[29:33], crc32.ChecksumIEEE(data[12:29]))
	err := ValidateIngestImage(data)
	if !IsPermanentImageError(err) || !strings.Contains(err.Error(), "内存预算") {
		t.Fatalf("expected permanent decode budget error, got %v", err)
	}
	_, err = PrepareModelImage(context.Background(), bytes.NewReader(data))
	if !IsPermanentImageError(err) || !strings.Contains(err.Error(), "内存预算") {
		t.Fatalf("expected rejection before trying incomplete pixel data, got %v", err)
	}
}

func TestValidateIngestImageAllowsLargeSequentialJPEGButBoundsProgressive(t *testing.T) {
	var encoded bytes.Buffer
	if err := jpeg.Encode(&encoded, image.NewRGBA(image.Rect(0, 0, 16, 16)), nil); err != nil {
		t.Fatal(err)
	}
	data := encoded.Bytes()
	sof := bytes.Index(data, []byte{0xff, 0xc0})
	if sof < 0 {
		t.Fatal("JPEG fixture has no sequential frame")
	}
	binary.BigEndian.PutUint16(data[sof+5:sof+7], 8488)
	binary.BigEndian.PutUint16(data[sof+7:sof+9], 15197)
	if err := ValidateIngestImage(data); err != nil {
		t.Fatalf("sequential JPEG matching the rejected original must fit the budget: %v", err)
	}
	data[sof+1] = 0xc2
	// A marker inside APP metadata must not lower a progressive JPEG's budget.
	progressive := append([]byte{0xff, 0xd8, 0xff, 0xe1, 0, 6, 0xff, 0xc0, 0, 0}, data[2:]...)
	if err := ValidateIngestImage(progressive); !IsPermanentImageError(err) || !strings.Contains(err.Error(), "内存预算") {
		t.Fatalf("progressive JPEG must retain the conservative budget: %v", err)
	}
}
