package storage

import "context"

// Client abstracts the subset of S3 operations the tool needs.
type Client interface {
	UploadFile(ctx context.Context, bucket, key, filePath string, contentType string) error
	DownloadToFile(ctx context.Context, bucket, key, destPath string) error
}
