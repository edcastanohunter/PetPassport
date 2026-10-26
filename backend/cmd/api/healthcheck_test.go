package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHealthcheck_ReflectsReadyz(t *testing.T) {
	status := http.StatusOK
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/readyz" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(status)
	}))
	defer srv.Close()
	t.Setenv("PP_INTERNAL_ADDR", strings.TrimPrefix(srv.URL, "http://"))

	if code := healthcheck(); code != 0 {
		t.Fatalf("readyz 200 → exit %d", code)
	}
	status = http.StatusServiceUnavailable
	if code := healthcheck(); code != 1 {
		t.Fatalf("readyz 503 → exit %d", code)
	}
	srv.Close()
	if code := healthcheck(); code != 1 {
		t.Fatalf("sin listener → exit %d", code)
	}
}
