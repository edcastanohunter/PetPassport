package httpserver

import (
	"bytes"
	"net/http"
	"strings"
)

// etagMiddleware intercepta peticiones GET/HEAD con If-None-Match (PP-F3-PRF-006).
// Si el handler establece un ETag coincidente, devuelve 304 Not Modified sin cuerpo.
func etagMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			next.ServeHTTP(w, r)
			return
		}

		ifNoneMatch := r.Header.Get("If-None-Match")
		if ifNoneMatch == "" {
			next.ServeHTTP(w, r)
			return
		}

		rw := &etagResponseWriter{
			ResponseWriter: w,
			header:         make(http.Header),
			buf:            &bytes.Buffer{},
		}
		next.ServeHTTP(rw, r)

		etag := rw.header.Get("ETag")
		if etag != "" && (rw.status == 0 || (rw.status >= 200 && rw.status < 300)) && matchesETag(etag, ifNoneMatch) {
			for k, v := range rw.header {
				if strings.EqualFold(k, "Content-Length") || strings.EqualFold(k, "Content-Type") {
					continue
				}
				for _, val := range v {
					w.Header().Add(k, val)
				}
			}
			w.WriteHeader(http.StatusNotModified)
			return
		}

		for k, v := range rw.header {
			for _, val := range v {
				w.Header().Add(k, val)
			}
		}
		if rw.status != 0 {
			w.WriteHeader(rw.status)
		}
		if rw.buf.Len() > 0 {
			_, _ = w.Write(rw.buf.Bytes())
		}
	})
}

type etagResponseWriter struct {
	http.ResponseWriter
	header      http.Header
	buf         *bytes.Buffer
	status      int
	wroteHeader bool
}

func (w *etagResponseWriter) Header() http.Header {
	return w.header
}

func (w *etagResponseWriter) WriteHeader(status int) {
	if !w.wroteHeader {
		w.status = status
		w.wroteHeader = true
	}
}

func (w *etagResponseWriter) Write(b []byte) (int, error) {
	if !w.wroteHeader {
		w.status = http.StatusOK
		w.wroteHeader = true
	}
	return w.buf.Write(b)
}

func matchesETag(etag, ifNoneMatch string) bool {
	ifNoneMatch = strings.TrimSpace(ifNoneMatch)
	if ifNoneMatch == "*" {
		return true
	}
	cleanETag := normalizeETag(etag)
	for _, candidate := range strings.Split(ifNoneMatch, ",") {
		if normalizeETag(candidate) == cleanETag {
			return true
		}
	}
	return false
}

func normalizeETag(t string) string {
	t = strings.TrimSpace(t)
	t = strings.TrimPrefix(t, "W/")
	return strings.Trim(t, `"`)
}
