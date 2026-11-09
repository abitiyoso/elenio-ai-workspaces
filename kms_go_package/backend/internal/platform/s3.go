package platform

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/url"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/elenio/knowledge-workspace/backend/internal/config"
)

type S3ObjectStore struct {
	client *s3.Client
	bucket string
}

func NewS3ObjectStore(ctx context.Context, cfg config.Config) (*S3ObjectStore, error) {
	if cfg.ObjectStorageEndpoint == "" {
		return nil, nil
	}
	u, err := url.Parse(cfg.ObjectStorageEndpoint)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, fmt.Errorf("invalid object storage endpoint")
	}
	awsCfg, err := awsconfig.LoadDefaultConfig(ctx,
		awsconfig.WithRegion(cfg.ObjectStorageRegion),
		awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(cfg.ObjectStorageAccessKey, cfg.ObjectStorageSecretKey, "")),
	)
	if err != nil {
		return nil, fmt.Errorf("load S3 configuration: %w", err)
	}
	client := s3.NewFromConfig(awsCfg, func(options *s3.Options) {
		options.BaseEndpoint = aws.String(cfg.ObjectStorageEndpoint)
		options.UsePathStyle = true
	})
	return &S3ObjectStore{client: client, bucket: cfg.ObjectStorageBucket}, nil
}

func (s *S3ObjectStore) Ping(ctx context.Context) error {
	if s == nil || s.client == nil {
		return fmt.Errorf("object storage is not configured")
	}
	_, err := s.client.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: aws.String(s.bucket)})
	if err != nil {
		return fmt.Errorf("check S3 bucket: %w", err)
	}
	return nil
}

func (s *S3ObjectStore) Put(ctx context.Context, key, mediaType string, body []byte) error {
	if s == nil || s.client == nil {
		return fmt.Errorf("object storage is not configured")
	}
	if strings.TrimSpace(key) == "" || len(body) == 0 {
		return fmt.Errorf("object key and body are required")
	}
	_, err := s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(s.bucket), Key: aws.String(key), ContentType: aws.String(mediaType), Body: bytes.NewReader(body),
	})
	if err != nil {
		return fmt.Errorf("put S3 object: %w", err)
	}
	return nil
}

func (s *S3ObjectStore) Get(ctx context.Context, key string) ([]byte, error) {
	if s == nil || s.client == nil {
		return nil, fmt.Errorf("object storage is not configured")
	}
	out, err := s.client.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(key)})
	if err != nil {
		return nil, fmt.Errorf("get S3 object: %w", err)
	}
	defer out.Body.Close()
	body, err := io.ReadAll(out.Body)
	if err != nil {
		return nil, fmt.Errorf("read S3 object: %w", err)
	}
	return body, nil
}

func (s *S3ObjectStore) Delete(ctx context.Context, key string) error {
	if s == nil || s.client == nil {
		return fmt.Errorf("object storage is not configured")
	}
	_, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(key)})
	if err != nil {
		return fmt.Errorf("delete S3 object: %w", err)
	}
	return nil
}
