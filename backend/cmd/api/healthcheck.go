package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"time"
)

// healthcheck implementa `api healthcheck`: GET a /readyz del listener
// interno. La imagen es distroless (sin shell ni curl), así que el
// `healthcheck` de compose invoca el propio binario (INF-006/INF-008).
func healthcheck() int {
	// Siempre loopback: de la configuración solo se toma el puerto.
	port := "9090"
	if _, p, err := net.SplitHostPort(os.Getenv("PP_INTERNAL_ADDR")); err == nil && p != "" {
		port = p
	}
	target := "http://" + net.JoinHostPort("127.0.0.1", port) + "/readyz"
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil) // #nosec G704 -- host fijo 127.0.0.1
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	resp, err := http.DefaultClient.Do(req) // #nosec G704 -- host fijo 127.0.0.1
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		fmt.Fprintf(os.Stderr, "readyz: %d\n", resp.StatusCode)
		return 1
	}
	return 0
}
