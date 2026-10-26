package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
)

// healthcheck implementa `api healthcheck`: GET a /readyz del listener
// interno. La imagen es distroless (sin shell ni curl), así que el
// `healthcheck` de compose invoca el propio binario (INF-006/INF-008).
func healthcheck() int {
	addr := os.Getenv("PP_INTERNAL_ADDR")
	if addr == "" {
		addr = ":9090"
	}
	if strings.HasPrefix(addr, ":") {
		addr = "127.0.0.1" + addr
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+addr+"/readyz", nil)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	resp, err := http.DefaultClient.Do(req)
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
