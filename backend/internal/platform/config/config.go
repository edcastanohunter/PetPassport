// Package config carga la configuración 12-factor del proceso (variables PP_*)
// y falla rápido si falta algo obligatorio. Ver docs/phase-1/03-arquitectura.md §8.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"
)

// Env identifica el entorno de ejecución.
type Env string

const (
	EnvDev     Env = "dev"
	EnvStaging Env = "staging"
	EnvProd    Env = "prod"
)

// Config es la configuración completa del proceso.
type Config struct {
	Env          Env
	HTTPAddr     string
	InternalAddr string

	DB struct {
		URL      string
		MaxConns int32
	}

	OIDC struct {
		Issuer       string
		DiscoveryURL string // red interna (dev y Quadlet: http://keycloak:8080/realms/petpassport); vacío = Issuer
		Audience     string
		AllowedAZP   []string // azp permitidos (IAM-007)
	}

	// IPHashSalt sala el hash de IP del audit log (SEC-007). Obligatoria fuera de dev.
	IPHashSalt string

	// Keycloak Admin API con la service account (IAM-011). BaseURL y Realm se
	// derivan de PP_OIDC_DISCOVERY_URL / PP_OIDC_ISSUER salvo PP_KC_ADMIN_URL.
	Keycloak struct {
		BaseURL           string
		Realm             string
		AdminClientID     string
		AdminClientSecret string
	}

	Storage struct {
		Endpoint  string
		Bucket    string
		AccessKey string
		SecretKey string
		UseSSL    bool
		// FiscalBucket guarda facturas y notas crédito con Object Lock y
		// retención FiscalRetention (PP-F5-ARQ-003, CMP-009).
		FiscalBucket    string
		FiscalRetention time.Duration
	}

	// Notifications (M2.4): el api ya no abre SMTP (ARQ-014); solo firma/valida
	// tokens de baja y verifica webhooks del proveedor de email.
	Notifications struct {
		UnsubscribeSecret  string // PP_UNSUBSCRIBE_SECRET (≥ 32 bytes fuera de dev)
		EmailWebhookSecret string // PP_EMAIL_WEBHOOK_SECRET (proveedor "generic"); vacío = webhooks 404
	}

	CORSOrigins   []string
	OTLPEndpoint  string
	LogLevel      slog.Level
	PublicBaseURL string
	Billing       Billing
	TypstBin      string

	Redis struct {
		Addr     string
		Password string
		UseTLS   bool
	}
	Search struct {
		Addresses []string
		Username  string
		Password  string
	}
	RateLimit struct {
		Allowlist []string
	}

	ShutdownDrain   time.Duration
	ShutdownTimeout time.Duration
}

