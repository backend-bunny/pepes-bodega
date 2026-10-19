// Package storage stores blobs in S3-compatible object storage (SeaweedFS).
package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"github.com/backend-bunny/pepes-bodega/internal/config"
)

type Store struct {
	client *minio.Client
	bucket string
}

func New(ctx context.Context, cfg config.S3) (*Store, error) {
	client, err := minio.New(cfg.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Secure: cfg.UseSSL,
		Region: cfg.Region,
	})
	if err != nil {
		return nil, err
	}
	s := &Store{client: client, bucket: cfg.Bucket}
	exists, err := bucketExists(ctx, client, cfg.Bucket)
	if err != nil {
		return nil, fmt.Errorf("check bucket %q: %w", cfg.Bucket, err)
	}
	if !exists {
		if err := client.MakeBucket(ctx, cfg.Bucket, minio.MakeBucketOptions{Region: cfg.Region}); err != nil {
			return nil, fmt.Errorf("create bucket %q: %w", cfg.Bucket, err)
		}
	}
	return s, nil
}

// bucketExists retries for a while: SeaweedFS opens its S3 port before the filer
// behind it is ready and answers 500 in the meantime.
func bucketExists(ctx context.Context, client *minio.Client, bucket string) (bool, error) {
	deadline := time.Now().Add(30 * time.Second)
	for {
		exists, err := client.BucketExists(ctx, bucket)
		if err == nil || time.Now().After(deadline) {
			return exists, err
		}
		select {
		case <-ctx.Done():
			return false, ctx.Err()
		case <-time.After(time.Second):
		}
	}
}

// BlobKey is the object key for a content-addressed blob.
func BlobKey(sha256 string) string { return "blobs/" + sha256 }

// ThumbKey is the object key for a blob's generated thumbnail (written by workers).
func ThumbKey(sha256 string) string { return "thumbs/" + sha256 + ".webp" }

func (s *Store) Put(ctx context.Context, key string, r io.Reader, size int64, contentType string) error {
	_, err := s.client.PutObject(ctx, s.bucket, key, r, size, minio.PutObjectOptions{ContentType: contentType})
	return err
}

func (s *Store) Exists(ctx context.Context, key string) (bool, error) {
	_, err := s.client.StatObject(ctx, s.bucket, key, minio.StatObjectOptions{})
	if err == nil {
		return true, nil
	}
	if minio.ToErrorResponse(err).StatusCode == http.StatusNotFound {
		return false, nil
	}
	return false, err
}

func (s *Store) Delete(ctx context.Context, key string) error {
	return s.client.RemoveObject(ctx, s.bucket, key, minio.RemoveObjectOptions{})
}

// ErrNotFound is returned by Open when the object does not exist.
var ErrNotFound = errors.New("object not found")

// Object is an open object with its metadata.
type Object struct {
	io.ReadSeekCloser
	Size        int64
	ContentType string
	ETag        string
}

func (s *Store) Open(ctx context.Context, key string) (*Object, error) {
	obj, err := s.client.GetObject(ctx, s.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, err
	}
	info, err := obj.Stat()
	if err != nil {
		_ = obj.Close()
		if minio.ToErrorResponse(err).StatusCode == http.StatusNotFound {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &Object{ReadSeekCloser: obj, Size: info.Size, ContentType: info.ContentType, ETag: info.ETag}, nil
}
