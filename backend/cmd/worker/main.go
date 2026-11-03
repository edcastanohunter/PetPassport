// Command worker es el proceso de proyecciones asíncronas, outbox, comandos
// programados y colas asynq (PP-F3-ARQ-008, PP-F3-ARQ-009, ADR-009, ADR-014, ADR-015).
// Mismo binario de la imagen backend; réplicas horizontales coordinadas por leases.
//
//	worker [run]                                   runners de larga duración con leases
//	worker leases [--release <name>]               inspección y liberación de leases
//	worker rebuild --projection <name> [--to-position N]
//	worker rebuild --all [--to-position N]          one-shot (ARQ-006)
//	worker rebuild --sync                           primarias F1, con la API en mantenimiento
//	worker maintenance on [--reason R]|off|status   modo mantenimiento de la API
//	worker pause|resume --projection <name>         (ARQ-006/007)
//	worker status                                   checkpoints y estados
//	worker snapshots --drop                         borra es.snapshots (ARQ-004)
//	worker healthcheck                              /readyz (imagen distroless)
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/hibiken/asynq"
	"github.com/jackc/pgx/v5"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/edcastanohunter/petpassport/backend/internal/platform/bus"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/config"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/db"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/eventstore"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/httpserver"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/lease"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/maintenance"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/outbox"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/process"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/projection"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/queue"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/scheduler"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/telemetry"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/worker"
	searchapp "github.com/edcastanohunter/petpassport/backend/internal/search/app"
	searchinfra "github.com/edcastanohunter/petpassport/backend/internal/search/infra"
)

var version = "dev"

func main() {
	cmd := "run"
	args := os.Args[1:]
	if len(args) > 0 && args[0] != "" && args[0][0] != '-' {
		cmd, args = args[0], args[1:]
	}
	if cmd == "healthcheck" {
		os.Exit(healthcheck())
	}
	if err := dispatch(cmd, args); err != nil {
		slog.Error("worker terminated", "command", cmd, "error", err.Error())
		os.Exit(1)
	}
}

func dispatch(cmd string, args []string) error {
	switch cmd {
	case "run":
		return run()
	case "leases":
		return leasesCommand(args)
	case "synth":
		return synthCommand(args)
	case "rebuild", "pause", "resume", "status", "snapshots", "maintenance":
		return oneShot(cmd, args)
	case "export":
		return exportUser(args)
	case "search":
		return searchCommand(args)
	default:
		return fmt.Errorf("subcomando desconocido %q (run|leases|rebuild|pause|resume|status|snapshots|maintenance|export|healthcheck)", cmd)
	}
}

// app agrupa lo que comparten run y los modos one-shot.
type app struct {
	cfg   *config.Worker
	tel   *telemetry.Telemetry
	pool  *db.Pool
	store *eventstore.Store
	subs  *projection.Subscriptions
}

func open(ctx context.Context, service string) (*app, error) {
	cfg, err := config.LoadWorker()
	if err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}
	tel, err := telemetry.Init(ctx, telemetry.Options{
		ServiceName: service, ServiceVersion: version, Env: string(cfg.Env),
		LogLevel: cfg.LogLevel, OTLPEndpoint: cfg.OTLPEndpoint,
	})
	if err != nil {
		return nil, fmt.Errorf("telemetry: %w", err)
	}
	pool, err := db.Open(ctx, cfg.DBURL, cfg.DBMaxConns, tel.Registry)
	if err != nil {
		return nil, err
	}
	registry, err := newRegistry()
	if err != nil {
		pool.Close()
		return nil, err
	}
	store := eventstore.NewStore(registry, pool, nil)
	subs := projection.NewSubscriptions(pool, store, tel.Registry, projection.Options{Logger: tel.Logger})
	registerAsyncProjections(subs)
	return &app{cfg: cfg, tel: tel, pool: pool, store: store, subs: subs}, nil
}

func (a *app) close() {
	a.pool.Close()
	_ = a.tel.Shutdown(context.Background())
}