// Load lee las variables de entorno y valida la configuración.
func Load() (*Config, error) {
	var cfg Config
	var errs []error

	cfg.Env = Env(getenv("PP_ENV", string(EnvDev)))
	switch cfg.Env {
	case EnvDev, EnvStaging, EnvProd:
	default:
		errs = append(errs, fmt.Errorf("PP_ENV inválido: %q", cfg.Env))
	}

	cfg.HTTPAddr = getenv("PP_HTTP_ADDR", ":8080")
	cfg.InternalAddr = getenv("PP_INTERNAL_ADDR", ":9090")

	cfg.DB.URL = os.Getenv("PP_DB_URL")
	if cfg.DB.URL == "" {
		errs = append(errs, errors.New("PP_DB_URL es obligatoria"))
	}
	maxConns, err := getenvInt("PP_DB_MAX_CONNS", 20)
	if err != nil {
		errs = append(errs, err)
	}
	if maxConns < 1 || maxConns > 1000 {
		errs = append(errs, fmt.Errorf("PP_DB_MAX_CONNS fuera de rango [1,1000]: %d", maxConns))
		maxConns = 20
	}
	cfg.DB.MaxConns = int32(maxConns) //nolint:gosec // rango validado arriba

	// OIDC es obligatorio fuera de dev. Sin issuer en dev, las rutas protegidas responden 503.
	cfg.OIDC.Issuer = os.Getenv("PP_OIDC_ISSUER")
	cfg.OIDC.DiscoveryURL = os.Getenv("PP_OIDC_DISCOVERY_URL")
	cfg.OIDC.Audience = getenv("PP_OIDC_AUDIENCE", "petpassport-api")
	cfg.OIDC.AllowedAZP = splitCSV(getenv("PP_OIDC_ALLOWED_AZP", "petpassport-web"))
	if cfg.Env != EnvDev && cfg.OIDC.Issuer == "" {
		errs = append(errs, errors.New("PP_OIDC_ISSUER es obligatoria fuera de dev"))
	}

	cfg.IPHashSalt = os.Getenv("PP_IP_HASH_SALT")
	if cfg.Env != EnvDev && len(cfg.IPHashSalt) < 16 {
		errs = append(errs, errors.New("PP_IP_HASH_SALT (≥16 caracteres) es obligatoria fuera de dev"))
	}
	if cfg.IPHashSalt == "" {
		cfg.IPHashSalt = "dev-salt-not-for-production"
	}

	cfg.Keycloak.AdminClientID = getenv("PP_KC_ADMIN_CLIENT_ID", "petpassport-backend")
	cfg.Keycloak.AdminClientSecret = os.Getenv("PP_KC_ADMIN_CLIENT_SECRET")
	issuerURL := cfg.OIDC.DiscoveryURL
	if issuerURL == "" {
		issuerURL = cfg.OIDC.Issuer
	}
	cfg.Keycloak.BaseURL, cfg.Keycloak.Realm = splitRealmURL(issuerURL)
	if v := os.Getenv("PP_KC_ADMIN_URL"); v != "" {
		cfg.Keycloak.BaseURL = strings.TrimRight(v, "/")
	}
	if cfg.Env != EnvDev && (cfg.Keycloak.AdminClientSecret == "" || cfg.Keycloak.BaseURL == "") {
		errs = append(errs, errors.New("PP_KC_ADMIN_CLIENT_SECRET y la URL de Keycloak son obligatorias fuera de dev (IAM-011)"))
	}

	cfg.Storage.Endpoint = os.Getenv("PP_STORAGE_ENDPOINT")
	cfg.Storage.Bucket = getenv("PP_STORAGE_BUCKET", "petpassport-media")
	cfg.Storage.AccessKey = os.Getenv("PP_STORAGE_ACCESS_KEY")
	cfg.Storage.SecretKey = os.Getenv("PP_STORAGE_SECRET_KEY")
	cfg.Storage.UseSSL = getenv("PP_STORAGE_USE_SSL", "false") == "true"
	cfg.Storage.FiscalBucket, cfg.Storage.FiscalRetention = fiscalStorage(cfg.Env, &errs)

	cfg.Notifications.UnsubscribeSecret = unsubscribeSecret(cfg.Env, &errs)
	cfg.Notifications.EmailWebhookSecret = os.Getenv("PP_EMAIL_WEBHOOK_SECRET")

	cfg.CORSOrigins = splitCSV(getenv("PP_CORS_ORIGINS", "http://localhost:5173"))
	cfg.OTLPEndpoint = os.Getenv("PP_OTEL_EXPORTER_OTLP_ENDPOINT")

	level, err := parseLevel(getenv("PP_LOG_LEVEL", "info"))
	if err != nil {
		errs = append(errs, err)
	}
	cfg.LogLevel = level
	if cfg.Env == EnvProd && level == slog.LevelDebug {
		errs = append(errs, errors.New("PP_LOG_LEVEL=debug no está permitido en prod"))
	}

	cfg.PublicBaseURL = getenv("PP_PUBLIC_BASE_URL", "http://localhost:5173")
	cfg.TypstBin = getenv("PP_TYPST_BIN", "typst")

	cfg.Redis.Addr = os.Getenv("PP_REDIS_ADDR")
	cfg.Redis.Password = os.Getenv("PP_REDIS_PASSWORD")
	cfg.Redis.UseTLS = os.Getenv("PP_REDIS_USE_TLS") == "true"
	if al := os.Getenv("PP_RATELIMIT_ALLOWLIST"); al != "" {
		cfg.RateLimit.Allowlist = splitCSV(al)
	}

	cfg.Search.Addresses = splitCSV(getenv("PP_OPENSEARCH_URLS", "http://localhost:9200"))
	cfg.Search.Username = os.Getenv("PP_OPENSEARCH_USERNAME")
	cfg.Search.Password = os.Getenv("PP_OPENSEARCH_PASSWORD")

	cfg.Billing = loadBilling(cfg.Env, &errs)

	cfg.ShutdownDrain = 5 * time.Second
	cfg.ShutdownTimeout = 30 * time.Second

	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	return &cfg, nil
}

