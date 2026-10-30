package config

import (
	"log/slog"
	"strings"
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
	t.Setenv("PP_UNSUBSCRIBE_SECRET", strings.Repeat("u", 32))
	if _, err := Load(); err == nil {
		t.Fatal("staging sin PP_KC_ADMIN_CLIENT_SECRET debe fallar")
	}
	t.Setenv("PP_KC_ADMIN_CLIENT_SECRET", "s3cret")
	if _, err := Load(); err != nil {
		t.Fatal(err)
	}
}

func TestLoad_UnsubscribeSecretRequiredOutsideDev(t *testing.T) {
	t.Setenv("PP_DB_URL", "postgres://x")
	t.Setenv("PP_ENV", "staging")
	t.Setenv("PP_OIDC_ISSUER", "https://auth.example.com/realms/petpassport")
	t.Setenv("PP_IP_HASH_SALT", "0123456789abcdef")
	t.Setenv("PP_KC_ADMIN_CLIENT_SECRET", "s3cret")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "PP_UNSUBSCRIBE_SECRET") {
		t.Fatalf("staging sin PP_UNSUBSCRIBE_SECRET debe fallar: %v", err)
	}
	t.Setenv("PP_UNSUBSCRIBE_SECRET", "corto")
	if _, err := Load(); err == nil {
		t.Fatal("un secreto de menos de 32 bytes debe fallar")
	}
	t.Setenv("PP_UNSUBSCRIBE_SECRET", strings.Repeat("u", 32))
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Notifications.UnsubscribeSecret != strings.Repeat("u", 32) {
		t.Fatalf("secreto: %q", cfg.Notifications.UnsubscribeSecret)
	}
}

func TestLoadWorker_MailSettings(t *testing.T) {
	t.Setenv("PP_DB_URL", "postgres://x")
	cfg, err := LoadWorker()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SMTP.Port != 1025 || cfg.APIPublicURL == "" || cfg.MailAddress == "" || cfg.UnsubscribeSecret == "" {
		t.Fatalf("defaults de dev: %+v", cfg)
	}
	t.Setenv("PP_ENV", "prod")
	if _, err := LoadWorker(); err == nil {
		t.Fatal("prod sin SMTP, secreto y dirección debe fallar")
	}
	t.Setenv("PP_SMTP_HOST", "smtp.example.com")
	t.Setenv("PP_UNSUBSCRIBE_SECRET", strings.Repeat("u", 32))
	t.Setenv("PP_MAIL_ADDRESS", "PetPassport · Calle 1, Bogotá")
	if _, err := LoadWorker(); err != nil {
		t.Fatal(err)
	}
}
