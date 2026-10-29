package document

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"strings"
	"testing"
)

// buildPDF arma un PDF mínimo válido con la tabla xref correcta. extraCatalog
// se añade al diccionario del catálogo; pages es el número de páginas.
func buildPDF(t *testing.T, pages int, extraCatalog string) []byte {
	t.Helper()
	var objs []string
	kids := make([]string, pages)
	for i := range pages {
		kids[i] = fmt.Sprintf("%d 0 R", 3+i)
	}
	objs = append(objs, "<< /Type /Catalog /Pages 2 0 R "+extraCatalog+" >>")
	objs = append(objs, fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>", strings.Join(kids, " "), pages))
	for range pages {
		objs = append(objs, "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>")
	}
	var b bytes.Buffer
	b.WriteString("%PDF-1.4\n")
	offsets := make([]int, len(objs))
	for i, o := range objs {
		offsets[i] = b.Len()
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	xref := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(objs)+1)
	for _, off := range offsets {
		fmt.Fprintf(&b, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, xref)
	return b.Bytes()
}

func TestProcess_ValidPDF(t *testing.T) {
	res, err := Process(context.Background(), bytes.NewReader(buildPDF(t, 2, "")))
	if err != nil {
		t.Fatalf("PDF válido rechazado: %v", err)
	}
	if res.Kind != KindPDF || res.Kind.MIME() != "application/pdf" {
		t.Fatalf("kind: %+v", res.Kind)
	}
}

func TestProcess_RejectsUnsafePDF(t *testing.T) {
	cases := map[string]string{
		"javascript":  "/OpenAction << /S /JavaScript /JS (app.alert(1)) >>",
		"launch":      "/OpenAction << /S /Launch /F (calc.exe) >>",
		"acroform":    "/AcroForm << /Fields [] >>",
		"embedded":    "/Names << /EmbeddedFiles << /Names [] >> >>",
		"names-js":    "/Names << /JavaScript << /Names [] >> >>",
		"submit-form": "/OpenAction << /S /SubmitForm /F (http://x) >>",
	}
	for name, extra := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := Process(context.Background(), bytes.NewReader(buildPDF(t, 1, extra)))
			if !errors.Is(err, ErrUnsafePDF) && !errors.Is(err, ErrInvalidPDF) {
				t.Fatalf("esperaba rechazo, obtuvo %v", err)
			}
		})
	}
}

func TestProcess_TooManyPages(t *testing.T) {
	_, err := Process(context.Background(), bytes.NewReader(buildPDF(t, MaxPages+1, "")))
	if !errors.Is(err, ErrTooManyPages) {
		t.Fatalf("esperaba ErrTooManyPages, obtuvo %v", err)
	}
}

func TestProcess_CorruptPDF(t *testing.T) {
	_, err := Process(context.Background(), strings.NewReader("%PDF-1.4\nbasura sin estructura"))
	if !errors.Is(err, ErrInvalidPDF) {
		t.Fatalf("esperaba ErrInvalidPDF, obtuvo %v", err)
	}
}

func TestProcess_SizeAndType(t *testing.T) {
	if _, err := Process(context.Background(), bytes.NewReader(nil)); !errors.Is(err, ErrEmpty) {
		t.Fatalf("vacío: %v", err)
	}
	big := append([]byte("%PDF-1.4\n"), bytes.Repeat([]byte{'a'}, MaxBytes)...)
	if _, err := Process(context.Background(), bytes.NewReader(big)); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("grande: %v", err)
	}
	if _, err := Process(context.Background(), strings.NewReader("<svg xmlns='http://www.w3.org/2000/svg'/>")); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("svg: %v", err)
	}
	if _, err := Process(context.Background(), strings.NewReader("GIF89a......")); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("gif: %v", err)
	}
}

func testImage(w, h int) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.Set(x, y, color.RGBA{uint8(x), uint8(y), 128, 255})
		}
	}
	return img
}

// Las imágenes clínicas se re-codifican sin reducir resolución
// (radiografías) y sin metadatos.
func TestProcess_ImagesKeepResolution(t *testing.T) {
	var jb, pb bytes.Buffer
	if err := jpeg.Encode(&jb, testImage(1600, 1200), nil); err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(&pb, testImage(300, 200)); err != nil {
		t.Fatal(err)
	}
	for name, tc := range map[string]struct {
		data []byte
		kind Kind
		w, h int
	}{
		"jpeg": {jb.Bytes(), KindJPEG, 1600, 1200},
		"png":  {pb.Bytes(), KindPNG, 300, 200},
	} {
		t.Run(name, func(t *testing.T) {
			res, err := Process(context.Background(), bytes.NewReader(tc.data))
			if err != nil {
				t.Fatal(err)
			}
			if res.Kind != tc.kind {
				t.Fatalf("kind %s", res.Kind)
			}
			cfg, _, err := image.DecodeConfig(bytes.NewReader(res.Data))
			if err != nil {
				t.Fatal(err)
			}
			if cfg.Width != tc.w || cfg.Height != tc.h {
				t.Fatalf("resolución %dx%d, esperaba %dx%d", cfg.Width, cfg.Height, tc.w, tc.h)
			}
		})
	}
}

// La re-codificación elimina EXIF (incluido GPS): el segmento APP1 no
// sobrevive.
func TestProcess_StripsEXIF(t *testing.T) {
	var jb bytes.Buffer
	if err := jpeg.Encode(&jb, testImage(64, 64), nil); err != nil {
		t.Fatal(err)
	}
	raw := jb.Bytes()
	const exif = "Exif\x00\x00GPSLatitude"
	const segLen = byte(len(exif) + 2)
	app1 := append([]byte{0xFF, 0xE1, 0x00, segLen}, exif...)
	withExif := append(append(append([]byte{}, raw[:2]...), app1...), raw[2:]...)
	res, err := Process(context.Background(), bytes.NewReader(withExif))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(res.Data, []byte("GPSLatitude")) || bytes.Contains(res.Data, []byte("Exif")) {
		t.Fatal("la salida conserva EXIF")
	}
}
