package antivirus_test

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/edcastanohunter/petpassport/backend/internal/platform/antivirus"
)

type memoryStorage struct {
	files map[string][]byte
}

func newMemoryStorage() *memoryStorage {
	return &memoryStorage{files: make(map[string][]byte)}
}

func (m *memoryStorage) Put(ctx context.Context, key, contentType string, r io.Reader, size int64) error {
	data, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	m.files[key] = data
	return nil
}

func (m *memoryStorage) Delete(ctx context.Context, key string) error {
	delete(m.files, key)
	return nil
}

func (m *memoryStorage) PresignGet(ctx context.Context, key string, ttl time.Duration) (string, error) {
	return "https://storage.local/" + key, nil
}

func (m *memoryStorage) GetObject(ctx context.Context, key string) (io.ReadCloser, error) {
	data, ok := m.files[key]
	if !ok {
		return nil, io.EOF
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

func TestAntivirus_SEC005_InProcessScanner_CleanFile(t *testing.T) {
	ctx := context.Background()
	scanner := antivirus.NewInProcessScanner()

	cleanContent := strings.NewReader("PDF documento veterinario clínico normal sin virus")
	res, err := scanner.Scan(ctx, cleanContent)
	if err != nil {
		t.Fatalf("scan error: %v", err)
	}
	if res.Status != antivirus.StatusClean {
		t.Fatalf("expected StatusClean, got %s", res.Status)
	}
}

func TestAntivirus_SEC005_InProcessScanner_EICARMalware(t *testing.T) {
	ctx := context.Background()
	scanner := antivirus.NewInProcessScanner()

	eicarContent := strings.NewReader(antivirus.EICARStandardSignature)
	res, err := scanner.Scan(ctx, eicarContent)
	if err != nil {
		t.Fatalf("scan error: %v", err)
	}
	if res.Status != antivirus.StatusInfected {
		t.Fatalf("expected StatusInfected, got %s", res.Status)
	}
	if res.VirusName != "Eicar-Test-Signature" {
		t.Fatalf("expected virus name Eicar-Test-Signature, got %s", res.VirusName)
	}
}

func TestAntivirus_ARQ014_Service_Quarantine(t *testing.T) {
	ctx := context.Background()
	st := newMemoryStorage()
	scanner := antivirus.NewInProcessScanner()
	svc := antivirus.NewService(nil, st, scanner, slog.New(slog.NewTextHandler(io.Discard, nil)))

	encID := uuid.New()
	attID := uuid.New()
	key := "clinical/encounters/" + attID.String() + ".pdf"

	// Guardar archivo infectado en storage
	_ = st.Put(ctx, key, "application/pdf", strings.NewReader(antivirus.EICARStandardSignature), -1)

	// Ejecutar escaneo
	res, err := svc.ScanAttachment(ctx, encID, attID, key)
	if err != nil {
		t.Fatalf("ScanAttachment error: %v", err)
	}
	if res.Status != antivirus.StatusInfected {
		t.Fatalf("expected infected, got %s", res.Status)
	}

	// El archivo original debe haberse eliminado de la ruta normal
	if _, exists := st.files[key]; exists {
		t.Fatal("expected original infected file deleted from active key")
	}

	// Debe estar en quarantine/
	quarantineKey := "quarantine/" + key
	if _, exists := st.files[quarantineKey]; !exists {
		t.Fatalf("expected file moved to %s", quarantineKey)
	}
}
