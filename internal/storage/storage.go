package storage

import (
	"context"
	"io"
	"time"
)

// Storage abstracts an S3-compatible object store. MinIO backs it in local
// dev (see docker-compose.yml); the same code talks to real S3/R2/Spaces in
// production by pointing STORAGE_ENDPOINT at the real service.
type Storage interface {
	// Upload writes reader's content to key, replacing any existing object there.
	Upload(ctx context.Context, key string, reader io.Reader, size int64, contentType string) error

	// PresignedURL returns a temporary, signed URL for reading key directly
	// from the object store, valid for expiry.
	PresignedURL(ctx context.Context, key string, expiry time.Duration) (string, error)

	// Delete removes key. Deleting a key that doesn't exist is not an error.
	Delete(ctx context.Context, key string) error
}
