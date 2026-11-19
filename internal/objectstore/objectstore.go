package objectstore

import (
	"context"
	"fmt"
	"io"

	"github.com/minio/minio-go/v7"
)

// Client is the minimal subset of *minio.Client this adapter needs,
// so it can be tested against a fake.
type Client interface {
	GetObject(ctx context.Context, bucketName, objectName string, opts minio.GetObjectOptions) (*minio.Object, error)
}

type DeleteClient interface {
	RemoveObject(context.Context, string, string, minio.RemoveObjectOptions) error
}

// ObjectRemover deletes source objects from the same bucket used by FileSource.
func ObjectRemover(client DeleteClient, bucket string) func(context.Context, string) error {
	return func(ctx context.Context, ref string) error {
		err := client.RemoveObject(ctx, bucket, ref, minio.RemoveObjectOptions{})
		if minio.ToErrorResponse(err).Code == "NoSuchKey" {
			return nil
		}
		return err
	}
}

// FileSource is the MinIO-backed ports.FileSource adapter.
type FileSource struct {
	client Client
	bucket string
}

// NewFileSource returns a FileSource reading from bucket via client.
func NewFileSource(client Client, bucket string) *FileSource {
	return &FileSource{client: client, bucket: bucket}
}

// Open streams the object identified by ref (its MinIO object key)
// from the configured bucket. The returned ReadCloser streams directly
// from MinIO - the Phase 1 batch reader never needs the whole object
// in memory, per specs/mission.md's large-file requirement.
func (f *FileSource) Open(ctx context.Context, ref string) (io.ReadCloser, error) {
	obj, err := f.client.GetObject(ctx, f.bucket, ref, minio.GetObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("objectstore: opening %q in bucket %q: %w", ref, f.bucket, err)
	}
	// minio.Object is returned lazily by the SDK - Stat confirms the
	// object actually exists and is readable now, rather than
	// surfacing a confusing error on the caller's first Read call.
	if _, err := obj.Stat(); err != nil {
		obj.Close()
		return nil, fmt.Errorf("objectstore: stat %q in bucket %q: %w", ref, f.bucket, err)
	}
	return obj, nil
}
