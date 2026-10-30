// Package document valida adjuntos clínicos (PP-F2-ARQ-011, SEC-003):
// PDF con pdfcpu (cabecera %PDF-, ≤ 10 MB, ≤ 50 páginas, sin cifrado,
// JavaScript, formularios, acciones de lanzamiento ni ficheros embebidos) e
// imágenes JPEG/PNG/WebP re-codificadas a resolución completa (radiografías y
// laboratorio: no se reducen). El Content-Type del cliente se ignora: el tipo
// se decide por magic bytes.
package document

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"regexp"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
	"golang.org/x/image/webp"
)

// Límites (SEC-003, DOM-004).
const (
	MaxBytes    = 10 << 20 // 10 MB
	MaxPages    = 50
	MaxPixels   = 40 << 20 // 40 MP (radiografías digitales grandes)
	JPEGQuality = 92
)

// Kind es el tipo resultante del adjunto (coincide con domain.ContentType).
type Kind string

// Tipos de salida.
const (
	KindJPEG Kind = "jpeg"
	KindPNG  Kind = "png"
	KindPDF  Kind = "pdf"
)

// MIME devuelve el Content-Type fijado por el servidor (SEC-003).
func (k Kind) MIME() string {
	switch k {
	case KindJPEG:
		return "image/jpeg"
	case KindPNG:
		return "image/png"
	case KindPDF:
		return "application/pdf"
	}
	return "application/octet-stream"
}

// Errores; http los mapea al catálogo (413/415/422).
var (
	ErrTooLarge      = errors.New("document: archivo mayor a 10 MB")
	ErrUnsupported   = errors.New("document: formato no soportado")
	ErrInvalidPDF    = errors.New("document: PDF inválido")
	ErrUnsafePDF     = errors.New("document: PDF con contenido activo o embebido")
	ErrTooManyPages  = errors.New("document: PDF con más de 50 páginas")
	ErrTooManyPixels = errors.New("document: imagen demasiado grande")
	ErrEmpty         = errors.New("document: falta el archivo")
)

// Result es el adjunto validado listo para storage.
type Result struct {
	Data []byte
	Kind Kind
}

var (
	pdfMagic  = []byte("%PDF-")
	pngMagic  = []byte{0x89, 'P', 'N', 'G'}
	jpegMagic = []byte{0xFF, 0xD8, 0xFF}
	riffMagic = []byte("RIFF")
)

// Process lee (≤ 10 MB), detecta el tipo y valida o re-codifica.
func Process(ctx context.Context, r io.Reader) (Result, error) {
	if r == nil {
		return Result{}, ErrEmpty
	}
	data, err := io.ReadAll(io.LimitReader(r, MaxBytes+1))
	if err != nil {
		return Result{}, fmt.Errorf("document: leer cuerpo: %w", err)
	}
	switch {
	case len(data) == 0:
		return Result{}, ErrEmpty
	case len(data) > MaxBytes:
		return Result{}, ErrTooLarge
	case bytes.HasPrefix(data, pdfMagic):
		if err := ValidatePDF(ctx, data); err != nil {
			return Result{}, err
		}
		return Result{Data: data, Kind: KindPDF}, nil
	case bytes.HasPrefix(data, jpegMagic):
		return reencode(data, jpeg.DecodeConfig, jpeg.Decode, KindJPEG)
	case bytes.HasPrefix(data, pngMagic):
		return reencode(data, png.DecodeConfig, png.Decode, KindPNG)
	case len(data) >= 12 && bytes.HasPrefix(data, riffMagic) && string(data[8:12]) == "WEBP":
		// Sin codificador WebP en la stdlib: se re-codifica sin pérdida a PNG.
		return reencode(data, webp.DecodeConfig, webp.Decode, KindPNG)
	}
	return Result{}, ErrUnsupported
}

