package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"sort"
	"strings"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/muecahit94/dockvault/models"
)

// S3Driver stores backup archives in an S3-compatible object storage bucket.
type S3Driver struct {
	client *minio.Client
	bucket string
	prefix string
}

// NewS3Driver initializes an S3Driver from a StorageProvider configuration.
func NewS3Driver(provider models.StorageProvider) (*S3Driver, error) {
	endpoint, useSSL := normalizeEndpoint(provider.Endpoint, provider.UseSSL)
	if endpoint == "" {
		return nil, fmt.Errorf("S3 endpoint is required")
	}
	bucket := strings.TrimSpace(provider.Bucket)
	if bucket == "" {
		return nil, fmt.Errorf("S3 bucket is required")
	}
	accessKey := strings.TrimSpace(provider.AccessKey)
	secretKey := strings.TrimSpace(provider.SecretKey)
	if accessKey == "" || secretKey == "" {
		return nil, fmt.Errorf("S3 access key and secret key are required")
	}

	opts := &minio.Options{
		Creds:  credentials.NewStaticV4(accessKey, secretKey, ""),
		Secure: useSSL,
		Region: strings.TrimSpace(provider.Region),
	}

	client, err := minio.New(endpoint, opts)
	if err != nil {
		return nil, fmt.Errorf("initialize S3 client: %w", err)
	}

	prefix := strings.Trim(strings.TrimSpace(provider.Prefix), "/")
	return &S3Driver{
		client: client,
		bucket: bucket,
		prefix: prefix,
	}, nil
}

func (d *S3Driver) Test(ctx context.Context) error {
	exists, err := d.client.BucketExists(ctx, d.bucket)
	if err != nil {
		return fmt.Errorf("check S3 bucket %q: %w", d.bucket, err)
	}
	if !exists {
		return fmt.Errorf("S3 bucket %q does not exist or is inaccessible", d.bucket)
	}
	return nil
}

func (d *S3Driver) Save(ctx context.Context, localTempPath, fileName string) (string, error) {
	safeName, err := SafeFileName(fileName)
	if err != nil {
		return "", err
	}

	objectKey := safeName
	if d.prefix != "" {
		objectKey = path.Join(d.prefix, safeName)
	}

	err = withRetry(ctx, "S3 upload "+objectKey, isS3Retryable, func() error {
		_, putErr := d.client.FPutObject(ctx, d.bucket, objectKey, localTempPath, minio.PutObjectOptions{
			ContentType: "application/gzip",
		})
		return putErr
	})
	if err != nil {
		return "", fmt.Errorf("upload backup to s3://%s/%s: %w", d.bucket, objectKey, err)
	}

	// Clean up the temporary local archive after successful upload.
	_ = os.Remove(localTempPath)
	return objectKey, nil
}

func (d *S3Driver) Open(ctx context.Context, storagePath string) (io.ReadCloser, int64, error) {
	objectKey := strings.TrimPrefix(strings.TrimSpace(storagePath), "/")
	if objectKey == "" {
		return nil, 0, fmt.Errorf("empty S3 object key")
	}

	var (
		obj  *minio.Object
		size int64
	)
	err := withRetry(ctx, "S3 open "+objectKey, isS3Retryable, func() error {
		o, getErr := d.client.GetObject(ctx, d.bucket, objectKey, minio.GetObjectOptions{})
		if getErr != nil {
			return fmt.Errorf("fetch S3 object %q: %w", objectKey, getErr)
		}
		stat, statErr := o.Stat()
		if statErr != nil {
			_ = o.Close()
			return fmt.Errorf("stat S3 object %q: %w", objectKey, statErr)
		}
		obj, size = o, stat.Size
		return nil
	})
	if err != nil {
		return nil, 0, err
	}
	return obj, size, nil
}

func (d *S3Driver) Delete(ctx context.Context, storagePath string) error {
	objectKey := strings.TrimPrefix(strings.TrimSpace(storagePath), "/")
	if objectKey == "" {
		return nil
	}
	err := withRetry(ctx, "S3 delete "+objectKey, isS3Retryable, func() error {
		return d.client.RemoveObject(ctx, d.bucket, objectKey, minio.RemoveObjectOptions{})
	})
	if err != nil {
		return fmt.Errorf("delete S3 object %q: %w", objectKey, err)
	}
	return nil
}

// List enumerates backup archives in the configured S3 bucket and prefix.
func (d *S3Driver) List(ctx context.Context) ([]RemoteObject, error) {
	prefix := ""
	if d.prefix != "" {
		prefix = d.prefix + "/"
	}
	var out []RemoteObject
	err := withRetry(ctx, "S3 list", isS3Retryable, func() error {
		out = out[:0]
		listCtx, cancel := context.WithCancel(ctx)
		defer cancel()
		for obj := range d.client.ListObjects(listCtx, d.bucket, minio.ListObjectsOptions{
			Prefix:    prefix,
			Recursive: true,
		}) {
			if obj.Err != nil {
				return fmt.Errorf("list S3 objects: %w", obj.Err)
			}
			name := path.Base(obj.Key)
			if !IsBackupArchiveName(name) {
				continue
			}
			out = append(out, RemoteObject{
				Name:         name,
				StoragePath:  obj.Key,
				SizeBytes:    obj.Size,
				LastModified: obj.LastModified.UTC(),
				Encrypted:    strings.HasSuffix(name, ".enc"),
			})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].LastModified.After(out[j].LastModified)
	})
	return out, nil
}

// isS3Retryable reports transient S3 failures (throttling, 5xx, dropped connections).
func isS3Retryable(err error) bool {
	var resp minio.ErrorResponse
	if errors.As(err, &resp) && resp.StatusCode != 0 {
		switch resp.Code {
		case "RequestTimeout", "SlowDown", "InternalError", "ServiceUnavailable":
			return true
		}
		return isRetryableHTTPStatus(resp.StatusCode)
	}
	return isTransientNetworkError(err)
}

func normalizeEndpoint(raw string, useSSL bool) (string, bool) {
	ep := strings.TrimSpace(raw)
	if strings.HasPrefix(ep, "https://") {
		ep = strings.TrimPrefix(ep, "https://")
		useSSL = true
	} else if strings.HasPrefix(ep, "http://") {
		ep = strings.TrimPrefix(ep, "http://")
		useSSL = false
	}
	ep = strings.TrimRight(ep, "/")
	return ep, useSSL
}