// run: config -> telemetry -> db -> registry -> leases por runner -> asynq -> runners (ARQ-008, ARQ-009).
func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	a, err := open(ctx, "petpassport-worker")
	if err != nil {
		return err
	}
	defer a.close()
	logger := a.tel.Logger

	stored, err := a.store.StoredVersions(ctx)
	if err != nil {
		return err
	}
	if err := a.store.Registry().VerifyStored(stored); err != nil {
		return err
	}
	logger.Info("event registry verified against es.events", "types", a.store.Registry().Len(), "stored_versions", len(stored))

	reg := a.tel.Registry
	commands := bus.New().Instrument(reg)
	allowed := scheduler.NewAllowedCommands()
	sched := scheduler.New(allowed, nil)
	consumer := process.NewConsumer(a.pool, a.store, reg, process.ConsumerOptions{Logger: logger})
	registerProcessManagers(consumer, allowed, sched)
	registerCommands(commands, a.pool, a.store, logger)
	if err := process.ValidateEmits(consumer.ProcessManagers(), allowed); err != nil {
		return err
	}
	cmdRunner := scheduler.NewRunner(a.pool, allowed, commands, reg, scheduler.RunnerOptions{Logger: logger})
	reg.MustRegister(outbox.PendingGauge(a.pool), outbox.AgeGauge(a.pool))

	// PP-F3-ARQ-009: Leases de worker por runner en vez de lock global de instancia.
	lm := lease.NewManager(a.pool.Pool, a.cfg.WorkerID, logger)
	defer func() { _ = lm.ReleaseAll(context.Background()) }()

	sup := worker.NewSupervisor(logger)
	health := httpserver.NewHealth()
	health.Register("db", a.pool.Ping)
	health.Register("runners", sup.Check)
	srv := internalServer(a.cfg.InternalAddr, health, a.tel)
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("internal server", "error", err.Error())
			stop()
		}
	}()
	defer func() {
		shCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shCtx)
	}()

	health.SetReady(true)
	logger.Info("worker ready with leases", "worker_id", a.cfg.WorkerID, "projections", a.subs.Names())

	makeLeased := func(name string, fn func(context.Context) error) worker.Runner {
		return worker.Runner{
			Name: name,
			Run: func(ctx context.Context) error {
				return runLeaseUnit(ctx, lm, name, fn, logger)
			},
		}
	}

	runners := []worker.Runner{
		makeLeased("projections", a.subs.Run),
		makeLeased("outbox", consumer.Run),
		makeLeased("command-scheduler", cmdRunner.Run),
	}
	clinical, err := clinicalRunners(ctx, a, logger)
	if err != nil {
		return err
	}
	for _, cr := range clinical {
		runners = append(runners, makeLeased(cr.Name, cr.Run))
	}
	notif, err := notificationsRunners(a, logger)
	if err != nil {
		return err
	}
	for _, nr := range notif {
		runners = append(runners, makeLeased(nr.Name, nr.Run))
	}

	// Pool de workers asynq (PP-F3-ARQ-008) si Redis está configurado
	if a.cfg.Redis.Addr != "" {
		asynqSrv := queue.NewServer(queue.ServerConfig{
			RedisAddr:     a.cfg.Redis.Addr,
			RedisPassword: a.cfg.Redis.Password,
			UseTLS:        a.cfg.Redis.UseTLS,
			Concurrency:   a.cfg.AsynqConcurrency,
			Logger:        logger,
		})
		asynqSrv.HandleFunc(queue.TypeNotifyEmail, func(ctx context.Context, t *asynq.Task) error {
			var p queue.NotifyEmailPayload
			if err := json.Unmarshal(t.Payload(), &p); err != nil {
				return err
			}
			logger.InfoContext(ctx, "asynq notify:email received", "delivery_id", p.DeliveryID)
			return nil
		})
		asynqSrv.HandleFunc(queue.TypeDocRender, func(ctx context.Context, t *asynq.Task) error {
			var p queue.DocRenderPayload
			if err := json.Unmarshal(t.Payload(), &p); err != nil {
				return err
			}
			logger.InfoContext(ctx, "asynq doc:render received", "kind", p.Kind, "ref", p.Ref, "version", p.Version)
			return nil
		})

		var searchIndexer *searchapp.Indexer
		var searchAccessProjector *searchapp.AccessProjector
		if osClient, err := searchapp.NewClient(searchapp.OpenSearchConfig{
			Addresses: a.cfg.Search.Addresses,
			Username:  a.cfg.Search.Username,
			Password:  a.cfg.Search.Password,
		}); err == nil {
			cachedPetAccess := searchinfra.NewCachedPetAccess(a.pool.Pool, nil)
			docLoader := searchinfra.NewDocumentLoader(a.pool.Pool, cachedPetAccess)
			searchIndexer = searchapp.NewIndexer(osClient, docLoader, a.pool.Pool, logger)
			searchAccessProjector = searchapp.NewAccessProjector(osClient, searchIndexer, cachedPetAccess, a.pool.Pool, logger)
		} else {
			logger.WarnContext(ctx, "opensearch client init failed in worker", "error", err)
		}

		asynqSrv.HandleFunc(queue.TypeSearchBulk, func(ctx context.Context, t *asynq.Task) error {
			var p queue.SearchBulkPayload
			if err := json.Unmarshal(t.Payload(), &p); err != nil {
				return err
			}
			logger.InfoContext(ctx, "asynq search:bulk received", "index", p.Index, "doc_count", len(p.DocIDs))
			if searchIndexer != nil {
				if p.Index == "pets" {
					return searchIndexer.ProcessBulkPets(ctx, p.DocIDs)
				} else if p.Index == "clinical" {
					return searchIndexer.ProcessBulkClinical(ctx, p.DocIDs)
				}
			}
			return nil
		})

		asynqSrv.HandleFunc(queue.TypeSearchAccess, func(ctx context.Context, t *asynq.Task) error {
			var p queue.SearchAccessPayload
			if err := json.Unmarshal(t.Payload(), &p); err != nil {
				return err
			}
			logger.InfoContext(ctx, "asynq search:access received", "pet_id", p.PetID)
			if searchAccessProjector != nil {
				return searchAccessProjector.UpdatePetAccess(ctx, p.PetID)
			}
			return nil
		})
		runners = append(runners, worker.Runner{
			Name: "asynq-worker-pool",
			Run: func(runCtx context.Context) error {
				for {
					select {
					case <-runCtx.Done():
						return nil
					default:
					}

					// Ejecución resiliente: caídas o reinicios de Redis no deben matar el supervisor completo
					if err := asynqSrv.Run(runCtx); err != nil && !errors.Is(err, context.Canceled) {
						logger.ErrorContext(runCtx, "asynq worker pool stopped with error, retrying in 5s", "error", err)
						select {
						case <-runCtx.Done():
							return nil
						case <-time.After(5 * time.Second):
						}
					} else {
						return nil
					}
				}
			},
		})
	}

	err = sup.Run(ctx, runners...)
	health.SetReady(false)
	logger.Info("worker stopped")
	return err
}

