package storage

import (
	"context"
)

// Client abstracts the subset of S3 operations the tool needs.
type Client interface {
	UploadFile(ctx context.Context, key, filePath string, contentType string) error
	DownloadToFile(ctx context.Context, key, destPath string) error
	ClearBucket(ctx context.Context) error
	GetDownloadLink(ctx context.Context, key string) string
}

var (
	defaultClient Client
)

// SetDefaultClient sets the global storage client used by the application.
func SetDefaultClient(c Client) {
	defaultClient = c
}

// DefaultClient returns the global storage client if one has been configured.
func DefaultClient() Client {
	return defaultClient
}
