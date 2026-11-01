package queue

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/hibiken/asynq"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Nombres de colas según PP-F3-ARQ-008.
const (
	QueueCritical = "critical"
	QueueDefault  = "default"
	QueueLow      = "low"
)

// Prioridades de colas (6 : 3 : 1).
var QueuePriorities = map[string]int{
	QueueCritical: 6,
	QueueDefault:  3,
	QueueLow:      1,
}

// Tipos de trabajos asynq (PP-F3-ARQ-008).
const (
	TypeNotifyEmail   = "notify:email"
	TypeNotifyPush    = "notify:push"
	TypeSearchBulk    = "search:bulk"
	TypeSearchAccess  = "search:access"
	TypeDocRender     = "doc:render"
	TypeMediaProcess  = "media:process"
	TypeMediaScan     = "media:scan"
	TypeExportUser    = "export:user"
	TypeExportCohort  = "export:cohort"
	TypeCatalogImport = "catalog:import"
	TypeStatsRefresh  = "stats:refresh"
)

var (
	metricTasksEnqueued = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "petpassport",
			Subsystem: "asynq",
			Name:      "tasks_enqueued_total",
			Help:      "Total de tareas encoladas en asynq por tipo y cola.",
		},
		[]string{"type", "queue"},
	)
)

// Client envuelve asynq.Client para encolar trabajos con dedup y reintentos (ARQ-008).
type Client struct {
	client *asynq.Client
	logger *slog.Logger
}

// NewClient crea un cliente productor de asynq contra Redis DB 1.
func NewClient(redisAddr, redisPassword string, useTLS bool, logger *slog.Logger) *Client {
	if redisAddr == "" {
		redisAddr = "localhost:6379"
	}
	opt := asynq.RedisClientOpt{
		Addr:         redisAddr,
		Password:     redisPassword,
		DB:           1, // DB 1 para trabajos/colas (ARQ-005, DAT-004)
		DialTimeout:  1 * time.Second,
		ReadTimeout:  1 * time.Second,
		WriteTimeout: 1 * time.Second,
	}
	if useTLS {
		opt.TLSConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	}

	return &Client{
		client: asynq.NewClient(opt),
		logger: logger,
	}
}

// Close cierra el cliente asynq.
func (c *Client) Close() error {
	if c.client != nil {
		return c.client.Close()
	}
	return nil
}

// NotifyEmailPayload es el payload para enviar una notificación por correo.
type NotifyEmailPayload struct {
	DeliveryID uuid.UUID `json:"delivery_id"`
}

// DocRenderPayload es el payload para renderizar un documento PDF.
type DocRenderPayload struct {
	Kind    string `json:"kind"`
	Ref     string `json:"ref"`
	Version int64  `json:"version"`
}

// MediaProcessPayload es el payload para procesar una imagen o archivo.
type MediaProcessPayload struct {
	ObjectKey string `json:"object_key"`
	Kind      string `json:"kind"`
}

// EnqueueNotifyEmail encola un trabajo de envío de correo en la cola 'critical'
// con TaskID determinista notify:email:{delivery_id} para deduplicación.
func (c *Client) EnqueueNotifyEmail(ctx context.Context, deliveryID uuid.UUID) (*asynq.TaskInfo, error) {
	payload, err := json.Marshal(NotifyEmailPayload{DeliveryID: deliveryID})
	if err != nil {
		return nil, err
	}

	taskID := fmt.Sprintf("notify:email:%s", deliveryID)
	task := asynq.NewTask(TypeNotifyEmail, payload,
		asynq.TaskID(taskID),
		asynq.Queue(QueueCritical),
		asynq.MaxRetry(10),
		asynq.Retention(24*time.Hour),
	)

	info, err := c.client.EnqueueContext(ctx, task)
	if err != nil {
		return nil, err
	}
	metricTasksEnqueued.WithLabelValues(TypeNotifyEmail, QueueCritical).Inc()
	return info, nil
}

// EnqueueDocRender encola un trabajo de renderizado de PDF en la cola 'default'
// con TaskID determinista doc:render:{kind}:{ref}:{version} para deduplicación.
func (c *Client) EnqueueDocRender(ctx context.Context, kind, ref string, version int64) (*asynq.TaskInfo, error) {
	payload, err := json.Marshal(DocRenderPayload{Kind: kind, Ref: ref, Version: version})
	if err != nil {
		return nil, err
	}

	taskID := fmt.Sprintf("doc:render:%s:%s:%d", kind, ref, version)
	task := asynq.NewTask(TypeDocRender, payload,
		asynq.TaskID(taskID),
		asynq.Queue(QueueDefault),
		asynq.MaxRetry(10),
		asynq.Retention(24*time.Hour),
	)

	info, err := c.client.EnqueueContext(ctx, task)
	if err != nil {
		return nil, err
	}
	metricTasksEnqueued.WithLabelValues(TypeDocRender, QueueDefault).Inc()
	return info, nil
}

// Enqueue encola una tarea personalizada con opciones.
func (c *Client) Enqueue(ctx context.Context, task *asynq.Task, opts ...asynq.Option) (*asynq.TaskInfo, error) {
	info, err := c.client.EnqueueContext(ctx, task, opts...)
	if err == nil {
		q := task.Type()
		metricTasksEnqueued.WithLabelValues(q, QueueDefault).Inc()
	}
	return info, err
}

// ServerConfig contiene opciones para el consumidor asynq en cmd/worker.
type ServerConfig struct {
	RedisAddr     string
	RedisPassword string
	UseTLS        bool
	Concurrency   int
	Logger        *slog.Logger
}

// Server construye el consumidor de colas asynq.
type Server struct {
	srv *asynq.Server
	mux *asynq.ServeMux
}

// NewServer inicializa el servidor consumidor de asynq con prioridades de colas (critical:6, default:3, low:1).
func NewServer(cfg ServerConfig) *Server {
	if cfg.RedisAddr == "" {
		cfg.RedisAddr = "localhost:6379"
	}
	if cfg.Concurrency <= 0 {
		cfg.Concurrency = 10
	}

	opt := asynq.RedisClientOpt{
		Addr:         cfg.RedisAddr,
		Password:     cfg.RedisPassword,
		DB:           1, // DB 1 para trabajos
		DialTimeout:  1 * time.Second,
		ReadTimeout:  1 * time.Second,
		WriteTimeout: 1 * time.Second,
	}
	if cfg.UseTLS {
		opt.TLSConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	}

	srv := asynq.NewServer(opt, asynq.Config{
		Concurrency:     cfg.Concurrency,
		Queues:          QueuePriorities,
		ShutdownTimeout: 25 * time.Second,
		ErrorHandler: asynq.ErrorHandlerFunc(func(ctx context.Context, task *asynq.Task, err error) {
			if cfg.Logger != nil {
				cfg.Logger.ErrorContext(ctx, "asynq task failed", "type", task.Type(), "error", err)
			}
		}),
	})

	return &Server{
		srv: srv,
		mux: asynq.NewServeMux(),
	}
}

// HandleFunc registra un handler para un tipo de tarea en asynq.
func (s *Server) HandleFunc(pattern string, handler func(context.Context, *asynq.Task) error) {
	s.mux.HandleFunc(pattern, handler)
}

// Run arranca el servidor asynq bloqueando hasta que el contexto se cancele.
func (s *Server) Run(ctx context.Context) error {
	errCh := make(chan error, 1)
	go func() {
		errCh <- s.srv.Run(s.mux)
	}()

	select {
	case <-ctx.Done():
		s.srv.Shutdown()
		return nil
	case err := <-errCh:
		return err
	}
}