func runLeaseUnit(ctx context.Context, lm *lease.Manager, name string, fn func(context.Context) error, logger *slog.Logger) error {
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		ok, leaseCtx, err := lm.Acquire(ctx, name)
		if err != nil {
			if logger != nil {
				logger.ErrorContext(ctx, "lease acquire error", "runner", name, "error", err)
			}
			select {
			case <-ctx.Done():
				return nil
			case <-ticker.C:
				continue
			}
		}
		if !ok {
			select {
			case <-ctx.Done():
				return nil
			case <-ticker.C:
				continue
			}
		}

		if logger != nil {
			logger.InfoContext(ctx, "running runner with lease", "runner", name, "holder", lm.Holder())
		}
		if err := fn(leaseCtx); err != nil && !errors.Is(err, context.Canceled) {
			if logger != nil {
				logger.ErrorContext(ctx, "runner execution failed", "runner", name, "error", err)
			}
		}
		_ = lm.Release(context.Background(), name)

		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

func leasesCommand(args []string) error {
	fs := flag.NewFlagSet("leases", flag.ContinueOnError)
	releaseName := fs.String("release", "", "fuerza la liberación de un lease")
	if err := fs.Parse(args); err != nil {
		return err
	}
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	a, err := open(ctx, "petpassport-worker-cli")
	if err != nil {
		return err
	}
	defer a.close()

	lm := lease.NewManager(a.pool.Pool, "cli", a.tel.Logger)
	if *releaseName != "" {
		if err := lm.ForceRelease(ctx, *releaseName); err != nil {
			return fmt.Errorf("force release: %w", err)
		}
		fmt.Printf("lease %q liberado con éxito\n", *releaseName)
		return nil
	}

	list, err := lm.List(ctx)
	if err != nil {
		return fmt.Errorf("list leases: %w", err)
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "NAME\tHOLDER\tACQUIRED_AT\tEXPIRES_AT\tACTIVE")
	for _, l := range list {
		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%v\n",
			l.Name, l.Holder, l.AcquiredAt.Format(time.RFC3339), l.ExpiresAt.Format(time.RFC3339), l.Active)
	}
	return w.Flush()
}

func internalServer(addr string, health *httpserver.Health, tel *telemetry.Telemetry) *http.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", health.Liveness)
	mux.HandleFunc("GET /readyz", health.Readiness)
	mux.Handle("GET /metrics", promhttp.HandlerFor(tel.Registry, promhttp.HandlerOpts{}))
	return &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
}

