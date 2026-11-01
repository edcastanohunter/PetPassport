package testkit

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

// Redis arranca un contenedor redis:7-alpine o usa la variable TEST_REDIS_ADDR.
// Devuelve la dirección "host:port".
func Redis(t testing.TB) string {
	t.Helper()

	if addr := os.Getenv("TEST_REDIS_ADDR"); addr != "" {
		return addr
	}

	ctx := context.Background()
	req := testcontainers.ContainerRequest{
		Image:        "redis:7-alpine",
		ExposedPorts: []string{"6379/tcp"},
		WaitingFor:   wait.ForLog("Ready to accept connections").WithOccurrence(1).WithStartupTimeout(30 * time.Second),
	}

	ctr, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		t.Fatalf("redis container: %v", err)
	}

	t.Cleanup(func() {
		_ = ctr.Terminate(context.Background())
	})

	endpoint, err := ctr.Endpoint(ctx, "")
	if err != nil {
		t.Fatalf("redis endpoint: %v", err)
	}

	return endpoint
}
