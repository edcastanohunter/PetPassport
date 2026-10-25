package image

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"
)

func makeJPEG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for x := 0; x < w; x++ {
		for y := 0; y < h; y++ {
			img.Set(x, y, color.RGBA{uint8(x % 256), uint8(y % 256), 128, 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestProcess_JPEGToOutput(t *testing.T) {
	src := makeJPEG(t, 2000, 1500)
	res, err := Process(bytes.NewReader(src))
	if err != nil {
		t.Fatal(err)
	}
	if res.ContentType != OutputContentType || len(res.Full) == 0 || len(res.Thumb) == 0 {
		t.Fatalf("resultado: %+v", res)
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(res.Full))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Width > MaxSide || cfg.Height > MaxSide {
		t.Fatalf("foto no reducida: %dx%d", cfg.Width, cfg.Height)
	}
	cfgT, _, err := image.DecodeConfig(bytes.NewReader(res.Thumb))
	if err != nil {
		t.Fatal(err)
	}
	if cfgT.Width > ThumbSide || cfgT.Height > ThumbSide {
		t.Fatalf("miniatura no reducida: %dx%d", cfgT.Width, cfgT.Height)
	}
}

func TestProcess_RejectsNonImage(t *testing.T) {
	if _, err := Process(bytes.NewReader([]byte("<svg onload=alert(1)>"))); err == nil {
		t.Fatal("SVG debe rechazarse (SEC-004)")
	}
	if _, err := Process(bytes.NewReader([]byte("GIF89a...."))); err == nil {
		t.Fatal("GIF debe rechazarse (SEC-004)")
	}
	heic := append([]byte{0, 0, 0, 24}, []byte("ftypheic....")...)
	if _, err := Process(bytes.NewReader(heic)); err != ErrUnsupported {
		t.Fatalf("HEIC debe dar ErrUnsupported, got %v", err)
	}
}

func TestProcess_RejectsHugePixels(t *testing.T) {
	// PNG con cabecera de dimensiones enormes (zip-bomb): IHDR parcheado con
	// CRC recalculado para que DecodeConfig acepte la cabecera.
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 10, 10))); err != nil {
		t.Fatal(err)
	}
	data := buf.Bytes()
	data[16], data[17], data[18], data[19] = 0x00, 0x0E, 0xA4, 0x80 // width 960000
	data[20], data[21], data[22], data[23] = 0x00, 0x0E, 0xA4, 0x80 // height 960000
	binary.BigEndian.PutUint32(data[29:33], crc32.ChecksumIEEE(data[12:29]))
	if _, err := Process(bytes.NewReader(data)); err != ErrTooManyPixels {
		t.Fatalf("píxeles excesivos deben rechazarse, got %v", err)
	}
}