// oneShot ejecuta los modos administrativos. No toman el lock de instancia:
// se coordinan con el worker en marcha por el FOR UPDATE del checkpoint y el
// status (rebuilding/paused), como describe 03 §7.
func oneShot(cmd string, args []string) error {
	action := ""
	if cmd == "maintenance" && len(args) > 0 {
		action, args = args[0], args[1:]
	}
	fs := flag.NewFlagSet(cmd, flag.ContinueOnError)
	name := fs.String("projection", "", "nombre de la proyección asíncrona (p. ej. clinical.medical_timeline)")
	all := fs.Bool("all", false, "rebuild: todas las proyecciones asíncronas")
	syncPrimaries := fs.Bool("sync", false, "rebuild: proyecciones primarias de F1 (API en mantenimiento)")
	reason := fs.String("reason", "mantenimiento programado", "maintenance on: motivo (solo logs)")
	to := fs.Int64("to-position", 0, "rebuild: replay hasta esta global_position (0 = cabeza)")
	drop := fs.Bool("drop", false, "snapshots: borra todos los snapshots")
	if err := fs.Parse(args); err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	a, err := open(ctx, "petpassport-worker-cli")
	if err != nil {
		return err
	}
	defer a.close()
	logger := a.tel.Logger

	switch cmd {
	case "rebuild":
		progress := func(n string, pos, head int64, projected int) {
			logger.Info("rebuild progress", "projection", n, "global_position", pos, "head", head, "batch", projected)
		}
		switch {
		case *syncPrimaries:
			return rebuildSync(ctx, a)
		case *all:
			return a.subs.RebuildAll(ctx, *to, progress)
		case *name != "":
			return a.subs.Rebuild(ctx, *name, *to, progress)
		default:
			return errors.New("rebuild: --projection <name>, --all o --sync")
		}
	case "pause", "resume":
		if *name == "" {
			return fmt.Errorf("%s: --projection <name>", cmd)
		}
		if cmd == "pause" {
			return a.subs.Pause(ctx, *name)
		}
		return a.subs.Resume(ctx, *name)
	case "status":
		if err := a.subs.Ensure(ctx); err != nil {
			return err
		}
		cps, err := projection.Checkpoints(ctx, a.pool)
		if err != nil {
			return err
		}
		head, err := a.store.Head(ctx, a.pool)
		if err != nil {
			return err
		}
		w := tabwriter.NewWriter(os.Stdout, 2, 4, 2, ' ', 0)
		_, _ = fmt.Fprintf(w, "PROJECTION\tMODE\tSTATUS\tPOSITION\tLAG\tLAST_ERROR\n")
		for _, c := range cps {
			_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%d\t%d\t%s\n", c.Name, c.Mode, c.Status, c.LastPosition, head-c.LastPosition, firstLine(c.LastError))
		}
		return w.Flush()
	case "snapshots":
		if !*drop {
			return errors.New("snapshots: --drop")
		}
		var n int64
		err := a.pool.Do(ctx, func(tx pgx.Tx) error {
			var err error
			n, err = eventstore.SnapshotStore{}.DropAll(ctx, tx)
			return err
		})
		logger.Info("snapshots dropped", "count", n)
		return err
	case "maintenance":
		switch action {
		case "on":
			return maintenance.Enable(ctx, a.pool, *reason)
		case "off":
			return maintenance.Disable(ctx, a.pool)
		case "status":
			st, err := maintenance.Get(ctx, a.pool)
			if err != nil {
				return err
			}
			if !st.Active {
				fmt.Println("maintenance: off")
				return nil
			}
			fmt.Printf("maintenance: on since %s (%s)\n", st.Since.Format(time.RFC3339), st.Reason)
			return nil
		default:
			return errors.New("maintenance: on [--reason R] | off | status")
		}
	}
	return nil
}

// rebuildSync reconstruye las proyecciones primarias (ARQ-006, solo para
// desastre, RUNBOOK §15): activa el modo mantenimiento (si no lo estaba),
// espera a que todas las instancias del api lo vean, reconstruye en una sola
// transacción y lo desactiva. Si el rebuild falla, la transacción se revierte
// y los read models quedan como estaban.
func rebuildSync(ctx context.Context, a *app) error {
	logger := a.tel.Logger
	prev, err := maintenance.Get(ctx, a.pool)
	if err != nil {
		return err
	}
	if !prev.Active {
		if err := maintenance.Enable(ctx, a.pool, "rebuild --sync"); err != nil {
			return err
		}
		defer func() {
			if err := maintenance.Disable(context.WithoutCancel(ctx), a.pool); err != nil {
				logger.Error("rebuild --sync: no se pudo desactivar el modo mantenimiento", "error", err.Error())
			}
		}()
		logger.Info("rebuild --sync: API en mantenimiento; esperando a que todas las instancias lo vean")
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2*maintenance.DefaultTTL + time.Second):
		}
	}
	runner := newSyncRunner(a.pool, a.store.Registry(), logger)
	report, err := runner.RebuildSync(ctx, a.pool, a.store, projection.SyncRebuildOptions{Logger: logger})
	if err != nil {
		return err
	}
	logger.Info("rebuild --sync: listo", "projectors", report.Projectors, "head", report.Head,
		"events", report.Events, "duration", report.Duration.String())
	return nil
}

func firstLine(s string) string {
	for i, r := range s {
		if r == '\n' {
			return s[:i]
		}
	}
	return s
}
