package files

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// terminalErrorReader returns its error once, with the last bytes, then EOF.
// A later read must not hide the original error.
type terminalErrorReader struct {
	data []byte
	err  error
}

func (r *terminalErrorReader) Read(p []byte) (int, error) {
	n := copy(p, r.data)
	r.data = r.data[n:]
	if len(r.data) > 0 {
		return n, nil
	}
	if r.err != nil {
		err := r.err
		r.err = nil
		return n, err
	}
	return n, io.EOF
}

func assertNoUpload(t *testing.T, h *Handler) {
	t.Helper()
	var rows int
	if err := h.Store.DB().QueryRow("SELECT count(*) FROM uploads").Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 0 {
		t.Errorf("upload rows left: %d", rows)
	}
	entries, err := os.ReadDir(h.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("upload files left: %v", entries)
	}
	if len(h.reserved) != 0 {
		t.Errorf("quota reservations left: %v", h.reserved)
	}
}

func TestSavePreservesReadErrors(t *testing.T) {
	for _, readErr := range []error{errors.New("read failed"), io.ErrUnexpectedEOF} {
		for _, size := range []int{0, 100, 511, 512, 700, 1024} {
			for _, separate := range []bool{false, true} {
				t.Run(fmt.Sprintf("%v/%d/separate=%t", readErr, size, separate), func(t *testing.T) {
					data := bytes.Repeat([]byte("x"), size)
					body := func() io.Reader {
						if separate {
							return io.MultiReader(bytes.NewReader(data), &terminalErrorReader{err: readErr})
						}
						return &terminalErrorReader{data: data, err: readErr}
					}
					h := quotaHandler(t)
					h.MaxBytes, h.QuotaBytes = 1024, 3072
					dst := filepath.Join(h.Dir, "upload")
					if _, _, err := h.save(dst, body()); !errors.Is(err, readErr) {
						t.Errorf("save error = %v, want %v", err, readErr)
					}
					assertNoUpload(t, h)
					// The HTTP path must reject the same error and release its quota.
					if code := serveUpload(h, body(), -1); code != http.StatusInternalServerError {
						t.Errorf("status = %d, want 500", code)
					}
					assertNoUpload(t, h)
				})
			}
		}
	}
}

func TestUploadRejectsLengthMismatch(t *testing.T) {
	for _, tc := range []struct {
		size   int
		length int64
	}{{0, 1000}, {100, 1000}, {700, 1000}, {1001, 1000}, {100, 0}} {
		t.Run(fmt.Sprintf("size=%d/length=%d", tc.size, tc.length), func(t *testing.T) {
			h := quotaHandler(t)
			h.MaxBytes, h.QuotaBytes = 2048, 6144
			if code := serveUpload(h, strings.NewReader(strings.Repeat("x", tc.size)), tc.length); code != http.StatusBadRequest {
				t.Errorf("status = %d, want 400", code)
			}
			assertNoUpload(t, h)
		})
	}
}

func TestUploadBodyBoundaries(t *testing.T) {
	for _, maxBytes := range []int{100, 1024} {
		for _, size := range []int{0, 1, 100, 511, 512, 513, maxBytes, maxBytes + 1} {
			if size > maxBytes+1 {
				continue
			}
			for _, unknown := range []bool{false, true} {
				t.Run(fmt.Sprintf("max=%d/size=%d/unknown=%t", maxBytes, size, unknown), func(t *testing.T) {
					h := quotaHandler(t)
					h.MaxBytes, h.QuotaBytes = int64(maxBytes), 3*int64(maxBytes)
					length := int64(size)
					if unknown {
						length = -1
					}
					data := bytes.Repeat([]byte("x"), size)
					want := http.StatusOK
					if size == 0 {
						want = http.StatusBadRequest
					} else if size > maxBytes {
						want = http.StatusRequestEntityTooLarge
					}
					if code := serveUpload(h, bytes.NewReader(data), length); code != want {
						t.Fatalf("status = %d, want %d", code, want)
					}
					if want != http.StatusOK {
						assertNoUpload(t, h)
						return
					}
					var key string
					var storedSize int64
					if err := h.Store.DB().QueryRow("SELECT key, size FROM uploads").Scan(&key, &storedSize); err != nil {
						t.Fatal(err)
					}
					stored, err := os.ReadFile(filepath.Join(h.Dir, key))
					if err != nil || !bytes.Equal(stored, data) || storedSize != int64(size) {
						t.Fatalf("stored %d bytes, row size %d, error %v", len(stored), storedSize, err)
					}
				})
			}
		}
	}
}

func TestUploadRawHTTPBodies(t *testing.T) {
	for _, tc := range []struct {
		name    string
		size    int
		chunked bool
		want    int
	}{
		{"truncated sniff", 100, false, http.StatusInternalServerError},
		{"truncated copy", 700, false, http.StatusInternalServerError},
		{"complete", 1000, false, http.StatusOK},
		{"chunked small", 100, true, http.StatusOK},
		{"chunked large", 1000, true, http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := quotaHandler(t)
			h.MaxBytes, h.QuotaBytes = 2048, 6144
			done := make(chan struct{})
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				defer close(done)
				h.ServeHTTP(w, r)
			}))
			defer srv.Close()
			conn, err := net.DialTimeout("tcp", srv.Listener.Addr().String(), 5*time.Second)
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			if err := conn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
				t.Fatal(err)
			}
			framing := "Content-Length: 1000\r\n"
			body := strings.Repeat("x", tc.size)
			if tc.chunked {
				framing = "Transfer-Encoding: chunked\r\n"
				body = fmt.Sprintf("%x\r\n%s\r\n0\r\n\r\n", tc.size, body)
			}
			if _, err := fmt.Fprintf(conn, "POST /api/upload HTTP/1.1\r\nHost: localhost\r\nX-Upload-Token: good\r\nConnection: close\r\n%s\r\n%s", framing, body); err != nil {
				t.Fatal(err)
			}
			if err := conn.(*net.TCPConn).CloseWrite(); err != nil {
				t.Fatal(err)
			}
			resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			if _, err := io.Copy(io.Discard, resp.Body); err != nil {
				t.Fatal(err)
			}
			if resp.StatusCode != tc.want {
				t.Errorf("status = %d, want %d", resp.StatusCode, tc.want)
			}
			<-done
			if tc.want != http.StatusOK {
				assertNoUpload(t, h)
			}
		})
	}
}
