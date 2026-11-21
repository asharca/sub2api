//go:build unit

package repository

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestS3BackupStore_UploadFile(t *testing.T) {
	var received []byte
	var receivedLength int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPut, r.Method)
		receivedLength = r.ContentLength
		var err error
		received, err = io.ReadAll(r.Body)
		require.NoError(t, err)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client, err := newS3Client(context.Background(), s3ClientParams{
		Endpoint:        server.URL,
		Region:          "auto",
		AccessKeyID:     "test-ak",
		SecretAccessKey: "test-sk",
		ForcePathStyle:  true,
	})
	require.NoError(t, err)

	content := []byte("streamed backup payload")
	filePath := t.TempDir() + "/part.gz"
	require.NoError(t, os.WriteFile(filePath, content, 0o600))

	store := &S3BackupStore{client: client, bucket: "backup-bucket"}
	size, err := store.UploadFile(context.Background(), "backup/part-1", filePath, "application/octet-stream")
	require.NoError(t, err)
	require.Equal(t, int64(len(content)), size)
	require.Equal(t, int64(len(content)), receivedLength)
	require.Equal(t, content, received)
}

func TestS3BackupStore_UploadFileWithProgress(t *testing.T) {
	for _, tc := range []struct {
		name      string
		content   string
		status    int
		confirmed []int64
	}{
		{"small", "backup", http.StatusOK, []int64{6}},
		{"empty", "", http.StatusOK, []int64{0}},
		{"rejected", "backup", http.StatusForbidden, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
			}))
			defer server.Close()
			ctx := context.Background()
			client, err := newS3Client(ctx, s3ClientParams{Endpoint: server.URL, Region: "auto", AccessKeyID: "ak", SecretAccessKey: "sk", ForcePathStyle: true})
			require.NoError(t, err)
			filePath := t.TempDir() + "/archive.gz"
			require.NoError(t, os.WriteFile(filePath, []byte(tc.content), 0o600))
			store := S3BackupStore{client: client, bucket: "backup-bucket"}
			var confirmed []int64
			size, err := store.UploadFileWithProgress(ctx, "backup/archive.gz", filePath, "application/gzip", func(n int64) {
				confirmed = append(confirmed, n)
			})
			if tc.status == http.StatusOK {
				require.NoError(t, err)
				require.Equal(t, int64(len(tc.content)), size)
			} else {
				require.Error(t, err)
				require.Zero(t, size)
			}
			require.Equal(t, tc.confirmed, confirmed)
		})
	}
}
