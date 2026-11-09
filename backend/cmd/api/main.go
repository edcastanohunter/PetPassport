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

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/edcastanohunter/petpassport/backend/api/gen"
	clinicalapp "github.com/edcastanohunter/petpassport/backend/internal/clinical/app"
	clinicalhttp "github.com/edcastanohunter/petpassport/backend/internal/clinical/http"
	clinicalinfra "github.com/edcastanohunter/petpassport/backend/internal/clinical/infra"
	identityapp "github.com/edcastanohunter/petpassport/backend/internal/identity/app"
	identityhttp "github.com/edcastanohunter/petpassport/backend/internal/identity/http"
	identityinfra "github.com/edcastanohunter/petpassport/backend/internal/identity/infra"
	"github.com/edcastanohunter/petpassport/backend/internal/identity/infra/keycloakadmin"
	medicationsapp "github.com/edcastanohunter/petpassport/backend/internal/medications/app"
	medicationshttp "github.com/edcastanohunter/petpassport/backend/internal/medications/http"
	medicationsinfra "github.com/edcastanohunter/petpassport/backend/internal/medications/infra"
	notificationsapp "github.com/edcastanohunter/petpassport/backend/internal/notifications/app"
	notificationshttp "github.com/edcastanohunter/petpassport/backend/internal/notifications/http"
	notificationsinfra "github.com/edcastanohunter/petpassport/backend/internal/notifications/infra"
	orgapp "github.com/edcastanohunter/petpassport/backend/internal/partnernetwork/app"
	orghttp "github.com/edcastanohunter/petpassport/backend/internal/partnernetwork/http"
	orginfra "github.com/edcastanohunter/petpassport/backend/internal/partnernetwork/infra"
	petapp "github.com/edcastanohunter/petpassport/backend/internal/petregistry/app"
	pethttp "github.com/edcastanohunter/petpassport/backend/internal/petregistry/http"
	petinfra "github.com/edcastanohunter/petpassport/backend/internal/petregistry/infra"
	pharmacyapp "github.com/edcastanohunter/petpassport/backend/internal/pharmacyorders/app"
	pharmacyordersdomain "github.com/edcastanohunter/petpassport/backend/internal/pharmacyorders/domain"
	pharmacyhttp "github.com/edcastanohunter/petpassport/backend/internal/pharmacyorders/http"
	pharmacyinfra "github.com/edcastanohunter/petpassport/backend/internal/pharmacyorders/infra"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/auth"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/authz"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/bus"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/cache"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/config"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/db"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/eventstore"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/httpserver"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/idempotency"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/maintenance"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/outbox"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/pdfrender"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/projection"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/queue"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/ratelimit"
	ppredis "github.com/edcastanohunter/petpassport/backend/internal/platform/redis"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/storage"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/telemetry"
	prescriptionapp "github.com/edcastanohunter/petpassport/backend/internal/prescriptions/app"
	prescriptiondomain "github.com/edcastanohunter/petpassport/backend/internal/prescriptions/domain"
	prescriptionhttp "github.com/edcastanohunter/petpassport/backend/internal/prescriptions/http"
	prescriptioninfra "github.com/edcastanohunter/petpassport/backend/internal/prescriptions/infra"
	schedulingapp "github.com/edcastanohunter/petpassport/backend/internal/scheduling/app"
	schedulinghttp "github.com/edcastanohunter/petpassport/backend/internal/scheduling/http"
	schedulinginfra "github.com/edcastanohunter/petpassport/backend/internal/scheduling/infra"
	searchapp "github.com/edcastanohunter/petpassport/backend/internal/search/app"
	searchhttp "github.com/edcastanohunter/petpassport/backend/internal/search/http"
	searchinfra "github.com/edcastanohunter/petpassport/backend/internal/search/infra"
)

// version se inyecta en build con -ldflags "-X main.version=...".
var version = "dev"

