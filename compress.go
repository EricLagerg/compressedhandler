package compressedhandler

import (
	"compress/flate"
	"compress/gzip"
	"compress/lzw"
	"io"
	"net/http"
	"strconv"
	"strings"
)

//go:generate stringer -type=flateType
type flateType uint8

const (
	None flateType = iota
	Deflate
	Compress
	Gzip
)

type codings map[string]float64

// The default qvalue to assign to an encoding if no explicit qvalue is set.
// This is actually kind of ambiguous in RFC 2616, so hopefully it's correct.
// The examples seem to indicate that it is.
const DefaultQValue = 1.0

// CompressedResponseWriter provides an http.ResponseWriter interface, which
// compresses bytes before writing them to the underlying response. This
// doesn't set the Content-Encoding header, nor close the writers, so don't
// forget to do that.
type CompressedResponseWriter struct {
	io.Writer
	http.ResponseWriter
}

// Write appends data to the compressed writer.
func (c CompressedResponseWriter) Write(b []byte) (int, error) {
	return c.Writer.Write(b)
}

// CompressedHandler wraps an HTTP handler, to transparently compress the
// response body if the client supports it (via the Accept-Encoding header).
func CompressedHandler(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch accepts(r) {
		// Bytes written during ServeHTTP are redirected to a specific
		// writer before being written to the underlying response.

		case Gzip:
			gzw := gzip.NewWriter(w)
			defer gzw.Close()

			w.Header().Set("Content-Encoding", "gzip")
			h.ServeHTTP(CompressedResponseWriter{gzw, w}, r)

		case Compress:
			lzww := lzw.NewWriter(w, lzw.MSB, 8)
			defer lzww.Close()

			w.Header().Set("Content-Encoding", "compress")
			h.ServeHTTP(CompressedResponseWriter{lzww, w}, r)

		case Deflate:
			flw, _ := flate.NewWriter(w, flate.DefaultCompression)
			defer flw.Close()

			w.Header().Set("Content-Encoding", "deflate")
			h.ServeHTTP(CompressedResponseWriter{flw, w}, r)

		default:
			h.ServeHTTP(w, r)
		}
	})
}

// accepts indicates the highest level of compression the browser supports.
func accepts(r *http.Request) flateType {
	acceptedEncodings, _ := parseEncodings(r.Header.Get("Accept-Encoding"))

	if acceptedEncodings["gzip"] > 0.0 {
		return Gzip
	}

	if acceptedEncodings["compress"] > 0.0 {
		return Compress
	}

	if acceptedEncodings["deflate"] > 0.0 {
		return Deflate
	}

	return None
}

// parseEncodings attempts to parse a list of codings, per RFC 2616, as might
// appear in an Accept-Encoding header. It returns a map of content-codings to
// quality values, and an error containing the errors encounted. It's probably
// safe to ignore those, because silently ignoring errors is how the internet
// works.
//
// See: http://tools.ietf.org/html/rfc2616#section-14.3
func parseEncodings(s string) (codings, error) {
	c := make(codings)
	e := make(ErrorList, 0)

	for _, ss := range strings.Split(s, ",") {
		coding, qvalue, err := parseCoding(ss)
		println(coding)

		if err != nil {
			e = append(e, KeyError{ss, err})

		} else {
			c[coding] = qvalue
		}
	}

	if len(e) > 0 {
		return c, &e
	}

	return c, nil
}

// parseCoding parses a single coding (content-coding with an optional qvalue),
// as might appear in an Accept-Encoding header. It attempts to forgive minor
// formatting errors.
func parseCoding(s string) (coding string, qvalue float64, err error) {
	for n, part := range strings.Split(s, ";") {
		part = strings.TrimSpace(part)
		qvalue = DefaultQValue

		if n == 0 {
			coding = strings.ToLower(part)

		} else if strings.HasPrefix(part, "q=") {
			qvalue, err = strconv.ParseFloat(strings.TrimPrefix(part, "q="), 64)

			if qvalue < 0.0 {
				qvalue = 0.0

			} else if qvalue > 1.0 {
				qvalue = 1.0
			}
		}
	}

	if coding == "" {
		err = ErrEmptyContentCoding
	}

	return
}