// IsDev indica si el proceso corre en desarrollo.
func (c *Config) IsDev() bool { return c.Env == EnvDev }

// splitRealmURL separa https://host/realms/<realm> en (https://host, realm).
func splitRealmURL(u string) (base, realm string) {
	u = strings.TrimRight(u, "/")
	i := strings.LastIndex(u, "/realms/")
	if i < 0 {
		return "", ""
	}
	return u[:i], u[i+len("/realms/"):]
}

func getenv(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}

// fiscalRetentionDays: 10 años (3 bisiestos) de conservación de documentos
// fiscales (RN-34, CMP-009; Estatuto Tributario art. 632 y Código de Comercio).
const fiscalRetentionDays = 3653

// fiscalStorage lee el bucket fiscal y su retención. Fuera de dev la retención
// no puede bajar de 10 años: un valor menor solo sirve para poder vaciar el
// MinIO de desarrollo.
func fiscalStorage(env Env, errs *[]error) (string, time.Duration) {
	bucket := getenv("PP_STORAGE_FISCAL_BUCKET", "petpassport-compliance-archive")
	days, err := getenvInt("PP_STORAGE_FISCAL_RETENTION_DAYS", fiscalRetentionDays)
	if err != nil {
		*errs = append(*errs, err)
		return bucket, fiscalRetentionDays * 24 * time.Hour
	}
	if days < 1 || (env != EnvDev && days < fiscalRetentionDays) {
		*errs = append(*errs, fmt.Errorf("PP_STORAGE_FISCAL_RETENTION_DAYS=%d: fuera de dev el mínimo es %d (10 años)", days, fiscalRetentionDays))
	}
	return bucket, time.Duration(days) * 24 * time.Hour
}

func getenvInt(key string, def int) (int, error) {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return def, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, fmt.Errorf("%s debe ser un entero: %w", key, err)
	}
	return n, nil
}

