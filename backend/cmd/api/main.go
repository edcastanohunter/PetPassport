// Command api es el servidor HTTP de PetPassport. Secuencia de arranque y apagado
// según docs/phase-1/03-arquitectura.md §8.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/edcastanohunter/petpassport/backend/api/gen"
	identityapp "github.com/edcastanohunter/petpassport/backend/internal/identity/app"
	identityhttp "github.com/edcastanohunter/petpassport/backend/internal/identity/http"
	identityinfra "github.com/edcastanohunter/petpassport/backend/internal/identity/infra"
	"github.com/edcastanohunter/petpassport/backend/internal/identity/infra/keycloakadmin"
	orgapp "github.com/edcastanohunter/petpassport/backend/internal/partnernetwork/app"
	orghttp "github.com/edcastanohunter/petpassport/backend/internal/partnernetwork/http"
	orginfra "github.com/edcastanohunter/petpassport/backend/internal/partnernetwork/infra"
	petapp "github.com/edcastanohunter/petpassport/backend/internal/petregistry/app"
	pethttp "github.com/edcastanohunter/petpassport/backend/internal/petregistry/http"
	petinfra "github.com/edcastanohunter/petpassport/backend/internal/petregistry/infra"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/auth"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/authz"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/bus"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/config"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/db"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/eventstore"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/httpserver"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/idempotency"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/mail"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/outbox"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/projection"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/storage"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/telemetry"
)

// version se inyecta en build con -ldflags "-X main.version=...".
var version = "dev"

