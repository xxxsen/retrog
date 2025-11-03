package storage

import "context"

// Client abstracts the subset of S3 operations the tool needs.
type Client interface {
	UploadFile(ctx context.Context, bucket, key, filePath string, contentType string) error
	DownloadToFile(ctx context.Context, bucket, key, destPath string) error
	ClearBucket(ctx context.Context, bucket string) error
}

var defaultClient Client

// SetDefaultClient sets the global storage client used by the application.
func SetDefaultClient(c Client) {
	defaultClient = c
}

// DefaultClient returns the global storage client if one has been configured.
func DefaultClient() Client {
	return defaultClient
}
