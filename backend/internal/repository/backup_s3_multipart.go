package repository

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

const archivePartSize int64 = 64 << 20

func (s *S3BackupStore) uploadMultipart(ctx context.Context, key string, file io.ReaderAt, size int64, contentType string) (_ int64, err error) {
	// S3 accepts at most 10,000 parts; use larger sections when necessary.
	partSize := max(archivePartSize, (size-1)/10000+1)
	if partSize > 5<<30 {
		return 0, fmt.Errorf("archive exceeds S3 multipart capacity: %d bytes", size)
	}
	start, err := s.client.CreateMultipartUpload(ctx, &s3.CreateMultipartUploadInput{Bucket: &s.bucket, Key: &key, ContentType: &contentType})
	if err != nil {
		return 0, fmt.Errorf("create multipart upload: %w", err)
	}
	id := start.UploadId
	if id == nil || *id == "" {
		return 0, errors.New("create multipart upload: missing upload ID")
	}
	defer func() {
		if err != nil {
			cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
			defer cancel()
			if _, abortErr := s.client.AbortMultipartUpload(cleanup, &s3.AbortMultipartUploadInput{Bucket: &s.bucket, Key: &key, UploadId: id}); abortErr != nil {
				err = errors.Join(err, fmt.Errorf("abort multipart upload: %w", abortErr))
			}
		}
	}()
	parts := make([]types.CompletedPart, 0, (size-1)/partSize+1)
	for off := int64(0); off < size; off += partSize {
		n := min(partSize, size-off)
		part := int32(len(parts) + 1)
		out, err := s.client.UploadPart(ctx, &s3.UploadPartInput{Bucket: &s.bucket, Key: &key, UploadId: id, PartNumber: &part, ContentLength: &n, Body: io.NewSectionReader(file, off, n)})
		if err != nil {
			return 0, fmt.Errorf("upload part %d: %w", part, err)
		}
		if out.ETag == nil || *out.ETag == "" {
			return 0, fmt.Errorf("upload part %d: missing ETag", part)
		}
		parts = append(parts, types.CompletedPart{ETag: out.ETag, PartNumber: &part, ChecksumCRC32: out.ChecksumCRC32, ChecksumCRC32C: out.ChecksumCRC32C, ChecksumSHA1: out.ChecksumSHA1, ChecksumSHA256: out.ChecksumSHA256, ChecksumCRC64NVME: out.ChecksumCRC64NVME})
	}
	_, err = s.client.CompleteMultipartUpload(ctx, &s3.CompleteMultipartUploadInput{Bucket: &s.bucket, Key: &key, UploadId: id, MultipartUpload: &types.CompletedMultipartUpload{Parts: parts}})
	if err != nil {
		return 0, fmt.Errorf("complete multipart upload: %w", err)
	}
	return size, nil
}
