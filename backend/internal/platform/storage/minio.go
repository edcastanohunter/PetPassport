package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// MinIO/S3 (ARQ-012). Dependencia justificada: cliente S3 compatible con
// MinIO de desarrollo y con el proveedor gestionado de staging/prod.
type MinIO struct {
	client    *minio.Client
	bucket    string
	retainFor time.Duration
	now       func() time.Time
}

// MinIOOptions configura el adaptador.
type MinIOOptions struct {
	Endpoint  string
	Bucket    string
	AccessKey string
	SecretKey string
	UseSSL    bool
	// RetainFor > 0 exige un bucket con Object Lock y escribe cada objeto con
	// retención COMPLIANCE hasta ahora + RetainFor: ni la propia aplicación ni
	// un administrador pueden borrarlo o sobrescribirlo antes (documentos
	// fiscales, PP-F5-ARQ-003, CMP-009: 10 años).
	RetainFor time.Duration
}

// NewMinIO construye el adaptador y comprueba el bucket (lo crea si falta;
// en dev es esperado, en prod existirá).
func NewMinIO(ctx context.Context, o MinIOOptions) (*MinIO, error) {
	client, err := minio.New(o.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(o.AccessKey, o.SecretKey, ""),
		Secure: o.UseSSL,
	})
	if err != nil {
		return nil, fmt.Errorf("storage: cliente minio: %w", err)
	}
	m := &MinIO{client: client, bucket: o.Bucket, retainFor: o.RetainFor, now: time.Now}
	ok, err := client.BucketExists(ctx, o.Bucket)
	if err != nil {
		return nil, fmt.Errorf("storage: bucket: %w", err)
	}
	if !ok {
		// Object Lock solo se puede activar al crear el bucket.
		if err := client.MakeBucket(ctx, o.Bucket, minio.MakeBucketOptions{ObjectLocking: o.RetainFor > 0}); err != nil {
			return nil, fmt.Errorf("storage: crear bucket: %w", err)
		}
		return m, nil
	}
	if o.RetainFor > 0 {
		lock, _, _, _, err := client.GetObjectLockConfig(ctx, o.Bucket)
		if err != nil || lock != "Enabled" {
			// Sin Object Lock la retención por objeto falla al escribir: mejor no
			// arrancar que descubrirlo al emitir la primera factura.
			return nil, fmt.Errorf("storage: el bucket %s no tiene Object Lock (%s): %w", o.Bucket, lock, ErrNoObjectLock)
		}
	}
	return m, nil
}

// Put implementa ObjectStorage.
func (m *MinIO) Put(ctx context.Context, key, contentType string, r io.Reader, size int64) error {
	_, err := m.client.PutObject(ctx, m.bucket, key, r, size, m.putOptions(contentType))
	if err != nil {
		return fmt.Errorf("storage: put %s: %w", key, err)
	}
	return nil
}

// ErrNoObjectLock: el bucket de un almacén con retención no tiene Object Lock.
var ErrNoObjectLock = errors.New("storage: bucket sin Object Lock")

func (m *MinIO) putOptions(contentType string) minio.PutObjectOptions {
	opts := minio.PutObjectOptions{
		ContentType:        contentType,
		ContentDisposition: "inline",
		CacheControl:       "private, max-age=86400",
	}
	if m.retainFor > 0 {
		opts.Mode = minio.Compliance
		opts.RetainUntilDate = m.now().UTC().Add(m.retainFor)
		// S3 exige Content-MD5 (o un checksum) en escrituras con retención.
		opts.SendContentMd5 = true
	}
	return opts
}

// Delete implementa ObjectStorage (idempotente).
func (m *MinIO) Delete(ctx context.Context, key string) error {
	if err := m.client.RemoveObject(ctx, m.bucket, key, minio.RemoveObjectOptions{}); err != nil {
		return fmt.Errorf("storage: delete %s: %w", key, err)
	}
	return nil
}

// PresignGet implementa ObjectStorage.
func (m *MinIO) PresignGet(ctx context.Context, key string, ttl time.Duration) (string, error) {
	u, err := m.client.PresignedGetObject(ctx, m.bucket, key, ttl, url.Values{})
	if err != nil {
		return "", fmt.Errorf("storage: presign %s: %w", key, err)
	}
	return u.String(), nil
}

// PresignDownload emite una URL prefirmada que fuerza la descarga con el
// Content-Type fijado por el servidor (SEC-003: adjuntos PDF).
func (m *MinIO) PresignDownload(ctx context.Context, key string, ttl time.Duration, filename, contentType string) (string, error) {
	q := url.Values{}
	q.Set("response-content-disposition", `attachment; filename="`+filename+`"`)
	q.Set("response-content-type", contentType)
	u, err := m.client.PresignedGetObject(ctx, m.bucket, key, ttl, q)
	if err != nil {
		return "", fmt.Errorf("storage: presign download: %w", err)
	}
	return u.String(), nil
}

// GetObject implementa ObjectReader.
func (m *MinIO) GetObject(ctx context.Context, key string) (io.ReadCloser, error) {
	obj, err := m.client.GetObject(ctx, m.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("storage: get %s: %w", key, err)
	}
	return obj, nil
}
