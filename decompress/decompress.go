package decompress

import (
	"bytes"
	"compress/gzip"
	"compress/zlib"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
	"github.com/caddyserver/caddy/v2/caddyconfig/httpcaddyfile"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
	"github.com/dustin/go-humanize"
	"github.com/prometheus/client_golang/prometheus"
)

const defaultMaxSize = 10 << 20

func init() {
	caddy.RegisterModule(Decompress{})
	httpcaddyfile.RegisterHandlerDirective("request_decompress", parseCaddyfile)
}

// Decompress inflates gzip or deflate request bodies before passing the request on.
type Decompress struct {
	MaxSize int64 `json:"max_size,omitempty"`

	byteCounter *prometheus.CounterVec
}

func (Decompress) CaddyModule() caddy.ModuleInfo {
	return caddy.ModuleInfo{
		ID:  "http.handlers.request_decompress",
		New: func() caddy.Module { return new(Decompress) },
	}
}

func (d *Decompress) Provision(ctx caddy.Context) error {
	if d.MaxSize <= 0 {
		d.MaxSize = defaultMaxSize
	}
	counter := prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "caddy",
		Subsystem: "request_decompress",
		Name:      "bytes_total",
		Help:      "Request body bytes of decompressed requests, as received (stage=wire) and after decompression (stage=decoded).",
	}, []string{"encoding", "stage"})
	if err := ctx.GetMetricsRegistry().Register(counter); err != nil {
		var exists prometheus.AlreadyRegisteredError
		if !errors.As(err, &exists) {
			return err
		}
		counter = exists.ExistingCollector.(*prometheus.CounterVec)
	}
	d.byteCounter = counter
	return nil
}

func (d Decompress) ServeHTTP(w http.ResponseWriter, r *http.Request, next caddyhttp.Handler) error {
	encoding := strings.ToLower(strings.TrimSpace(r.Header.Get("Content-Encoding")))
	wire := &countingReader{r: r.Body}
	var dec io.Reader
	var err error
	switch encoding {
	case "gzip":
		dec, err = gzip.NewReader(wire)
	case "deflate":
		dec, err = zlib.NewReader(wire)
	default:
		return next.ServeHTTP(w, r)
	}
	if err != nil {
		return bodyError(err)
	}

	body, err := io.ReadAll(io.LimitReader(dec, d.MaxSize+1))
	if err != nil {
		return bodyError(err)
	}
	if int64(len(body)) > d.MaxSize {
		return caddyhttp.Error(http.StatusRequestEntityTooLarge, fmt.Errorf("decompressed body exceeds %d bytes", d.MaxSize))
	}

	if d.byteCounter != nil {
		d.byteCounter.WithLabelValues(encoding, "wire").Add(float64(wire.n))
		d.byteCounter.WithLabelValues(encoding, "decoded").Add(float64(len(body)))
	}

	r.Header.Del("Content-Encoding")
	r.Header.Set("Content-Length", strconv.Itoa(len(body)))
	r.ContentLength = int64(len(body))
	r.Body = io.NopCloser(bytes.NewReader(body))
	return next.ServeHTTP(w, r)
}

type countingReader struct {
	r io.Reader
	n int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	return n, err
}

func bodyError(err error) error {
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		return caddyhttp.Error(http.StatusRequestEntityTooLarge, err)
	}
	return caddyhttp.Error(http.StatusBadRequest, err)
}

// parseCaddyfile parses `request_decompress [<max_size>]`.
func parseCaddyfile(h httpcaddyfile.Helper) (caddyhttp.MiddlewareHandler, error) {
	d := new(Decompress)
	err := d.UnmarshalCaddyfile(h.Dispenser)
	return d, err
}

func (d *Decompress) UnmarshalCaddyfile(disp *caddyfile.Dispenser) error {
	disp.Next()
	if disp.NextArg() {
		size, err := humanize.ParseBytes(disp.Val())
		if err != nil {
			return disp.Errf("parsing max_size: %v", err)
		}
		d.MaxSize = int64(size)
	}
	if disp.NextArg() {
		return disp.ArgErr()
	}
	return nil
}

var (
	_ caddy.Provisioner           = (*Decompress)(nil)
	_ caddyhttp.MiddlewareHandler = (*Decompress)(nil)
	_ caddyfile.Unmarshaler       = (*Decompress)(nil)
)
