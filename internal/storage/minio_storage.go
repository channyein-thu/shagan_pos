package storage

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// MinIOStorage implements Storage against any S3-compatible endpoint
// (MinIO locally, real S3/R2/Spaces in production).
type MinIOStorage struct {
	client *minio.Client
	bucket string
}

var _ Storage = (*MinIOStorage)(nil)

// NewMinIOStorage connects to the object store at endpoint and ensures bucket
// exists, retrying briefly since the object store container may still be
// starting up when the app does (e.g. right after `docker compose up`).
func NewMinIOStorage(endpoint, accessKey, secretKey, bucket string, useSSL bool) (*MinIOStorage, error) {
	client, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(accessKey, secretKey, ""),
		Secure: useSSL,
	})
	if err != nil {
		return nil, fmt.Errorf("create storage client: %w", err)
	}

	s := &MinIOStorage{client: client, bucket: bucket}

	const (
		maxAttempts = 5
		backoff     = 2 * time.Second
	)
	for attempt := 1; ; attempt++ {
		err = s.ensureBucket(context.Background())
		if err == nil {
			return s, nil
		}
		if attempt >= maxAttempts {
			return nil, fmt.Errorf("ensure bucket %q after %d attempts: %w", bucket, maxAttempts, err)
		}
		time.Sleep(backoff)
	}
}

func (s *MinIOStorage) ensureBucket(ctx context.Context) error {
	exists, err := s.client.BucketExists(ctx, s.bucket)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}
	return s.client.MakeBucket(ctx, s.bucket, minio.MakeBucketOptions{})
}

func (s *MinIOStorage) Upload(ctx context.Context, key string, reader io.Reader, size int64, contentType string) error {
	_, err := s.client.PutObject(ctx, s.bucket, key, reader, size, minio.PutObjectOptions{
		ContentType: contentType,
	})
	return err
}

func (s *MinIOStorage) PresignedURL(ctx context.Context, key string, expiry time.Duration) (string, error) {
	u, err := s.client.PresignedGetObject(ctx, s.bucket, key, expiry, nil)
	if err != nil {
		return "", err
	}
	return u.String(), nil
}

func (s *MinIOStorage) Delete(ctx context.Context, key string) error {
	return s.client.RemoveObject(ctx, s.bucket, key, minio.RemoveObjectOptions{})
}
