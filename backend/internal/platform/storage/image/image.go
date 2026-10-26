// Package image implementa el pipeline de fotos (ARQ-012, SEC-004):
// límite 5 MB → detección por magic bytes (el Content-Type del cliente se
// ignora) → decodificación con límite de píxeles (≤ 25 MP, contra zip-bomb)
// → strip de metadatos (la re-codificación no conserva EXIF) → redimensionar
// a 1024 px y miniatura 256 px → re-codificar.
package image

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"time"

	xdraw "golang.org/x/image/draw"
)

// Límites del pipeline (ARQ-012, PRD-008).
const (
	MaxUploadBytes = 5 << 20  // 5 MB
	MaxPixels      = 25 << 20 // 25 MP
	MaxSide        = 1024     // foto principal
	ThumbSide      = 256      // miniatura
	JPEGQuality    = 82
	DecodeTimeout  = 10 * time.Second
)

// ContentType del objeto resultante.
const OutputContentType = "image/jpeg"

// Errores del pipeline; http los mapea al catálogo.
var (
	ErrTooLarge       = errors.New("photo: archivo mayor a 5 MB")
	ErrUnsupported    = errors.New("photo: formato no soportado") // → 415 UNSUPPORTED_MEDIA_TYPE
	ErrNotAnImage     = errors.New("photo: no es una imagen")
	ErrTooManyPixels  = errors.New("photo: imagen demasiado grande")
	ErrIncompleteBody = errors.New("photo: cuerpo incompleto")
)

// Result del pipeline.
type Result struct {
	Full        []byte
	Thumb       []byte
	ContentType string // "image/jpeg"
}

// magic bytes de los formatos admitidos (SVG y GIF se rechazan, SEC-004).
var (
	pngMagic  = []byte{0x89, 'P', 'N', 'G'}
	jpegMagic = []byte{0xFF, 0xD8, 0xFF}
	webpMagic = []byte{'R', 'I', 'F', 'F'}
)

// Process valida, decodifica y produce foto + miniatura. El cuerpo se
// bufferiza (≤ 5 MB) porque DecodeConfig y Decode necesitan posición inicial;
// la cota de memoria está garantizada por el límite de lectura.
func Process(r io.Reader) (Result, error) {
	if r == nil {
		return Result{}, ErrIncompleteBody
	}
	limited := io.LimitReader(r, MaxUploadBytes+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return Result{}, fmt.Errorf("photo: leer cuerpo: %w", err)
	}
	if len(data) > MaxUploadBytes {
		return Result{}, ErrTooLarge
	}
	head := data
	if len(head) > 512 {
		head = head[:512]
	}

	// HEIC: ftyp...heic/heix/mif1 — se rechaza con mensaje claro (SEC-004:
	// decisión M1.2 sin libheif).
	if len(head) >= 12 && string(head[4:8]) == "ftyp" {
		brand := string(head[8:12])
		if brand == "heic" || brand == "heix" || brand == "mif1" || brand == "hevc" {
			return Result{}, ErrUnsupported
		}
	}

	var img image.Image
	switch {
	case bytes.HasPrefix(head, jpegMagic):
		img, err = decodeLimited(data, jpeg.Decode)
	case bytes.HasPrefix(head, pngMagic):
		img, err = decodeLimited(data, png.Decode)
	case len(head) >= 12 && bytes.HasPrefix(head, webpMagic) && string(head[8:12]) == "WEBP":
		// WebP de entrada se acepta y se convierte a JPEG de salida.
		img, err = decodeWebP(data)
	default:
		return Result{}, ErrNotAnImage
	}
	if err != nil {
		if errors.Is(err, ErrTooManyPixels) || errors.Is(err, ErrUnsupported) {
			return Result{}, err
		}
		return Result{}, ErrNotAnImage
	}

	full := resize(img, MaxSide)
	thumb := resize(img, ThumbSide)
	var bufFull, bufThumb bytes.Buffer
	if err := jpeg.Encode(&bufFull, full, &jpeg.Options{Quality: JPEGQuality}); err != nil {
		return Result{}, fmt.Errorf("photo: codificar: %w", err)
	}
	if err := jpeg.Encode(&bufThumb, thumb, &jpeg.Options{Quality: JPEGQuality}); err != nil {
		return Result{}, fmt.Errorf("photo: codificar miniatura: %w", err)
	}
	return Result{Full: bufFull.Bytes(), Thumb: bufThumb.Bytes(), ContentType: OutputContentType}, nil
}

// decodeLimited decodifica con cota de píxeles ANTES de materializar el mapa
// (image.DecodeConfig lee solo la cabecera; data se re-lee desde el buffer).
func decodeLimited(data []byte, dec func(io.Reader) (image.Image, error)) (image.Image, error) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	if cfg.Width*cfg.Height > MaxPixels {
		return nil, ErrTooManyPixels
	}
	return dec(bytes.NewReader(data))
}

// decodeWebP decodifica WebP de entrada. golang.org/x/image/webp es solo
// lectura, suficiente para convertir a JPEG.
func decodeWebP(data []byte) (image.Image, error) {
	cfg, err := webpConfig(data)
	if err != nil {
		return nil, err
	}
	if cfg.Width*cfg.Height > MaxPixels {
		return nil, ErrTooManyPixels
	}
	return webpDecode(data)
}

// resize escala manteniendo proporción (catmull rom, calidad razonable y
// pura Go; sin dependencias con cgo).
func resize(src image.Image, maxSide int) image.Image {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= maxSide && h <= maxSide {
		return src
	}
	scale := float64(maxSide) / float64(max(w, h))
	nw, nh := int(float64(w)*scale), int(float64(h)*scale)
	if nw < 1 {
		nw = 1
	}
	if nh < 1 {
		nh = 1
	}
	dst := image.NewRGBA(image.Rect(0, 0, nw, nh))
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), src, b, xdraw.Over, nil)
	return dst
}
