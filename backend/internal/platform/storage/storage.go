// Package storage es el puerto de object storage (ARQ-012): las fotos se
// sirven por URL prefirmada de 15 min emitida por la API (autorizada),
// nunca bucket público. El adaptador S3/MinIO está en minio.go; el pipeline
// de imagen en image/.
package storage

import (
	"context"
	"io"
	"time"
)

// ObjectStorage es el puerto de persistencia de objetos.
type ObjectStorage interface {
	// Put escribe el objeto (sobrescribe). contentType ya validado por la
	// capa superior.
	Put(ctx context.Context, key, contentType string, r io.Reader, size int64) error
	// Delete elimina el objeto (idempotente).
	Delete(ctx context.Context, key string) error
	// PresignGet emite una URL de lectura prefirmada (ARQ-012).
	PresignGet(ctx context.Context, key string, ttl time.Duration) (string, error)
}

// Keys de foto con prefijo pets/<petID>/ (DOM-004 PhotoRef).
func PhotoKey(petID, objectID, suffix string) string {
	return "pets/" + petID + "/" + objectID + suffix
}
