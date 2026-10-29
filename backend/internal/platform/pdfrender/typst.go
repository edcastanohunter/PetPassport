// Package pdfrender genera PDF con Typst (PP-F2-INF-004, spike Typst vs
// chromedp en el PR de M2.1): binario `typst` invocado como proceso aislado
// en un directorio temporal, sin fuentes del sistema (solo las embebidas en
// Typst, licencia libre → salida determinista), timeout de 30 s y límite de
// concurrencia. La plantilla lee los datos con `json("data.json")`: los
// valores son cadenas, no marcado, así que el contenido clínico no puede
// inyectar código Typst.
package pdfrender

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// Límites por defecto (INF-004).
const (
	DefaultTimeout     = 30 * time.Second
	DefaultConcurrency = 2
	maxOutputBytes     = 20 << 20
)

// ErrTimeout: la compilación superó el timeout.
var ErrTimeout = errors.New("pdfrender: timeout")

// File es un recurso adicional junto a la plantilla (p. ej. el QR).
type File struct {
	Name string
	Data []byte
}

// Typst compila plantillas con el binario typst.
type Typst struct {
	bin     string
	timeout time.Duration
	slots   chan struct{}
}

// NewTypst construye el renderizador. bin vacío = "typst" del PATH.
func NewTypst(bin string, concurrency int, timeout time.Duration) *Typst {
	if bin == "" {
		bin = "typst"
	}
	if concurrency <= 0 {
		concurrency = DefaultConcurrency
	}
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	return &Typst{bin: bin, timeout: timeout, slots: make(chan struct{}, concurrency)}
}

// Available informa si el binario existe (el worker no arranca el runner de
// documentos sin él).
func (t *Typst) Available() error {
	_, err := exec.LookPath(t.bin)
	return err
}

// Render compila template con data.json y los ficheros dados y devuelve el PDF.
func (t *Typst) Render(ctx context.Context, template, data []byte, files ...File) ([]byte, error) {
	select {
	case t.slots <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	defer func() { <-t.slots }()

	dir, err := os.MkdirTemp("", "pp-typst-*")
	if err != nil {
		return nil, fmt.Errorf("pdfrender: tmp: %w", err)
	}
	defer func() { _ = os.RemoveAll(dir) }()
	write := func(name string, b []byte) error {
		return os.WriteFile(filepath.Join(dir, filepath.Base(name)), b, 0o600)
	}
	if err := write("main.typ", template); err != nil {
		return nil, err
	}
	if err := write("data.json", data); err != nil {
		return nil, err
	}
	for _, f := range files {
		if err := write(f.Name, f.Data); err != nil {
			return nil, err
		}
	}

	ctx, cancel := context.WithTimeout(ctx, t.timeout)
	defer cancel()
	out := filepath.Join(dir, "out.pdf")
	// #nosec G204 -- binario configurado por el operador; argumentos fijos.
	cmd := exec.CommandContext(ctx, t.bin, "compile", "--root", dir, "--ignore-system-fonts",
		filepath.Join(dir, "main.typ"), out)
	cmd.Dir = dir
	cmd.Env = []string{"TYPST_PACKAGE_CACHE_PATH=" + dir, "HOME=" + dir}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return nil, ErrTimeout
		}
		msg := stderr.String()
		if len(msg) > 500 {
			msg = msg[:500]
		}
		return nil, fmt.Errorf("pdfrender: typst: %w: %s", err, msg)
	}
	pdf, err := os.ReadFile(out) // #nosec G304 -- ruta construida en el tmp propio
	if err != nil {
		return nil, fmt.Errorf("pdfrender: leer salida: %w", err)
	}
	if len(pdf) > maxOutputBytes {
		return nil, errors.New("pdfrender: salida demasiado grande")
	}
	return pdf, nil
}
