package objectstore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

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
	client  Client
	bucket  string
	workDir string
}

// NewWorkingFileSource keeps a seekable download until processing finishes.
func NewWorkingFileSource(client Client, bucket, workDir string) *FileSource {
	return &FileSource{client: client, bucket: bucket, workDir: workDir}
}

func (f *FileSource) path(ref string) string {
	return filepath.Join(f.workDir, fmt.Sprintf("reader-%x.download", sha256.Sum256([]byte(f.bucket+"\x00"+ref))))
}

// PruneWorkingCopies runs under the polling lock when the ledger has no
// active file. It handles a crash between a done checkpoint and unlinking.
func (f *FileSource) PruneWorkingCopies() error {
	if f.workDir == "" {
		return nil
	}
	entries, err := os.ReadDir(f.workDir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !entry.Type().IsRegular() {
			continue
		}
		name := strings.TrimSuffix(entry.Name(), ".partial")
		digest, matches := strings.CutPrefix(name, "reader-")
		if !matches {
			continue
		}
		digest, matches = strings.CutSuffix(digest, ".download")
		if !matches || len(digest) != sha256.Size*2 {
			continue
		}
		if _, err := hex.DecodeString(digest); err != nil {
			continue
		}
		if err := os.Remove(filepath.Join(f.workDir, entry.Name())); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

func (f *FileSource) RemoveWorkingCopy(ref string) error {
	if f.workDir == "" {
		return nil
	}
	err := os.Remove(f.path(ref))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
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
	if f.workDir == "" {
		object, _, err := f.openObject(ctx, ref)
		if err != nil {
			return nil, err
		}
		return object, nil
	}
	path := f.path(ref)
	if file, err := os.Open(path); !errors.Is(err, os.ErrNotExist) {
		return file, err
	}
	if err := os.MkdirAll(f.workDir, 0700); err != nil {
		return nil, err
	}
	source, info, err := f.openObject(ctx, ref)
	if err != nil {
		return nil, err
	}
	defer source.Close()

	partial := path + ".partial"
	target, err := os.OpenFile(partial, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return nil, err
	}
	defer os.Remove(partial)
	_, copyErr := io.Copy(target, source)
	if err := errors.Join(copyErr, target.Sync(), target.Close()); err != nil {
		return nil, err
	}
	if err := os.Chtimes(partial, info.LastModified, info.LastModified); err != nil {
		return nil, err
	}
	if err := os.Rename(partial, path); err != nil {
		return nil, err
	}
	return os.Open(path)
}

func (f *FileSource) openObject(ctx context.Context, ref string) (*minio.Object, minio.ObjectInfo, error) {
	obj, err := f.client.GetObject(ctx, f.bucket, ref, minio.GetObjectOptions{})
	if err != nil {
		return nil, minio.ObjectInfo{}, fmt.Errorf("objectstore: opening %q in bucket %q: %w", ref, f.bucket, err)
	}
	// minio.Object is returned lazily by the SDK - Stat confirms the
	// object actually exists and is readable now, rather than
	// surfacing a confusing error on the caller's first Read call.
	info, err := obj.Stat()
	if err != nil {
		obj.Close()
		return nil, minio.ObjectInfo{}, fmt.Errorf("objectstore: stat %q in bucket %q: %w", ref, f.bucket, err)
	}
	return obj, info, nil
}
