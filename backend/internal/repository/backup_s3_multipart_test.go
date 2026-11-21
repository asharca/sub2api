//go:build unit

package repository

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestArchiveMultipart(t *testing.T) {
	for _, failure := range []bool{false, true} {
		t.Run(map[bool]string{false: "complete", true: "abort"}[failure], func(t *testing.T) {
			var calls []string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "/bucket/archive.gz", r.URL.Path)
				q := r.URL.Query()
				switch {
				case q.Has("uploads"):
					calls = append(calls, "start")
					_, _ = io.WriteString(w, `<InitiateMultipartUploadResult><UploadId>abc</UploadId></InitiateMultipartUploadResult>`)
				case r.Method == "PUT":
					part := q.Get("partNumber")
					calls = append(calls, part)
					n, err := io.Copy(io.Discard, r.Body)
					assert.NoError(t, err)
					assert.Equal(t, map[string]int64{"1": archivePartSize, "2": 1}[part], n)
					if failure && part == "2" {
						w.WriteHeader(403)
						return
					}
					w.Header().Set("ETag", `"tag`+part+`"`)
				case r.Method == "DELETE":
					calls = append(calls, "abort")
					w.WriteHeader(204)
				default:
					calls = append(calls, "complete")
					_, _ = io.WriteString(w, `<CompleteMultipartUploadResult><ETag>done</ETag></CompleteMultipartUploadResult>`)
				}
			}))
			defer srv.Close()
			ctx := context.Background()
			client, err := newS3Client(ctx, s3ClientParams{Endpoint: srv.URL, Region: "auto", AccessKeyID: "ak", SecretAccessKey: "sk", ForcePathStyle: true})
			require.NoError(t, err)
			f, err := os.CreateTemp(t.TempDir(), "archive")
			require.NoError(t, err)
			require.NoError(t, f.Truncate(archivePartSize+1))
			require.NoError(t, f.Close())
			store := S3BackupStore{client: client, bucket: "bucket"}
			n, err := store.UploadFile(ctx, "archive.gz", f.Name(), "application/gzip")
			if failure {
				require.Error(t, err)
				require.Equal(t, []string{"start", "1", "2", "abort"}, calls)
			} else {
				require.NoError(t, err)
				require.Equal(t, archivePartSize+1, n)
				require.Equal(t, []string{"start", "1", "2", "complete"}, calls)
			}
		})
	}
}
