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

func TestLoad_KeycloakAdminDerivedFromDiscovery(t *testing.T) {
	t.Setenv("PP_DB_URL", "postgres://x")
	t.Setenv("PP_OIDC_ISSUER", "https://auth.example.com/realms/petpassport")
	t.Setenv("PP_OIDC_DISCOVERY_URL", "http://keycloak:8080/realms/petpassport/")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Keycloak.BaseURL != "http://keycloak:8080" || cfg.Keycloak.Realm != "petpassport" {
		t.Fatalf("keycloak admin: %+v", cfg.Keycloak)
	}
	t.Setenv("PP_KC_ADMIN_URL", "http://kc-internal:8080/")
	if cfg, _ = Load(); cfg.Keycloak.BaseURL != "http://kc-internal:8080" {
		t.Fatalf("override: %+v", cfg.Keycloak)
	}
}

func TestLoad_KeycloakAdminSecretRequiredOutsideDev(t *testing.T) {
	t.Setenv("PP_DB_URL", "postgres://x")
	t.Setenv("PP_ENV", "staging")
	t.Setenv("PP_OIDC_ISSUER", "https://auth.example.com/realms/petpassport")
	t.Setenv("PP_IP_HASH_SALT", "0123456789abcdef")
	if _, err := Load(); err == nil {
		t.Fatal("staging sin PP_KC_ADMIN_CLIENT_SECRET debe fallar")
	}
	t.Setenv("PP_KC_ADMIN_CLIENT_SECRET", "s3cret")
	if _, err := Load(); err != nil {
		t.Fatal(err)
	}
}
