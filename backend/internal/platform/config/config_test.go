package config

import (
	"log/slog"
	"testing"
)

func TestLoad_Defaults(t *testing.T) {
	t.Setenv("PP_DB_URL", "postgres://x")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Env != EnvDev || cfg.HTTPAddr != ":8080" || cfg.DB.MaxConns != 20 || cfg.LogLevel != slog.LevelInfo {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}
}

func TestLoad_FailsFast(t *testing.T) {
	t.Setenv("PP_DB_URL", "")
	t.Setenv("PP_ENV", "prod")
	t.Setenv("PP_LOG_LEVEL", "debug")
	t.Setenv("PP_DB_MAX_CONNS", "abc")
	if _, err := Load(); err == nil {
		t.Fatal("expected aggregated validation error")
	}
}