func splitCSV(s string) []string {
	parts := strings.Split(s, ",")
	out := parts[:0]
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func parseLevel(s string) (slog.Level, error) {
	switch strings.ToLower(s) {
	case "debug":
		return slog.LevelDebug, nil
	case "info":
		return slog.LevelInfo, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	}
	return 0, fmt.Errorf("PP_LOG_LEVEL inválido: %q", s)
}

// Worker es la configuración de `cmd/worker` (PP-F2-ARQ-001): solo lo que el
// worker necesita; no exige OIDC ni Keycloak.
type Worker struct {
	Env          Env
	InternalAddr string // /healthz, /readyz, /metrics
	DBURL        string
	DBMaxConns   int32
	Group        string // lock de instancia worker.<group>
	OTLPEndpoint string
	LogLevel     slog.Level

	// Documentos PDF (F2 INF-004): storage compartido con el api y binario
	// Typst. Sin endpoint de storage el runner de documentos no arranca.
	Storage struct {
		Endpoint  string
		Bucket    string
		AccessKey string
		SecretKey string
		UseSSL    bool
		// FiscalBucket guarda facturas y notas crédito con Object Lock y
		// retención FiscalRetention (PP-F5-ARQ-003, CMP-009).
		FiscalBucket    string
		FiscalRetention time.Duration
	}
	TypstBin      string
	PublicBaseURL string
	Billing       Billing

	// Email transaccional (M2.4): solo el worker abre SMTP (ARQ-014).
	SMTP struct {
		Host     string
		Port     int
		User     string
		Password string
		From     string
	}
	APIPublicURL      string // base pública de la API (destino de List-Unsubscribe-Post)
	UnsubscribeSecret string // PP_UNSUBSCRIBE_SECRET, el mismo que el api
	MailAddress       string // dirección física del pie de los correos
	WorkerID          string // PP_WORKER_ID (identificador único para leases ARQ-009)
	Redis             struct {
		Addr     string
		Password string
		UseTLS   bool
	}
	Search struct {
		Addresses []string
		Username  string
		Password  string
	}
	AsynqConcurrency int // PP_ASYNQ_CONCURRENCY (defecto 10)
}

// LoadWorker lee PP_* del worker y valida.
func LoadWorker() (*Worker, error) {
	var cfg Worker
	var errs []error
	cfg.Env = Env(getenv("PP_ENV", string(EnvDev)))
	switch cfg.Env {
	case EnvDev, EnvStaging, EnvProd:
	default:
		errs = append(errs, fmt.Errorf("PP_ENV inválido: %q", cfg.Env))
	}
	cfg.InternalAddr = getenv("PP_INTERNAL_ADDR", ":9090")
	cfg.DBURL = os.Getenv("PP_DB_URL")
	if cfg.DBURL == "" {
		errs = append(errs, errors.New("PP_DB_URL es obligatoria"))
	}
	maxConns, err := getenvInt("PP_DB_MAX_CONNS", 10)
	if err != nil {
		errs = append(errs, err)
	}
	if maxConns < 4 || maxConns > 200 {
		// Lock de instancia + runners de proyección, outbox y scheduler.
		errs = append(errs, fmt.Errorf("PP_DB_MAX_CONNS del worker fuera de rango [4,200]: %d", maxConns))
		maxConns = 10
	}
	cfg.DBMaxConns = int32(maxConns) //nolint:gosec // rango validado arriba
	cfg.Group = getenv("PP_WORKER_GROUP", "default")
	cfg.OTLPEndpoint = os.Getenv("PP_OTEL_EXPORTER_OTLP_ENDPOINT")
	level, err := parseLevel(getenv("PP_LOG_LEVEL", "info"))
	if err != nil {
		errs = append(errs, err)
	}
	cfg.LogLevel = level
	if cfg.Env == EnvProd && level == slog.LevelDebug {
		errs = append(errs, errors.New("PP_LOG_LEVEL=debug no está permitido en prod"))
	}
	cfg.Storage.Endpoint = os.Getenv("PP_STORAGE_ENDPOINT")
	cfg.Storage.Bucket = getenv("PP_STORAGE_BUCKET", "petpassport-media")
	cfg.Storage.AccessKey = os.Getenv("PP_STORAGE_ACCESS_KEY")
	cfg.Storage.SecretKey = os.Getenv("PP_STORAGE_SECRET_KEY")
	cfg.Storage.UseSSL = getenv("PP_STORAGE_USE_SSL", "false") == "true"
	cfg.Storage.FiscalBucket, cfg.Storage.FiscalRetention = fiscalStorage(cfg.Env, &errs)
	cfg.TypstBin = getenv("PP_TYPST_BIN", "typst")
	cfg.PublicBaseURL = getenv("PP_PUBLIC_BASE_URL", "http://localhost:5173")
	cfg.TypstBin = getenv("PP_TYPST_BIN", "typst")

	cfg.SMTP.Host = os.Getenv("PP_SMTP_HOST")
	port, err := getenvInt("PP_SMTP_PORT", 1025)
	if err != nil {
		errs = append(errs, err)
	}
	cfg.SMTP.Port = port
	cfg.SMTP.User = os.Getenv("PP_SMTP_USER")
	cfg.SMTP.Password = os.Getenv("PP_SMTP_PASSWORD")
	cfg.SMTP.From = getenv("PP_SMTP_FROM", "no-reply@petpassport.local")
	cfg.APIPublicURL = getenv("PP_API_PUBLIC_URL", "http://api.pp.localhost/api/v1")
	cfg.UnsubscribeSecret = unsubscribeSecret(cfg.Env, &errs)
	cfg.MailAddress = os.Getenv("PP_MAIL_ADDRESS")
	if cfg.Env != EnvDev && cfg.MailAddress == "" {
		errs = append(errs, errors.New("PP_MAIL_ADDRESS (dirección física del pie de los correos) es obligatoria fuera de dev"))
	}
	if cfg.MailAddress == "" {
		cfg.MailAddress = "PetPassport"
	}
	if cfg.Env != EnvDev && cfg.SMTP.Host == "" {
		errs = append(errs, errors.New("PP_SMTP_HOST es obligatoria fuera de dev: sin ella el worker no puede enviar correos"))
	}

	hostname, _ := os.Hostname()
	if hostname == "" {
		hostname = "worker"
	}
	cfg.WorkerID = getenv("PP_WORKER_ID", fmt.Sprintf("%s-%d", hostname, os.Getpid()))
	cfg.Redis.Addr = os.Getenv("PP_REDIS_ADDR")
	cfg.Redis.Password = os.Getenv("PP_REDIS_PASSWORD")
	cfg.Redis.UseTLS = os.Getenv("PP_REDIS_USE_TLS") == "true"
	asynqConc, _ := getenvInt("PP_ASYNQ_CONCURRENCY", 10)
	cfg.AsynqConcurrency = asynqConc

	cfg.Search.Addresses = splitCSV(getenv("PP_OPENSEARCH_URLS", "http://localhost:9200"))
	cfg.Search.Username = os.Getenv("PP_OPENSEARCH_USERNAME")
	cfg.Search.Password = os.Getenv("PP_OPENSEARCH_PASSWORD")
	cfg.Billing = loadBilling(cfg.Env, &errs)

	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	return &cfg, nil
}

// Billing es la configuración de cobro y facturación (PP-F5-ARQ-002, SEC-001).
// Los secretos solo viven en el entorno: nunca en el repo ni en logs.
type Billing struct {
	// Wompi (Colombia). Con PP_WOMPI_PRIVATE_KEY vacía el adaptador no se registra.
	WompiBaseURL         string // PP_WOMPI_BASE_URL (defecto: sandbox fuera de prod)
	WompiPublicKey       string // PP_WOMPI_PUBLIC_KEY
	WompiPrivateKey      string // PP_WOMPI_PRIVATE_KEY
	WompiIntegritySecret string // PP_WOMPI_INTEGRITY_SECRET
	WompiEventsSecret    string // PP_WOMPI_EVENTS_SECRET
	// FakeWebhookSecret firma los webhooks del proveedor fake (dev y staging).
	FakeWebhookSecret string // PP_BILLING_FAKE_WEBHOOK_SECRET
}

// WompiEnabled indica si hay credenciales de Wompi.
func (b Billing) WompiEnabled() bool { return b.WompiPrivateKey != "" }

func loadBilling(env Env, errs *[]error) Billing {
	b := Billing{
		WompiBaseURL:         os.Getenv("PP_WOMPI_BASE_URL"),
		WompiPublicKey:       os.Getenv("PP_WOMPI_PUBLIC_KEY"),
		WompiPrivateKey:      os.Getenv("PP_WOMPI_PRIVATE_KEY"),
		WompiIntegritySecret: os.Getenv("PP_WOMPI_INTEGRITY_SECRET"),
		WompiEventsSecret:    os.Getenv("PP_WOMPI_EVENTS_SECRET"),
		FakeWebhookSecret:    os.Getenv("PP_BILLING_FAKE_WEBHOOK_SECRET"),
	}
	if b.WompiBaseURL == "" {
		b.WompiBaseURL = "https://sandbox.wompi.co/v1"
		if env == EnvProd {
			b.WompiBaseURL = "https://production.wompi.co/v1"
		}
	}
	if b.WompiEnabled() && (b.WompiPublicKey == "" || b.WompiIntegritySecret == "" || b.WompiEventsSecret == "") {
		*errs = append(*errs, errors.New("wompi requiere PP_WOMPI_PUBLIC_KEY, PP_WOMPI_INTEGRITY_SECRET y PP_WOMPI_EVENTS_SECRET junto a PP_WOMPI_PRIVATE_KEY"))
	}
	if env == EnvProd && b.WompiEnabled() && strings.Contains(b.WompiBaseURL, "sandbox") {
		*errs = append(*errs, errors.New("PP_WOMPI_BASE_URL apunta a sandbox en prod"))
	}
	return b
}

// unsubscribeSecret lee PP_UNSUBSCRIBE_SECRET: ≥ 32 bytes fuera de dev; en dev
// hay un valor por defecto no apto para producción.
func unsubscribeSecret(env Env, errs *[]error) string {
	v := os.Getenv("PP_UNSUBSCRIBE_SECRET")
	if env != EnvDev && len(v) < 32 {
		*errs = append(*errs, errors.New("PP_UNSUBSCRIBE_SECRET debe tener al menos 32 bytes fuera de dev (NOT-004)"))
	}
	if v == "" {
		v = "dev-unsubscribe-secret-not-for-production"
	}
	return v
}
