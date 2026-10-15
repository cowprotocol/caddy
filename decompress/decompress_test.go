package decompress

import (
	"bytes"
	"compress/gzip"
	"compress/zlib"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

const payload = `{"jsonrpc":"2.0","id":1,"method":"eth_blockNumber","params":[]}`

func gzipped(s string) []byte {
	var b bytes.Buffer
	w := gzip.NewWriter(&b)
	w.Write([]byte(s))
	w.Close()
	return b.Bytes()
}

func deflated(s string) []byte {
	var b bytes.Buffer
	w := zlib.NewWriter(&b)
	w.Write([]byte(s))
	w.Close()
	return b.Bytes()
}

func TestByteCounter(t *testing.T) {
	counter := prometheus.NewCounterVec(prometheus.CounterOpts{Name: "test_bytes_total"}, []string{"encoding", "stage"})
	d := Decompress{MaxSize: defaultMaxSize, byteCounter: counter}
	body := gzipped(payload)
	noop := caddyhttp.HandlerFunc(func(http.ResponseWriter, *http.Request) error { return nil })

	for _, enc := range []string{"gzip", ""} {
		req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
		req.Header.Set("Content-Encoding", enc)
		if err := d.ServeHTTP(httptest.NewRecorder(), req, noop); err != nil {
			t.Fatal(err)
		}
	}

	if got := testutil.ToFloat64(counter.WithLabelValues("gzip", "wire")); got != float64(len(body)) {
		t.Fatalf("wire = %v, want %d", got, len(body))
	}
	if got := testutil.ToFloat64(counter.WithLabelValues("gzip", "decoded")); got != float64(len(payload)) {
		t.Fatalf("decoded = %v, want %d", got, len(payload))
	}
	if n := testutil.CollectAndCount(counter); n != 2 {
		t.Fatalf("plain request must not be counted, got %d series", n)
	}
}

func TestServeHTTP(t *testing.T) {
	cases := []struct {
		name     string
		encoding string
		body     []byte
		maxSize  int64
		wantBody string
		wantErr  int
	}{
		{"gzip", "gzip", gzipped(payload), defaultMaxSize, payload, 0},
		{"deflate", "Deflate", deflated(payload), defaultMaxSize, payload, 0},
		{"plain passes through", "", []byte(payload), defaultMaxSize, payload, 0},
		{"unknown encoding passes through", "br", []byte("raw"), defaultMaxSize, "raw", 0},
		{"corrupt body", "gzip", []byte("not gzip"), defaultMaxSize, "", http.StatusBadRequest},
		{"too large", "gzip", gzipped(payload), 10, "", http.StatusRequestEntityTooLarge},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(tc.body))
			if tc.encoding != "" {
				req.Header.Set("Content-Encoding", tc.encoding)
			}
			var got []byte
			var gotEncoding string
			var gotLength int64
			next := caddyhttp.HandlerFunc(func(w http.ResponseWriter, r *http.Request) error {
				got, _ = io.ReadAll(r.Body)
				gotEncoding = r.Header.Get("Content-Encoding")
				gotLength = r.ContentLength
				return nil
			})

			err := Decompress{MaxSize: tc.maxSize}.ServeHTTP(httptest.NewRecorder(), req, next)

			if tc.wantErr != 0 {
				herr, ok := err.(caddyhttp.HandlerError)
				if !ok || herr.StatusCode != tc.wantErr {
					t.Fatalf("want status %d, got %v", tc.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tc.wantBody {
				t.Fatalf("body = %q, want %q", got, tc.wantBody)
			}
			if tc.encoding == "gzip" || tc.encoding == "Deflate" {
				if gotEncoding != "" || gotLength != int64(len(tc.wantBody)) {
					t.Fatalf("encoding %q length %d, want stripped and %d", gotEncoding, gotLength, len(tc.wantBody))
				}
			}
		})
	}
}
