package storage

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"path"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

type S3Options struct {
	Endpoint       string
	Secure         bool
	Region         string
	Bucket         string
	AccessKey      string
	SecretKey      string
	Prefix         string
	ForcePathStyle bool
}

type S3 struct {
	client *minio.Client
	bucket string
	prefix string
}

func NewS3(ctx context.Context, opts S3Options) (*S3, error) {
	var creds *credentials.Credentials
	if opts.AccessKey != "" {
		creds = credentials.NewStaticV4(opts.AccessKey, opts.SecretKey, "")
	} else {
		creds = credentials.NewChainCredentials([]credentials.Provider{
			&credentials.EnvAWS{},
			&credentials.EnvMinio{},
			&credentials.FileAWSCredentials{},
			&credentials.IAM{},
		})
	}

	lookup := minio.BucketLookupAuto
	if opts.ForcePathStyle {
		lookup = minio.BucketLookupPath
	}

	client, err := minio.New(opts.Endpoint, &minio.Options{
		Creds:        creds,
		Secure:       opts.Secure,
		Region:       opts.Region,
		BucketLookup: lookup,
	})
	if err != nil {
		return nil, err
	}

	exists, err := client.BucketExists(ctx, opts.Bucket)
	if err != nil {
		return nil, fmt.Errorf("check bucket %q: %w", opts.Bucket, err)
	}
	if !exists {
		return nil, fmt.Errorf("bucket %q does not exist", opts.Bucket)
	}

	return &S3{client: client, bucket: opts.Bucket, prefix: opts.Prefix}, nil
}

func (s *S3) objectName(key string) string {
	if s.prefix == "" {
		return key
	}
	return path.Join(s.prefix, key)
}

func (s *S3) Put(ctx context.Context, key string, r io.Reader, size int64, contentType string) error {
	_, err := s.client.PutObject(ctx, s.bucket, s.objectName(key), r, size, minio.PutObjectOptions{
		ContentType: contentType,
	})
	return err
}

func (s *S3) Open(ctx context.Context, key string) (io.ReadCloser, error) {
	obj, err := s.client.GetObject(ctx, s.bucket, s.objectName(key), minio.GetObjectOptions{})
	if err != nil {
		return nil, err
	}
	if _, err := obj.Stat(); err != nil {
		obj.Close()
		if minio.ToErrorResponse(err).Code == minio.NoSuchKey {
			return nil, fmt.Errorf("%s: %w", key, fs.ErrNotExist)
		}
		return nil, err
	}
	return obj, nil
}

func (s *S3) Delete(ctx context.Context, key string) error {
	return s.client.RemoveObject(ctx, s.bucket, s.objectName(key), minio.RemoveObjectOptions{})
}
