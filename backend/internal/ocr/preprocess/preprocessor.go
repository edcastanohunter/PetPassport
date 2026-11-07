package preprocess

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
	"math"

	xdraw "golang.org/x/image/draw"
	"golang.org/x/image/webp"

	ocrdomain "github.com/edcastanohunter/petpassport/backend/internal/ocr/domain"
)

var (
	ErrTooManyPixels = errors.New("preprocess: imagen excede el límite de 25 megapíxeles")
	ErrUnsupported   = errors.New("preprocess: formato de imagen no soportado")
)

const (
	MaxPixels   = 25 << 20 // 25 MP
	DefaultSide = 1600     // 1600 px lado mayor (PP-F4-OCR-002)
	HighResSide = 2400     // 2400 px si densidad de texto alta
	JPEGQuality = 85
)

// Preprocessor implementa ocrdomain.Preprocessor (PP-F4-OCR-002).
type Preprocessor struct {
	maxPixels int
}

func NewPreprocessor() *Preprocessor {
	return &Preprocessor{
		maxPixels: MaxPixels,
	}
}

func (p *Preprocessor) ProcessPage(ctx context.Context, pageNum int, rawData []byte) (ocrdomain.PreprocessedPage, error) {
	if len(rawData) == 0 {
		return ocrdomain.PreprocessedPage{}, errors.New("preprocess: datos vacíos")
	}

	head := rawData
	if len(head) > 512 {
		head = head[:512]
	}

	// HEIC check
	if len(head) >= 12 && string(head[4:8]) == "ftyp" {
		brand := string(head[8:12])
		if brand == "heic" || brand == "heix" || brand == "mif1" || brand == "hevc" {
			return ocrdomain.PreprocessedPage{}, ErrUnsupported
		}
	}

	// Decodificación
	srcImg, err := decodeImage(rawData)
	if err != nil {
		return ocrdomain.PreprocessedPage{}, fmt.Errorf("decodificar imagen: %w", err)
	}

	bounds := srcImg.Bounds()
	w, h := bounds.Dx(), bounds.Dy()
	if int64(w)*int64(h) > int64(p.maxPixels) {
		return ocrdomain.PreprocessedPage{}, ErrTooManyPixels
	}

	// Convertir a escala de grises para análisis de calidad
	gray := toGray(srcImg)

	// Métricas de calidad
	lapVar := computeLaplacianVariance(gray)
	brightness := computeMeanBrightness(gray)
	edgeDensity := computeEdgeDensity(gray)

	// Puntuación de calidad normalizada 0.0 - 1.0
	qualityScore, isBlurry, isDark, warnings := evaluateQuality(lapVar, brightness, edgeDensity)

	// Mejora de contraste (min-max normalization)
	enhanced := enhanceContrast(gray)

	// Determinar resolución de salida
	targetMaxSide := DefaultSide
	if edgeDensity > 0.15 {
		targetMaxSide = HighResSide
	}

	// Redimensionar preservando relación de aspecto
	resized := resizeImage(enhanced, targetMaxSide)
	resBounds := resized.Bounds()

	// Codificar a JPEG de alta fidelidad
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, resized, &jpeg.Options{Quality: JPEGQuality}); err != nil {
		return ocrdomain.PreprocessedPage{}, fmt.Errorf("codificar jpeg: %w", err)
	}

	outBytes := buf.Bytes()
	sum := sha256.Sum256(outBytes)

	return ocrdomain.PreprocessedPage{
		PageNumber:   pageNum,
		Data:         outBytes,
		ContentType:  "image/jpeg",
		Width:        resBounds.Dx(),
		Height:       resBounds.Dy(),
		Sha256:       sum[:],
		QualityScore: qualityScore,
		IsTooBlurry:  isBlurry,
		IsTooDark:    isDark,
		Warnings:     warnings,
	}, nil
}

func decodeImage(data []byte) (image.Image, error) {
	// Intentar formatos estándar
	img, _, err := image.Decode(bytes.NewReader(data))
	if err == nil {
		return img, nil
	}

	// Probar decodificador WebP
	if img, errWebP := webp.Decode(bytes.NewReader(data)); errWebP == nil {
		return img, nil
	}

	// Intentar JPEG / PNG directo
	if jImg, errJ := jpeg.Decode(bytes.NewReader(data)); errJ == nil {
		return jImg, nil
	}
	if pImg, errP := png.Decode(bytes.NewReader(data)); errP == nil {
		return pImg, nil
	}

	return nil, ErrUnsupported
}

func toGray(src image.Image) *image.Gray {
	bounds := src.Bounds()
	gray := image.NewGray(bounds)
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			gray.Set(x, y, color.GrayModel.Convert(src.At(x, y)))
		}
	}
	return gray
}

// computeLaplacianVariance calcula la varianza de la convolución Laplaciana (detección de desenfoque).
func computeLaplacianVariance(gray *image.Gray) float64 {
	bounds := gray.Bounds()
	w, h := bounds.Dx(), bounds.Dy()
	if w < 3 || h < 3 {
		return 0
	}

	var sum float64
	var sumSq float64
	var count float64

	// Convolución con kernel 3x3:
	//  0  1  0
	//  1 -4  1
	//  0  1  0
	for y := 1; y < h-1; y++ {
		for x := 1; x < w-1; x++ {
			c := float64(gray.GrayAt(x, y).Y)
			top := float64(gray.GrayAt(x, y-1).Y)
			bot := float64(gray.GrayAt(x, y+1).Y)
			left := float64(gray.GrayAt(x-1, y).Y)
			right := float64(gray.GrayAt(x+1, y).Y)

			lap := top + bot + left + right - 4*c
			sum += lap
			sumSq += lap * lap
			count++
		}
	}

	if count == 0 {
		return 0
	}
	mean := sum / count
	variance := (sumSq / count) - (mean * mean)
	return math.Max(0, variance)
}

