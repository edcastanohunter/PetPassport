package preprocess

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"testing"
)

func createTestJPEG(w, h int, c color.Color) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(img, img.Bounds(), &image.Uniform{C: c}, image.Point{}, draw.Src)
	var buf bytes.Buffer
	_ = jpeg.Encode(&buf, img, &jpeg.Options{Quality: 85})
	return buf.Bytes()
}

func createPatternedJPEG(w, h int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if (x/10+y/10)%2 == 0 {
				img.Set(x, y, color.White)
			} else {
				img.Set(x, y, color.Black)
			}
		}
	}
	var buf bytes.Buffer
	_ = jpeg.Encode(&buf, img, &jpeg.Options{Quality: 85})
	return buf.Bytes()
}

func TestPreprocessor_ProcessPage(t *testing.T) {
	p := NewPreprocessor()
	ctx := context.Background()

	// Sharp patterned image
	data := createPatternedJPEG(400, 300)
	page, err := p.ProcessPage(ctx, 1, data)
	if err != nil {
		t.Fatalf("expected successful processing, got %v", err)
	}

	if page.PageNumber != 1 {
		t.Errorf("expected page 1, got %d", page.PageNumber)
	}
	if page.Width != 400 || page.Height != 300 {
		t.Errorf("expected dimensions 400x300, got %dx%d", page.Width, page.Height)
	}
	if page.QualityScore < 0.4 {
		t.Errorf("expected sharp pattern to have good quality score >= 0.4, got %f", page.QualityScore)
	}
	if len(page.Sha256) != 32 {
		t.Errorf("expected 32-byte sha256 hash, got %d", len(page.Sha256))
	}
}

func TestPreprocessor_DarkImage(t *testing.T) {
	p := NewPreprocessor()
	ctx := context.Background()

	// Very dark image
	data := createTestJPEG(200, 200, color.RGBA{R: 10, G: 10, B: 10, A: 255})
	page, err := p.ProcessPage(ctx, 1, data)
	if err != nil {
		t.Fatalf("process dark image failed: %v", err)
	}

	if !page.IsTooDark {
		t.Errorf("expected page to be flagged as too dark")
	}
	if page.QualityScore > 0.35 {
		t.Errorf("expected low quality score for pitch black image, got %f", page.QualityScore)
	}
}

func TestPreprocessor_TooLarge(t *testing.T) {
	p := &Preprocessor{maxPixels: 1000} // artificial low limit
	ctx := context.Background()

	data := createTestJPEG(100, 100, color.White) // 10,000 pixels > 1,000
	_, err := p.ProcessPage(ctx, 1, data)
	if err == nil {
		t.Errorf("expected ErrTooManyPixels")
	}
}
