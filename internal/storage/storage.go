package storage

import (
	"context"
	"fmt"

	appconfig "retrog/internal/config"
)

// Client abstracts the subset of S3 operations the tool needs.
type Client interface {
	UploadFile(ctx context.Context, bucket, key, filePath string, contentType string) error
	DownloadToFile(ctx context.Context, bucket, key, destPath string) error
	ClearBucket(ctx context.Context, bucket string) error
}

var (
	defaultClient   Client
	defaultS3Config appconfig.S3Config
	hasS3Config     bool
)

// SetDefaultClient sets the global storage client used by the application.
func SetDefaultClient(c Client) {
	defaultClient = c
}

// DefaultClient returns the global storage client if one has been configured.
func DefaultClient() Client {
	return defaultClient
}

// SetDefaultS3Config stores the global S3 configuration used when creating clients.
func SetDefaultS3Config(cfg appconfig.S3Config) {
	defaultS3Config = cfg
	hasS3Config = true
}

// DefaultS3Config returns the stored S3 configuration if present.
func DefaultS3Config() (appconfig.S3Config, bool) {
	return defaultS3Config, hasS3Config
}

// EnsureDefaultClient guarantees a default client exists, initialising it if necessary.
func EnsureDefaultClient(ctx context.Context) (Client, error) {
	if defaultClient != nil {
		return defaultClient, nil
	}
	if !hasS3Config {
		return nil, fmt.Errorf("default s3 configuration not initialised")
	}
	client, err := NewS3Client(ctx, defaultS3Config)
	if err != nil {
		return nil, err
	}
	defaultClient = client
	return defaultClient, nil
}
