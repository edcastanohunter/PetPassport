// Command worker es el proceso de proyecciones asíncronas, outbox y comandos
// programados (PP-F2-ARQ-001, ADR-009, docs/phase-2/03 §2). Mismo binario de
// la imagen backend; una réplica por grupo (lock de instancia).
//
//	worker [run]                                   runners de larga duración
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

	"github.com/jackc/pgx/v5"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/edcastanohunter/petpassport/backend/internal/platform/bus"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/config"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/db"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/eventstore"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/httpserver"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/maintenance"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/outbox"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/process"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/projection"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/scheduler"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/telemetry"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/worker"
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
	case "rebuild", "pause", "resume", "status", "snapshots", "maintenance":
		return oneShot(cmd, args)
	default:
		return fmt.Errorf("subcomando desconocido %q (run|rebuild|pause|resume|status|snapshots|maintenance|healthcheck)", cmd)
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

// run: config → telemetry → db → registry (+ verificación de la cadena
// contra es.events) → lock de instancia → runners (03 §2).
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
	if err := process.ValidateEmits(consumer.ProcessManagers(), allowed); err != nil {
		return err
	}
	cmdRunner := scheduler.NewRunner(a.pool, allowed, commands, reg, scheduler.RunnerOptions{Logger: logger})
	reg.MustRegister(outbox.PendingGauge(a.pool), outbox.AgeGauge(a.pool))

	// /healthz y /readyz desde el arranque (readyz falla hasta tener el lock).
	lock := worker.NewInstanceLock(a.pool.Pool, a.cfg.Group, reg, logger)
	sup := worker.NewSupervisor(logger)
	health := httpserver.NewHealth()
	health.Register("db", a.pool.Ping)
	health.Register("instance_lock", func(context.Context) error {
		if !lock.Held() {
			return errors.New("sin lock de instancia")
		}
		return nil
	})
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

	if err := lock.Acquire(ctx, 5*time.Second); err != nil {
		if errors.Is(err, context.Canceled) {
			return nil
		}
		return err
	}
	defer lock.Release()
	health.SetReady(true)
	logger.Info("worker ready", "group", a.cfg.Group, "projections", a.subs.Names())

	err = sup.Run(ctx,
		worker.Runner{Name: "instance-heartbeat", Run: func(ctx context.Context) error { return lock.Heartbeat(ctx, 10*time.Second) }},
		worker.Runner{Name: "projections", Run: a.subs.Run},
		worker.Runner{Name: "outbox", Run: consumer.Run},
		worker.Runner{Name: "command-scheduler", Run: cmdRunner.Run},
	)
	health.SetReady(false)
	logger.Info("worker stopped")
	return err
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