// reencode decodifica con cota de píxeles y re-codifica: la salida no
// conserva metadatos (EXIF/GPS eliminados por construcción).
func reencode(data []byte, cfgFn func(io.Reader) (image.Config, error), dec func(io.Reader) (image.Image, error), out Kind) (Result, error) {
	cfg, err := cfgFn(bytes.NewReader(data))
	if err != nil {
		return Result{}, ErrUnsupported
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || int64(cfg.Width)*int64(cfg.Height) > MaxPixels {
		return Result{}, ErrTooManyPixels
	}
	img, err := dec(bytes.NewReader(data))
	if err != nil {
		return Result{}, ErrUnsupported
	}
	var buf bytes.Buffer
	switch out {
	case KindJPEG:
		err = jpeg.Encode(&buf, img, &jpeg.Options{Quality: JPEGQuality})
	default:
		err = (&png.Encoder{CompressionLevel: png.BestCompression}).Encode(&buf, img)
	}
	if err != nil {
		return Result{}, fmt.Errorf("document: codificar: %w", err)
	}
	if buf.Len() > MaxBytes {
		return Result{}, ErrTooLarge
	}
	return Result{Data: buf.Bytes(), Kind: out}, nil
}

// Claves de diccionario que delatan contenido activo o embebido (SEC-003).
var unsafeKeys = []string{"JS", "JavaScript", "AcroForm", "EmbeddedFiles", "EmbeddedFile", "RichMedia", "XFA"}

// Acciones (/S) prohibidas.
var unsafeActions = map[types.Name]bool{
	"JavaScript": true, "Launch": true, "ImportData": true, "SubmitForm": true, "RichMediaExecute": true,
}

// unsafeTokens detecta nombres peligrosos en el contenido sin comprimir
// (ARQ-011: "/JS o /JavaScript en trailer/objects escaneados"). Los objetos
// dentro de object streams comprimidos los cubre el recorrido estructural.
var unsafeTokens = regexp.MustCompile(`/(JS|JavaScript|AcroForm|EmbeddedFiles?|Launch|RichMedia|XFA|SubmitForm|ImportData)\b`)

// ValidatePDF valida estructura con pdfcpu y rechaza cifrado, > 50 páginas
// y cualquier objeto con JavaScript, formularios, acciones de lanzamiento o
// ficheros embebidos. Los objetos de object streams ya están descomprimidos
// en la tabla xref tras la lectura.
func ValidatePDF(ctx context.Context, data []byte) error {
	if unsafeTokens.Match(data) {
		return ErrUnsafePDF
	}
	conf := model.NewStatelessConfiguration()
	conf.ValidationMode = model.ValidationRelaxed
	pctx, err := api.ReadAndValidate(ctx, bytes.NewReader(data), conf)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return err
		}
		return ErrInvalidPDF
	}
	if pctx.Encrypt != nil {
		return ErrUnsafePDF
	}
	if pctx.PageCount <= 0 {
		return ErrInvalidPDF
	}
	if pctx.PageCount > MaxPages {
		return ErrTooManyPages
	}
	if unsafeObject(pctx.RootDict, 0) {
		return ErrUnsafePDF
	}
	for _, entry := range pctx.Table {
		if entry == nil || entry.Free || entry.Object == nil {
			continue
		}
		if unsafeObject(entry.Object, 0) {
			return ErrUnsafePDF
		}
	}
	return nil
}

// maxDepth acota la recursión sobre objetos directos anidados (las
// referencias indirectas son entradas propias de la tabla xref).
const maxDepth = 32

// unsafeObject recorre diccionarios y arrays directos anidados.
func unsafeObject(o types.Object, depth int) bool {
	if depth > maxDepth {
		return true
	}
	switch v := o.(type) {
	case types.Dict:
		if unsafeDict(v) {
			return true
		}
		for _, child := range v {
			if unsafeObject(child, depth+1) {
				return true
			}
		}
	case types.StreamDict:
		return unsafeObject(v.Dict, depth+1)
	case types.Array:
		for _, child := range v {
			if unsafeObject(child, depth+1) {
				return true
			}
		}
	}
	return false
}

func unsafeDict(d types.Dict) bool {
	for _, k := range unsafeKeys {
		if _, ok := d[k]; ok {
			return true
		}
	}
	if s, ok := d["S"].(types.Name); ok && unsafeActions[s] {
		return true
	}
	return false
}