func computeMeanBrightness(gray *image.Gray) float64 {
	bounds := gray.Bounds()
	w, h := bounds.Dx(), bounds.Dy()
	if w == 0 || h == 0 {
		return 0
	}

	var sum uint64
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			sum += uint64(gray.GrayAt(x, y).Y)
		}
	}
	return float64(sum) / float64(w*h)
}

func computeEdgeDensity(gray *image.Gray) float64 {
	bounds := gray.Bounds()
	w, h := bounds.Dx(), bounds.Dy()
	if w < 2 || h < 2 {
		return 0
	}

	var edgePixels uint64
	var totalPixels uint64

	for y := 0; y < h-1; y++ {
		for x := 0; x < w-1; x++ {
			c := int(gray.GrayAt(x, y).Y)
			dx := int(gray.GrayAt(x+1, y).Y) - c
			dy := int(gray.GrayAt(x, y+1).Y) - c
			grad := math.Sqrt(float64(dx*dx + dy*dy))
			if grad > 25.0 {
				edgePixels++
			}
			totalPixels++
		}
	}

	if totalPixels == 0 {
		return 0
	}
	return float64(edgePixels) / float64(totalPixels)
}

func evaluateQuality(lapVar, brightness, edgeDensity float64) (float64, bool, bool, []string) {
	var warnings []string
	isBlurry := false
	isDark := false

	// Blur score: varianza < 50 muy borrosa, > 300 nítida
	blurScore := math.Min(1.0, lapVar/300.0)
	if lapVar < 40.0 {
		isBlurry = true
		warnings = append(warnings, "Imagen con desenfoque pronunciado (posible movimiento)")
	}

	// Brillo score: ideal 100-180
	var brightScore float64
	if brightness < 40.0 {
		isDark = true
		brightScore = math.Max(0.1, brightness/40.0*0.5)
		warnings = append(warnings, "Imagen excesivamente oscura (iluminación deficiente)")
	} else if brightness > 230.0 {
		brightScore = math.Max(0.1, (255.0-brightness)/25.0*0.5)
		warnings = append(warnings, "Imagen sobreexpuesta o con reflejos")
	} else {
		// Óptimo
		brightScore = 1.0 - math.Abs(brightness-140.0)/120.0
	}

	// Densidad de bordes: textos contienen típicamente 5% a 20% de bordes
	densityScore := math.Min(1.0, edgeDensity/0.10)

	quality := 0.50*blurScore + 0.30*brightScore + 0.20*densityScore
	quality = math.Max(0.05, math.Min(1.0, quality))

	return math.Round(quality*100) / 100, isBlurry, isDark, warnings
}

func enhanceContrast(src *image.Gray) *image.Gray {
	bounds := src.Bounds()
	w, h := bounds.Dx(), bounds.Dy()
	if w == 0 || h == 0 {
		return src
	}

	// Encontrar min y max con 1% percentile clipping
	var hist [256]int
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			hist[src.GrayAt(x, y).Y]++
		}
	}

	total := w * h
	p1 := total / 100
	p99 := total - p1

	var cum int
	minVal, maxVal := 0, 255
	for i := 0; i < 256; i++ {
		cum += hist[i]
		if cum >= p1 && minVal == 0 {
			minVal = i
		}
		if cum >= p99 {
			maxVal = i
			break
		}
	}

	if maxVal <= minVal {
		return src
	}

	out := image.NewGray(bounds)
	scale := 255.0 / float64(maxVal-minVal)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			val := int(src.GrayAt(x, y).Y)
			if val < minVal {
				out.SetGray(x, y, color.Gray{Y: 0})
			} else if val > maxVal {
				out.SetGray(x, y, color.Gray{Y: 255})
			} else {
				norm := uint8(math.Min(255, math.Max(0, float64(val-minVal)*scale)))
				out.SetGray(x, y, color.Gray{Y: norm})
			}
		}
	}
	return out
}

func resizeImage(src image.Image, maxSide int) image.Image {
	bounds := src.Bounds()
	w, h := bounds.Dx(), bounds.Dy()

	if w <= maxSide && h <= maxSide {
		return src
	}

	var nw, nh int
	if w >= h {
		nw = maxSide
		nh = int(float64(h) * float64(maxSide) / float64(w))
	} else {
		nh = maxSide
		nw = int(float64(w) * float64(maxSide) / float64(h))
	}

	if nw <= 0 {
		nw = 1
	}
	if nh <= 0 {
		nh = 1
	}

	dst := image.NewRGBA(image.Rect(0, 0, nw, nh))
	xdraw.BiLinear.Scale(dst, dst.Bounds(), src, bounds, xdraw.Over, nil)
	return dst
}

// Ensure interface compatibility
var _ ocrdomain.Preprocessor = (*Preprocessor)(nil)