func main() {
	if err := run(); err != nil {
		slog.Error("api terminated", "error", err)
		os.Exit(1)
	}
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// 1. Configuración (falla rápido).
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}

	// 2. Telemetría.
	tel, err := telemetry.Init(ctx, telemetry.Options{
		ServiceName: "petpassport-api", ServiceVersion: version, Env: string(cfg.Env),
		LogLevel: cfg.LogLevel, OTLPEndpoint: cfg.OTLPEndpoint,
	})
	if err != nil {
		return fmt.Errorf("telemetry: %w", err)
	}
	defer func() { _ = tel.Shutdown(context.Background()) }()
	logger := tel.Logger
	logger.Info("starting", "http_addr", cfg.HTTPAddr, "internal_addr", cfg.InternalAddr)

	// 3. Base de datos (pool + SELECT 1).
	pool, err := db.Open(ctx, cfg.DB.URL, cfg.DB.MaxConns, tel.Registry)
	if err != nil {
		return err
	}
	defer pool.Close()

	// 4. Event store registry — M1.2.

	// 5. Verificador OIDC (IAM-007). Fuera de dev el issuer es obligatorio; en dev
	// sin issuer las rutas protegidas responden 503.
	var verifier *auth.Verifier
	if cfg.OIDC.Issuer != "" {
		verifier, err = newVerifierWithRetry(ctx, logger, auth.VerifierConfig{
			Issuer: cfg.OIDC.Issuer, DiscoveryURL: cfg.OIDC.DiscoveryURL,
			Audience: cfg.OIDC.Audience, AllowedAZP: cfg.OIDC.AllowedAZP,
		})
		if err != nil {
			return fmt.Errorf("oidc: %w", err)
		}
	} else {
		logger.Warn("PP_OIDC_ISSUER vacío: autenticación deshabilitada (solo dev)")
	}

	// 6. Event Store (ARQ-004/007): registro de tipos, validación de arranque,
	// store con métricas.
	registry := eventstore.NewRegistry()
	petinfra.RegisterEvents(registry)
	orginfra.RegisterEvents(registry)
	if err := registry.Validate(); err != nil {
		return fmt.Errorf("eventstore registry: %w", err)
	}
	store := eventstore.NewStore(registry, pool, tel.Registry)
	tel.Registry.MustRegister(outbox.PendingGauge(pool))

	// 7. Módulos. Identity (M1.1); Pet Registry (M1.2); Partner Network (M1.3);
	// administración (M1.4).
	// Un único runner de proyecciones: las reacciones entre contextos (p. ej.
	// OrganizationSuspended → pet_care_links) ocurren en la misma tx (DOM-011).
	identityRepo := identityinfra.NewPostgresRepository(pool)
	identitySvc := identityapp.NewService(identityRepo, logger)
	unit := db.NewUnitOfWork(pool)
	runner := projection.NewRunner()
	commands := bus.New()
	idemStore := idempotency.NewStore(pool, 0)
	var mailer *mail.SMTP
	if cfg.SMTP.Host != "" {
		mailer = mail.NewSMTP(mail.Options{
			Host: cfg.SMTP.Host, Port: cfg.SMTP.Port, From: cfg.SMTP.From,
			User: cfg.SMTP.User, Password: cfg.SMTP.Password,
		})
	} else {
		logger.Warn("PP_SMTP_HOST vacío: emails transaccionales deshabilitados")
	}

	// Partner Network: membresías del Principal con caché LRU 60 s (IAM-008),
	// invalidada por los comandos que cambian membresías (DOM-011).
	userAdapter := orginfra.NewUserAdapter(identitySvc)
	orgQueries := orginfra.NewQueries(pool.Pool, userAdapter)
	membershipCache := auth.NewMembershipCache(orginfra.NewMembershipProvider(orgQueries), 60*time.Second, 0)
	orgOpts := orgapp.Options{
		Repo:        orginfra.NewRepository(store, registry, nil, nil),
		UoW:         unit,
		Projects:    orginfra.NewSyncProjector(runner),
		Codes:       orginfra.PublicCodes{},
		Tokens:      orginfra.NewInvitationTokens(pool.Pool),
		Queries:     orgQueries,
		Users:       userAdapter,
		Memberships: membershipCache,
		Auditor:     orginfra.TxAuditor{},
		BaseURL:     cfg.PublicBaseURL,
		Logger:      logger,
	}
	if mailer != nil {
		orgOpts.Mailer = mailer
	}
	orgSvc := orgapp.NewService(orgOpts)
	orgSvc.RegisterCommands(commands)
	runner.Register(orginfra.NewOrgsProjector(registry))

	// Pet Registry.
	guardianAdapter := petinfra.NewGuardianAdapter(identitySvc)
	orgAdapter := petinfra.NewOrganizationAdapter(orgQueries)
	petQueries := petinfra.NewQueries(pool.Pool).WithGuardian(guardianAdapter)
	petOpts := petapp.Options{
		Repo:     petinfra.NewRepository(store, registry, nil, nil),
		UoW:      unit,
		Projects: petinfra.NewSyncProjector(runner),
		Reserve:  petinfra.NewReservations(pool),
		Guardian: guardianAdapter,
		Orgs:     orgAdapter,
		Queries:  petQueries,
		Events:   store,
		BaseURL:  cfg.PublicBaseURL,
		Logger:   logger,
	}
	if mailer != nil {
		petOpts.Mailer = mailer
	}
	petSvc := petapp.NewService(petOpts)
	petSvc.RegisterCommands(commands)
	runner.Register(petinfra.NewPetsProjector(registry, guardianAdapter).WithOrganizations(orgAdapter))
	runner.Register(petinfra.OrgReactionsProjector{})
	var objects storage.ObjectStorage
	if cfg.Storage.Endpoint != "" {
		objects, err = storage.NewMinIO(ctx, storage.MinIOOptions{
			Endpoint: cfg.Storage.Endpoint, Bucket: cfg.Storage.Bucket,
			AccessKey: cfg.Storage.AccessKey, SecretKey: cfg.Storage.SecretKey, UseSSL: cfg.Storage.UseSSL,
		})
		if err != nil {
			return fmt.Errorf("storage: %w", err)
		}
	} else {
		logger.Warn("PP_STORAGE_ENDPOINT vacío: subida de fotos deshabilitada")
	}
	authz.InitMetrics(tel.Registry)

	// Administración (API-008): service account de Keycloak (IAM-011). Sin
	// secreto (solo dev) la suspensión queda solo en la plataforma.
	adminOpts := identityapp.AdminOptions{
		Repo: identityRepo, Directory: identityRepo, Pets: petQueries,
		Memberships: orginfra.NewMembershipProvider(orgQueries),
		Audit:       identityinfra.NewAuditLog(pool), Logger: logger,
	}
	if cfg.Keycloak.AdminClientSecret != "" {
		kc, err := keycloakadmin.New(keycloakadmin.Options{
			BaseURL: cfg.Keycloak.BaseURL, Realm: cfg.Keycloak.Realm,
			ClientID: cfg.Keycloak.AdminClientID, ClientSecret: cfg.Keycloak.AdminClientSecret,
		})
		if err != nil {
			return fmt.Errorf("keycloak admin: %w", err)
		}
		adminOpts.Keycloak = kc
	} else {
		logger.Warn("PP_KC_ADMIN_CLIENT_SECRET vacío: suspensión de usuarios sin sincronizar con Keycloak (solo dev)")
	}
	adminSvc := identityapp.NewAdminService(adminOpts)

	spec, err := gen.GetSpec()
	if err != nil {
		return fmt.Errorf("openapi: %w", err)
	}
	guard, err := auth.NewGuard(spec, auth.GuardOptions{
		Verifier: verifier, Resolver: identitySvc, Memberships: membershipCache, Logger: logger, BasePath: "/api/v1",
	})
	if err != nil {
		return err
	}

	// 8. Router y servidores.
	health := httpserver.NewHealth()
	health.Register("db", pool.Ping)
	identityHandlers := identityhttp.NewHandlers(identitySvc, cfg.IPHashSalt)
	servers := httpserver.New(cfg.HTTPAddr, cfg.InternalAddr, httpserver.Options{
		Logger: logger, Registry: tel.Registry, Health: health,
		CORSOrigins: cfg.CORSOrigins, Dev: cfg.IsDev(),
		Handlers: apiHandlers{
			Handlers:      identityHandlers,
			AdminHandlers: identityhttp.NewAdminHandlers(adminSvc, identityHandlers),
			PetHandlers:   pethttp.NewPetHandlers(petSvc, petQueries, idemStore, objects, logger).WithGuardian(guardianAdapter),
			OrgHandlers: orghttp.NewOrgHandlers(orgSvc, idemStore, membershipCache, identityHandlers, logger).
				WithIPHashSalt(cfg.IPHashSalt),
		},
		Guard: guard.Middleware,
	})
	errs := servers.Start()
	health.SetReady(true)
	logger.Info("ready")

	// Apagado ordenado: readyz=fail → drenado → Shutdown → cierre del pool (defer).
	select {
	case err := <-errs:
		return fmt.Errorf("server: %w", err)
	case <-ctx.Done():
	}
	logger.Info("shutting down")
	health.SetReady(false)
	time.Sleep(cfg.ShutdownDrain)
	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	if err := servers.Shutdown(shutdownCtx); err != nil && !errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("shutdown: %w", err)
	}
	logger.Info("stopped")
	return nil
}

// newVerifierWithRetry espera al proveedor OIDC hasta ~60 s (Keycloak tarda en
// arrancar en compose); después falla y el proceso no arranca (IAM-007 §1).
func newVerifierWithRetry(ctx context.Context, logger *slog.Logger, cfg auth.VerifierConfig) (*auth.Verifier, error) {
	const attempts = 20
	var lastErr error
	for i := 1; i <= attempts; i++ {
		v, err := auth.NewVerifier(ctx, cfg)
		if err == nil {
			return v, nil
		}
		lastErr = err
		logger.Warn("oidc discovery failed, retrying", "attempt", i, "error", err.Error())
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(3 * time.Second):
		}
	}
	return nil, lastErr
}
