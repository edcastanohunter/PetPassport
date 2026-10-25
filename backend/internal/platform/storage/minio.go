package storage

import (
	"context"
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
	client *minio.Client
	bucket string
}

// MinIOOptions configura el adaptador.
type MinIOOptions struct {
	Endpoint  string
	Bucket    string
	AccessKey string
	SecretKey string
	UseSSL    bool
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
	m := &MinIO{client: client, bucket: o.Bucket}
	ok, err := client.BucketExists(ctx, o.Bucket)
	if err != nil {
		return nil, fmt.Errorf("storage: bucket: %w", err)
	}
	if !ok {
		if err := client.MakeBucket(ctx, o.Bucket, minio.MakeBucketOptions{}); err != nil {
			return nil, fmt.Errorf("storage: crear bucket: %w", err)
		}
	}
	return m, nil
}

// Put implementa ObjectStorage.
func (m *MinIO) Put(ctx context.Context, key, contentType string, r io.Reader, size int64) error {
	_, err := m.client.PutObject(ctx, m.bucket, key, r, size, minio.PutObjectOptions{
		ContentType:        contentType,
		ContentDisposition: "inline",
		CacheControl:       "private, max-age=86400",
	})
	if err != nil {
		return fmt.Errorf("storage: put %s: %w", key, err)
	}
	return nil
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
