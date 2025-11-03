package storage

import (
	"context"
	"fmt"
	"io"
	"mime"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"

	appconfig "retrog/internal/config"
)

type s3Client struct {
	client *s3.Client
	bucket string
}

// NewS3Client builds a storage client backed by AWS S3 (or compatible) based on config.
func NewS3Client(ctx context.Context, cfg appconfig.S3Config) (Client, error) {
	if cfg.Region == "" {
		cfg.Region = "us-east-1"
	}

	loadOpts := []func(*config.LoadOptions) error{
		config.WithRegion(cfg.Region),
	}

	if cfg.AccessKeyID != "" && cfg.SecretAccessKey != "" {
		loadOpts = append(loadOpts, config.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(cfg.AccessKeyID, cfg.SecretAccessKey, cfg.SessionToken),
		))
	}

	awsCfg, err := config.LoadDefaultConfig(ctx, loadOpts...)
	if err != nil {
		return nil, fmt.Errorf("load aws config: %w", err)
	}

	endpoint := normalizeEndpoint(cfg.Host)
	client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		if endpoint != "" {
			o.BaseEndpoint = aws.String(endpoint)
		}
		o.UsePathStyle = cfg.ForcePathStyle
	})

	return &s3Client{client: client, bucket: cfg.Bucket}, nil
}

func (c *s3Client) UploadFile(ctx context.Context, key, filePath string, contentType string) error {
	file, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("open file for upload %s: %w", filePath, err)
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return fmt.Errorf("stat file for upload %s: %w", filePath, err)
	}

	if contentType == "" {
		contentType = mime.TypeByExtension(strings.ToLower(filepath.Ext(filePath)))
	}

	_, err = c.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:        aws.String(c.bucket),
		Key:           aws.String(key),
		Body:          file,
		ContentLength: aws.Int64(info.Size()),
		ContentType:   aws.String(contentType),
	})
	if err != nil {
		return fmt.Errorf("put object %s/%s: %w", c.bucket, key, err)
	}

	return nil
}

func (c *s3Client) DownloadToFile(ctx context.Context, key, destPath string) error {
	res, err := c.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return fmt.Errorf("get object %s/%s: %w", c.bucket, key, err)
	}
	defer res.Body.Close()

	if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
		return fmt.Errorf("ensure dest dir %s: %w", destPath, err)
	}

	out, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("create dest %s: %w", destPath, err)
	}
	defer out.Close()

	if _, err := io.Copy(out, res.Body); err != nil {
		return fmt.Errorf("write dest %s: %w", destPath, err)
	}

	return nil
}

func (c *s3Client) ClearBucket(ctx context.Context) error {
	var continuation *string

	for {
		resp, err := c.client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
			Bucket:            aws.String(c.bucket),
			ContinuationToken: continuation,
		})
		if err != nil {
			return fmt.Errorf("list objects in %s: %w", c.bucket, err)
		}

		if len(resp.Contents) > 0 {
			objs := make([]types.ObjectIdentifier, 0, len(resp.Contents))
			for _, obj := range resp.Contents {
				if obj.Key == nil {
					continue
				}
				objs = append(objs, types.ObjectIdentifier{Key: obj.Key})
			}
			if len(objs) > 0 {
				_, err = c.client.DeleteObjects(ctx, &s3.DeleteObjectsInput{
					Bucket: aws.String(c.bucket),
					Delete: &types.Delete{
						Objects: objs,
						Quiet:   aws.Bool(true),
					},
				})
				if err != nil {
					return fmt.Errorf("delete objects from %s: %w", c.bucket, err)
				}
			}
		}

		if resp.IsTruncated == nil || !*resp.IsTruncated {
			break
		}
		continuation = resp.NextContinuationToken
	}

	return nil
}

func normalizeEndpoint(host string) string {
	host = strings.TrimSpace(host)
	if host == "" {
		return ""
	}

	if strings.HasPrefix(host, "http://") || strings.HasPrefix(host, "https://") {
		return host
	}

	if strings.Contains(host, "://") {
		return host
	}

	u := url.URL{
		Scheme: "https",
		Host:   host,
	}
	return u.String()
}
