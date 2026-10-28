// Command backup es el respaldo frío de es.events (PP-F1-DAT-009, INF-009):
//
//	backup export-events [-out FICHERO]   exporta es.events completo (JSONL gzip)
//	backup verify-events FICHERO [MANIFIESTO]
//	backup import-events FICHERO [MANIFIESTO]  restaura en un es.events vacío
//
// Sin -out sube <prefijo>/AAAA/AAAA-MM-DDTHHMMSSZ.jsonl.gz y su .manifest.json
// al bucket de respaldos (PP_BACKUP_STORAGE_*), distinto del de media. Lo lanza
// el timer semanal de la VM (infra/deploy/systemd). Lee con un rol que solo
// necesita SELECT sobre es.events (PP_DB_URL). import-events solo se usa en el
// ensayo de restauración, con el rol migrador sobre una BD recién migrada: hace
// únicamente INSERT y se niega si es.events tiene datos.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/edcastanohunter/petpassport/backend/internal/platform/eventstore/export"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/storage"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	slog.SetDefault(logger)
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "uso: backup export-events [-out FICHERO] | verify-events|import-events FICHERO [MANIFIESTO]")
		os.Exit(2)
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	var err error
	switch os.Args[1] {
	case "export-events":
		err = exportEvents(ctx, os.Args[2:])
	case "verify-events":
		err = verifyEvents(os.Args[2:])
	case "import-events":
		err = importEvents(ctx, os.Args[2:])
	default:
		err = fmt.Errorf("subcomando desconocido %q", os.Args[1])
	}
	if err != nil {
		slog.Error("backup failed", "command", os.Args[1], "error", err)
		os.Exit(1)
	}
}

func exportEvents(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("export-events", flag.ContinueOnError)
	out := fs.String("out", "", "fichero local de salida (sin -out: sube al bucket de respaldos)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	url := os.Getenv("PP_DB_URL")
	if url == "" {
		return fmt.Errorf("PP_DB_URL es obligatoria")
	}
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return err
	}
	defer pool.Close()

	file := *out
	if file == "" {
		tmp, err := os.CreateTemp("", "es-events-*.jsonl.gz")
		if err != nil {
			return err
		}
		file = tmp.Name()
		_ = tmp.Close()
		defer func() { _ = os.Remove(file) }()
	}
	m, err := writeExport(ctx, pool, file)
	if err != nil {
		return err
	}
	slog.Info("es.events exported", "events", m.Events, "last_position", m.LastPosition,
		"bytes", m.Bytes, "sha256", m.SHA256)
	if *out != "" {
		return writeManifest(*out+".manifest.json", m)
	}
	return upload(ctx, file, m)
}

func writeExport(ctx context.Context, pool *pgxpool.Pool, file string) (export.Manifest, error) {
	snap, err := export.OpenSnapshot(ctx, pool)
	if err != nil {
		return export.Manifest{}, err
	}
	defer snap.Close(ctx)
	f, err := os.Create(file) // #nosec G304 G703 -- ruta elegida por el operador o temporal propia
	if err != nil {
		return export.Manifest{}, err
	}
	m, err := export.Write(ctx, snap, f, export.DefaultBatch, time.Now)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return export.Manifest{}, err
	}
	// El recuento del mismo snapshot debe coincidir con lo escrito.
	if n := snap.Count(); n != m.Events {
		return export.Manifest{}, fmt.Errorf("recuento %d ≠ exportados %d", n, m.Events)
	}
	return m, nil
}

func writeManifest(file string, m export.Manifest) error {
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(file, append(b, '\n'), 0o600) // #nosec G703 -- ruta elegida por el operador
}

func upload(ctx context.Context, file string, m export.Manifest) error {
	bucket := envOr("PP_BACKUP_STORAGE_BUCKET", "")
	if bucket == "" {
		return fmt.Errorf("PP_BACKUP_STORAGE_BUCKET es obligatoria sin -out")
	}
	store, err := storage.NewMinIO(ctx, storage.MinIOOptions{
		Endpoint:  os.Getenv("PP_BACKUP_STORAGE_ENDPOINT"),
		Bucket:    bucket,
		AccessKey: os.Getenv("PP_BACKUP_STORAGE_ACCESS_KEY"),
		SecretKey: os.Getenv("PP_BACKUP_STORAGE_SECRET_KEY"),
		UseSSL:    envOr("PP_BACKUP_STORAGE_USE_SSL", "true") == "true",
	})
	if err != nil {
		return err
	}
	ts := m.FinishedAt.UTC()
	key := path.Join(envOr("PP_BACKUP_PREFIX", "es-events"), ts.Format("2006"),
		ts.Format("2006-01-02T150405Z")+".jsonl.gz")
	f, err := os.Open(file) // #nosec G304 G703 -- temporal creado por este proceso
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	if err := store.Put(ctx, key, "application/gzip", f, m.Bytes); err != nil {
		return err
	}
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	if err := store.Put(ctx, key+".manifest.json", "application/json", bytes.NewReader(b), int64(len(b))); err != nil {
		return err
	}
	slog.Info("es.events uploaded", "bucket", bucket, "key", key)
	return nil
}

func verifyEvents(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("uso: verify-events FICHERO [MANIFIESTO]")
	}
	got, err := verifyFile(args)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(got)
}

// verifyFile valida el fichero y, si existe, lo contrasta con su manifiesto.
func verifyFile(args []string) (export.Manifest, error) {
	f, err := os.Open(args[0]) // #nosec G304 G703 -- fichero indicado por el operador
	if err != nil {
		return export.Manifest{}, err
	}
	defer func() { _ = f.Close() }()
	got, err := export.Verify(f)
	if err != nil {
		return export.Manifest{}, err
	}
	manifestFile := args[0] + ".manifest.json"
	if len(args) > 1 {
		manifestFile = args[1]
	}
	b, err := os.ReadFile(manifestFile) // #nosec G304 G703 -- fichero del operador
	if os.IsNotExist(err) {
		return got, nil
	}
	if err != nil {
		return export.Manifest{}, err
	}
	var want export.Manifest
	if err := json.Unmarshal(b, &want); err != nil {
		return export.Manifest{}, fmt.Errorf("manifiesto: %w", err)
	}
	if !got.Matches(want) {
		return export.Manifest{}, fmt.Errorf("el fichero no coincide con su manifiesto: %+v vs %+v", got, want)
	}
	return got, nil
}

func importEvents(ctx context.Context, args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("uso: import-events FICHERO [MANIFIESTO]")
	}
	// Primero se valida entero: no se empieza a escribir un fichero corrupto.
	want, err := verifyFile(args)
	if err != nil {
		return err
	}
	url := os.Getenv("PP_DB_URL")
	if url == "" {
		return fmt.Errorf("PP_DB_URL es obligatoria")
	}
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return err
	}
	defer pool.Close()
	f, err := os.Open(args[0]) // #nosec G304 G703 -- fichero indicado por el operador
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	got, err := export.Import(ctx, tx, f)
	if err != nil {
		return err
	}
	if !got.Matches(want) {
		return fmt.Errorf("importado %+v no coincide con %+v", got, want)
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	slog.Info("es.events imported", "events", got.Events, "last_position", got.LastPosition)
	return nil
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