func main() {
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		os.Exit(healthcheck())
	}
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
	petinfra.RegisterUpcasters(registry)
	orginfra.RegisterEvents(registry)
	clinicalinfra.RegisterEvents(registry)
	schedulinginfra.RegisterEvents(registry)
	prescriptioninfra.RegisterEvents(registry)
	pharmacyinfra.RegisterEvents(registry)
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
	runner := projection.NewRunner().Instrument(tel.Registry)
	commands := bus.New().Instrument(tel.Registry)
	idemStore := idempotency.NewStore(pool, 0).Instrument(tel.Registry)
	// Notifications (ARQ-014): los módulos encolan los correos con token; el
	// SMTP es solo del worker.
	notifier := notificationsinfra.NewEnqueuer(pool.Pool)

	// Partner Network: membresías del Principal con caché LRU 60 s (IAM-008),
	// invalidada por los comandos que cambian membresías (DOM-011).
	// Redis, caché compartida (ARQ-005, ARQ-006) y rate limiting distribuido (ARQ-007).
	var redisClient *ppredis.Client
	var distCache *cache.Cache
	if cfg.Redis.Addr != "" {
		rc, err := ppredis.NewClient(ppredis.Config{
			Addr:         cfg.Redis.Addr,
			Password:     cfg.Redis.Password,
			DB:           0,
			UseTLS:       cfg.Redis.UseTLS,
			DialTimeout:  200 * time.Millisecond,
			ReadTimeout:  100 * time.Millisecond,
			WriteTimeout: 200 * time.Millisecond,
			ClientName:   "api",
		})
		if err != nil {
			logger.Warn("redis connection failed (failing open)", "error", err)
		} else {
			redisClient = rc
			defer func() { _ = rc.Close() }()
			distCache = cache.New(rc)
			if err := distCache.SubscribeInvalidations(ctx); err != nil {
				logger.Warn("subscribing to cache invalidations failed", "error", err)
			}
		}
	}
	var queueClient *queue.Client
	if cfg.Redis.Addr != "" {
		queueClient = queue.NewClient(cfg.Redis.Addr, cfg.Redis.Password, cfg.Redis.UseTLS, logger)
		defer func() { _ = queueClient.Close() }()
	}
	limiter := ratelimit.NewLimiter(redisClient)
	rlConfig := ratelimit.DefaultConfig()
	rlConfig.Allowlist = cfg.RateLimit.Allowlist
	rateLimitMW := ratelimit.Middleware(limiter, rlConfig)

	userAdapter := orginfra.NewUserAdapter(identitySvc)
	orgQueries := orginfra.NewQueries(pool.Pool, userAdapter)
	membershipCache := auth.NewMembershipCache(orginfra.NewMembershipProvider(orgQueries), 300*time.Second, 0).WithCache(distCache)
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
		Notifier:    notifier,
		Logger:      logger,
	}
	orgSvc := orgapp.NewService(orgOpts)
	orgSvc.RegisterCommands(commands)
	orginfra.RegisterSyncProjectors(runner, registry)

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
		Notifier: notifier,
		Logger:   logger,
		// Solicitud de vínculo y bandeja del tutor (M2.5): la bandeja compone
		// las propuestas de cita y los controles de otros contextos.
		RequestQueries: petinfra.NewRequestQueries(pool.Pool),
		LookupLimiter:  petinfra.NewLookupLimiter(pool.Pool),
		InboxSources: []petapp.InboxSource{
			petinfra.NewProposalInbox(pool.Pool), petinfra.NewFollowUpInbox(pool.Pool),
		},
	}
	petSvc := petapp.NewService(petOpts)
	petSvc.RegisterCommands(commands)
	petinfra.RegisterSyncProjectors(runner, registry, guardianAdapter, orgAdapter)
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

	// Medication Catalog (F3 M3.2).
	medStore := medicationsinfra.NewStore(pool.Pool)
	medSvc := medicationsapp.NewCatalogService(medStore)

	// Clinical Records (F2 M2.1).
	clinicalQueries := clinicalinfra.NewQueries(pool.Pool)
	clinicalPets := clinicalinfra.NewCachedPetAccessAdapter(clinicalinfra.NewPetAccess(pool.Pool), distCache)
	clinicalSvc := clinicalapp.NewService(clinicalapp.Options{
		Repo:         clinicalinfra.NewRepository(store, registry, nil, nil),
		UoW:          unit,
		Projects:     clinicalinfra.NewSyncProjector(runner),
		PetAccess:    clinicalPets,
		Membership:   clinicalinfra.NewMembership(pool.Pool),
		Appointments: clinicalinfra.NewAppointmentAdapter(pool.Pool),
		Catalog:      medSvc,
		Queries:      clinicalQueries,
		Logger:       logger,
	})
	clinicalSvc.RegisterCommands(commands)
	cardQueries := clinicalinfra.NewCardQueries(pool.Pool)
	cardSvc := clinicalapp.NewCardService(clinicalapp.CardOptions{
		Repo:       clinicalinfra.NewCardRepository(store, registry, nil, nil),
		UoW:        unit,
		Projects:   clinicalinfra.NewSyncProjector(runner),
		PetAccess:  clinicalPets,
		Membership: clinicalinfra.NewMembership(pool.Pool),
		Catalog:    cardQueries,
		Logger:     logger,
	})
	cardSvc.RegisterCommands(commands)
	clinicalinfra.RegisterSyncProjectors(runner, registry)
	clinicalHandlers := clinicalhttp.NewClinicalHandlers(clinicalhttp.Deps{
		Commands: clinicalSvc, Queries: clinicalQueries, Pets: clinicalPets,
		Documents: clinicalinfra.NewDocuments(pool.Pool), Uploads: clinicalinfra.NewUploads(pool.Pool),
		Freshness: clinicalinfra.NewFreshness(pool.Pool), Auditor: clinicalinfra.NewReadAuditor(pool.Pool),
		Idem: idemStore, Objects: objects, Logger: logger,
		CardDeps: clinicalhttp.CardDeps{
			Card: cardSvc, CardQueries: cardQueries, Catalog: cardQueries,
			CatalogAdmin:  clinicalinfra.NewCatalogAdmin(pool.Pool),
			CardDocs:      clinicalinfra.NewDocuments(pool.Pool),
			Shares:        clinicalapp.NewShareService(clinicalinfra.NewShareStore(pool.Pool), cardQueries, cardQueries, nil, nil),
			PublicBaseURL: cfg.PublicBaseURL,
		},
	})

	// Scheduling (F2 M2.3, ADR-011).
	schedulingQueries := schedulinginfra.NewQueries(pool.Pool)
	schedulingPets := schedulinginfra.NewPetAccess(pool.Pool)
	schedulingMembers := schedulinginfra.NewMembership(pool.Pool)
	schedulingSvc := schedulingapp.NewService(schedulingapp.Options{
		Appointments: schedulinginfra.NewAppointmentRepository(store, registry, nil, nil),
		Agendas:      schedulinginfra.NewAgendaRepository(store, registry, nil, nil),
		AgendaReader: schedulingQueries,
		Queries:      schedulingQueries,
		Reservations: schedulinginfra.NewReservations(pool.Pool),
		UoW:          unit,
		Projects:     schedulinginfra.NewSyncProjector(runner),
		Pets:         schedulingPets,
		Membership:   schedulingMembers,
		Logger:       logger,
	})
	schedulingSvc.RegisterCommands(commands)
	schedulinginfra.RegisterSyncProjectors(runner, registry)
	schedulingHandlers := schedulinghttp.NewSchedulingHandlers(schedulinghttp.Deps{
		Service: schedulingSvc, Agendas: schedulingQueries, Pets: schedulingPets,
		Membership: schedulingMembers, Idem: idemStore, Logger: logger,
	})

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
	webhookSecrets := map[string][]byte{}
	if cfg.Notifications.EmailWebhookSecret != "" {
		webhookSecrets["generic"] = []byte(cfg.Notifications.EmailWebhookSecret)
	}
	pushSvc := notificationsapp.NewPushService(
		notificationsinfra.NewPushStore(pool.Pool),
		notificationsapp.VAPIDConfig{
			PublicKey:  os.Getenv("VAPID_PUBLIC_KEY"),
			PrivateKey: os.Getenv("VAPID_PRIVATE_KEY"),
			Subject:    "mailto:soporte@petpassport.app",
		},
		nil,
	)
	notificationHandlers := notificationshttp.NewNotificationHandlers(
		notificationsapp.NewPreferences(notificationsinfra.NewPrefStore(pool.Pool), []byte(cfg.Notifications.UnsubscribeSecret), nil),
		notificationsinfra.NewSuppressions(pool.Pool), webhookSecrets, logger).WithPush(pushSvc)
	// Search (F3 M3.1).
	osClient, err := searchapp.NewClient(searchapp.OpenSearchConfig{
		Addresses: cfg.Search.Addresses,
		Username:  cfg.Search.Username,
		Password:  cfg.Search.Password,
	})
	if err != nil {
		logger.Warn("opensearch client init failed, search will use fallback", "error", err)
	}
	freshnessProvider := searchinfra.NewDBFreshnessProvider(pool.Pool)
	pgFallback := searchinfra.NewPGFallback(pool.Pool)
	petAccessLoader := searchinfra.NewCachedPetAccess(pool.Pool, distCache)
	var photoSigner func(key string) string
	if objects != nil {
		photoSigner = func(key string) string {
			u, err := objects.PresignGet(context.Background(), key, 15*time.Minute)
			if err != nil {
				return ""
			}
			return u
		}
	}
	searchQS := searchapp.NewQueryService(osClient, petAccessLoader, freshnessProvider, photoSigner)
	searchCB := searchapp.NewCircuitBreaker(5, 30*time.Second, 60*time.Second)
	resilientSearch := searchapp.NewResilientSearchService(searchQS, pgFallback, searchCB)
	searchAdmin := searchapp.NewAdminService(osClient, searchinfra.NewPostgresSearchStore(pool.Pool), freshnessProvider, logger)
	var abuseDetector *searchapp.AbuseDetector
	if redisClient != nil {
		abuseDetector = searchapp.NewAbuseDetector(redisClient.Raw(), logger)
	}
	cohortStore := searchinfra.NewPGCohortStore(pool.Pool)
	cohortSvc := searchapp.NewCohortService(cohortStore, osClient)
	platformStatsRepo := searchinfra.NewPGPlatformStatsRepository(pool.Pool)
	platformStatsSvc := searchapp.NewPlatformStatsService(platformStatsRepo)
	searchHandler := searchhttp.NewSearchHandler(resilientSearch, searchAdmin, abuseDetector, distCache).
		WithCohorts(cohortSvc).
		WithStats(platformStatsSvc)
	medHandler := medicationshttp.NewMedicationHandler(medSvc, resilientSearch)

	directoryStore := orginfra.NewPGDirectoryStore(pool.Pool)
	directorySvc := orgapp.NewDirectoryService(directoryStore)
	orgHandlers := orghttp.NewOrgHandlers(orgSvc, idemStore, membershipCache, identityHandlers, logger).
		WithIPHashSalt(cfg.IPHashSalt).
		WithDirectory(directorySvc)

	petHandlers := pethttp.NewPetHandlers(petSvc, petQueries, idemStore, objects, logger).WithGuardian(guardianAdapter)

	// Professional Credentials (F4 M4.0).
	credStore := identityinfra.NewPostgresCredentialStore(pool)
	credSvc := identityapp.NewCredentialService(credStore, logger)
	credHandlers := identityhttp.NewCredentialHandlers(credSvc, identityHandlers)

	// Prescriptions (F4 M4.0).
	rxRepo := prescriptioninfra.NewRepository(store, registry)
	rxQueries := prescriptioninfra.NewQueries(pool)
	prescriptioninfra.RegisterSyncProjectors(runner, registry)
	typstRenderer := pdfrender.NewTypst(cfg.TypstBin, pdfrender.DefaultConcurrency, pdfrender.DefaultTimeout)
	rxPDFGen := prescriptioninfra.NewPrescriptionPDFGenerator(typstRenderer, objects, cfg.PublicBaseURL)
	rxSvc := prescriptionapp.NewService(prescriptionapp.Options{
		Repo:          rxRepo,
		UoW:           unit,
		Projects:      prescriptioninfra.NewSyncProjector(runner),
		Queries:       rxQueries,
		Credentials:   prescriptioninfra.NewPostgresCredentialCheck(pool.Pool),
		Rules:         prescriptioninfra.NewPostgresRegulatoryRules(pool.Pool),
		Catalogs:      prescriptioninfra.NewPostgresCatalogEquivalence(pool.Pool),
		PlanReader:    prescriptioninfra.NewPostgresEncounterPlanReader(pool.Pool),
		AccessChecker: prescriptioninfra.NewPostgresPetAccessChecker(pool.Pool),
		PetReader:     prescriptioninfra.NewPostgresPetReader(pool.Pool),
		OrgReader:     prescriptioninfra.NewPostgresOrgReader(pool.Pool),
		UserReader:    prescriptioninfra.NewPostgresUserReader(pool.Pool),
		PDFGen:        rxPDFGen,
		Logger:        logger,
	})
	prescriptionHandlers := prescriptionhttp.NewPrescriptionHandlers(rxSvc, objects).
		WithEncounterReader(prescriptioninfra.NewPostgresEncounterAccessReader(pool.Pool)).
		WithPetReader(prescriptioninfra.NewPostgresPetReader(pool.Pool))
	if queueClient != nil {
		prescriptionHandlers = prescriptionHandlers.WithEnqueuer(queueClient)
	}

	// Pharmacy Orders (F4 M4.2).
	pharmacyQRRepo := pharmacyinfra.NewQuoteRequestRepository(store, registry)
	pharmacyOrderRepo := pharmacyinfra.NewOrderRepository(store, registry)
	pharmacyQueries := pharmacyinfra.NewQueries(pool.Pool)
	pharmacyinfra.RegisterSyncProjectors(runner, registry)
	pharmacyGateway := pharmacyinfra.NewPortalPartnerGateway(pool.Pool)
	pharmacyDirectory := pharmacyinfra.NewPostgresPartnerDirectory(pool.Pool)
	pharmacyQuotaChecker := pharmacyinfra.NewPostgresQuotaChecker(pool.Pool)
	pharmacyEquivalence := pharmacyinfra.NewPostgresCatalogEquivalence(pool.Pool)
	pharmacyDispenser := prescriptionDispenserAdapter{rxSvc: rxSvc}
	pharmacyPetReader := postgresPetReader{pool: pool.Pool}

	pharmacySvc := pharmacyapp.NewService(pharmacyapp.Options{
		QuoteRequests:         pharmacyQRRepo,
		Orders:                pharmacyOrderRepo,
		UoW:                   unit,
		Projects:              pharmacyinfra.NewSyncProjector(runner),
		Gateway:               pharmacyGateway,
		Prescriptions:         pharmacyinfra.NewPostgresPrescriptionReader(pool.Pool),
		PrescriptionDispenser: pharmacyDispenser,
		Catalogs:              pharmacyEquivalence,
		Directory:             pharmacyDirectory,
		QuotaChecker:          pharmacyQuotaChecker,
		PetReader:             pharmacyPetReader,
		UserReader:            postgresUserReader{pool: pool.Pool},
		Logger:                logger,
	})
	pharmacyHandlers := pharmacyhttp.NewPharmacyHandlers(pharmacySvc, pharmacyQueries, logger)

	servers := httpserver.New(cfg.HTTPAddr, cfg.InternalAddr, httpserver.Options{
		Logger: logger, Registry: tel.Registry, Health: health,
		CORSOrigins: cfg.CORSOrigins, Dev: cfg.IsDev(),
		Handlers: apiHandlers{
			Handlers:             identityHandlers,
			AdminHandlers:        identityhttp.NewAdminHandlers(adminSvc, identityHandlers),
			CredentialHandlers:   credHandlers,
			PetHandlers:          petHandlers,
			OrgHandlers:          orgHandlers,
			StatsHandlers:        orghttp.NewStatsHandlers(orginfra.NewStatsQueries(pool.Pool), logger),
			ClinicalHandlers:     clinicalHandlers,
			SchedulingHandlers:   schedulingHandlers,
			NotificationHandlers: notificationHandlers,
			PrescriptionHandlers: prescriptionHandlers,
			PharmacyHandlers:     pharmacyHandlers,
		},
		Guard: guard.Middleware,
		Mount: func(r chi.Router) {
			searchHandler.MountRoutes(r)
			medHandler.MountRoutes(r)
			orgHandlers.MountDirectoryRoutes(r)
			petHandlers.MountProvisionalRoutes(r)
			notificationHandlers.MountPushRoutes(r)
		},
		Maintenance: maintenance.NewGate(pool.Pool, maintenance.DefaultTTL).Middleware,
		RateLimit:   rateLimitMW,
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
// arrancar en el entorno dev); después falla y el proceso no arranca (IAM-007 §1).
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

type prescriptionDispenserAdapter struct {
	rxSvc *prescriptionapp.Service
}

func (a prescriptionDispenserAdapter) RecordDispensation(ctx context.Context, prescriptionID uuid.UUID, orderID uuid.UUID, pharmacyOrgID uuid.UUID, lines []pharmacyordersdomain.DispensationLine) error {
	rxLines := make([]prescriptiondomain.DispensationLine, 0, len(lines))
	for _, l := range lines {
		rxLines = append(rxLines, prescriptiondomain.DispensationLine{
			ItemID: l.ItemID,
			Qty:    l.Qty,
		})
	}
	cmd := prescriptionapp.RecordDispensationCmd{
		PrescriptionID:       prescriptionID,
		PartnerIntegrationID: &orderID,
		PharmacyName:         pharmacyOrgID.String(),
		Lines:                rxLines,
	}
	return a.rxSvc.RecordDispensation(ctx, cmd, pharmacyOrgID)
}

type postgresPetReader struct {
	pool *pgxpool.Pool
}

func (r postgresPetReader) GetAnimalSnapshot(ctx context.Context, petID uuid.UUID) (pharmacyordersdomain.AnimalSnapshot, error) {
	var species string
	var weightKg *float64
	var dob *time.Time
	err := r.pool.QueryRow(ctx, "SELECT species, weight_kg, date_of_birth FROM read_petregistry.pets WHERE id = $1", petID).Scan(&species, &weightKg, &dob)
	if err != nil {
		return pharmacyordersdomain.AnimalSnapshot{Species: "canine"}, nil
	}
	var ageMonths *int
	if dob != nil {
		months := int(time.Since(*dob).Hours() / (24 * 30))
		ageMonths = &months
	}
	return pharmacyordersdomain.AnimalSnapshot{
		Species:   species,
		WeightKg:  weightKg,
		AgeMonths: ageMonths,
	}, nil
}

type postgresUserReader struct {
	pool *pgxpool.Pool
}

func (r postgresUserReader) GetUserName(ctx context.Context, userID uuid.UUID) (string, error) {
	var name string
	err := r.pool.QueryRow(ctx, "SELECT COALESCE(display_name, '') FROM read_identity.users WHERE id = $1", userID).Scan(&name)
	return name, err
}

func (r postgresUserReader) GetUserEmail(ctx context.Context, userID uuid.UUID) (string, error) {
	var email string
	err := r.pool.QueryRow(ctx, "SELECT COALESCE(email, '') FROM read_identity.users WHERE id = $1", userID).Scan(&email)
	return email, err
}
