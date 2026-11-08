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

// Nombres de colas según PP-F3-ARQ-008, PP-F4-ARQ-012.
const (
	QueueCritical = "critical"
	QueueDefault  = "default"
	QueueOCR      = "ocr"
	QueueLow      = "low"
)

// Prioridades de colas (6 : 3 : 4 : 1).
var QueuePriorities = map[string]int{
	QueueCritical: 6,
	QueueDefault:  3,
	QueueOCR:      4,
	QueueLow:      1,
}

// Tipos de trabajos asynq (PP-F3-ARQ-008, PP-F4-OCR-001).
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
	TypeOCRPipeline   = "ocr:pipeline"
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

// OCRPipelinePayload es el payload para procesar una receta externa con OCR (PP-F4-OCR-001).
type OCRPipelinePayload struct {
	PrescriptionID uuid.UUID `json:"prescription_id"`
	Attempt        int       `json:"attempt"`
	Country        string    `json:"country"`
	PetSpecies     string    `json:"pet_species"`
	PetName        string    `json:"pet_name"`
	GuardianName   string    `json:"guardian_name"`
}

// EnqueueOCRPipeline encola un trabajo de análisis de receta en la cola 'ocr' (PP-F4-ARQ-012, OCR-001).
func (c *Client) EnqueueOCRPipeline(ctx context.Context, p OCRPipelinePayload) (*asynq.TaskInfo, error) {
	if c == nil || c.client == nil {
		return nil, nil
	}
	payload, err := json.Marshal(p)
	if err != nil {
		return nil, err
	}

	taskID := fmt.Sprintf("ocr:pipeline:%s:%d", p.PrescriptionID, p.Attempt)
	task := asynq.NewTask(TypeOCRPipeline, payload,
		asynq.TaskID(taskID),
		asynq.Queue(QueueOCR),
		asynq.MaxRetry(3),
		asynq.Timeout(120*time.Second),
		asynq.Retention(24*time.Hour),
	)

	info, err := c.client.EnqueueContext(ctx, task)
	if err != nil {
		return nil, err
	}
	metricTasksEnqueued.WithLabelValues(TypeOCRPipeline, QueueOCR).Inc()
	return info, nil
}

// MediaProcessPayload es el payload para procesar una imagen o archivo.
type MediaProcessPayload struct {
	ObjectKey string `json:"object_key"`
	Kind      string `json:"kind"`
}

// SearchBulkPayload es el payload para indexar documentos en lote en OpenSearch.
type SearchBulkPayload struct {
	Index  string   `json:"index"`
	DocIDs []string `json:"doc_ids"`
}

// SearchAccessPayload es el payload para actualizar el acceso denormalizado de una mascota.
type SearchAccessPayload struct {
	PetID uuid.UUID `json:"pet_id"`
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

// EnqueueSearchBulk encola un trabajo de indexación en lote en la cola 'default' (ARQ-001).
func (c *Client) EnqueueSearchBulk(ctx context.Context, index string, docIDs []string) (*asynq.TaskInfo, error) {
	payload, err := json.Marshal(SearchBulkPayload{Index: index, DocIDs: docIDs})
	if err != nil {
		return nil, err
	}

	taskID := fmt.Sprintf("search:bulk:%s:%d", index, time.Now().UnixNano())
	task := asynq.NewTask(TypeSearchBulk, payload,
		asynq.TaskID(taskID),
		asynq.Queue(QueueDefault),
		asynq.MaxRetry(10),
		asynq.Retention(24*time.Hour),
	)

	info, err := c.client.EnqueueContext(ctx, task)
	if err != nil {
		return nil, err
	}
	metricTasksEnqueued.WithLabelValues(TypeSearchBulk, QueueDefault).Inc()
	return info, nil
}

// EnqueueSearchAccess encola un trabajo de actualización de acceso de mascota en la cola 'default' (DOM-004).
func (c *Client) EnqueueSearchAccess(ctx context.Context, petID uuid.UUID) (*asynq.TaskInfo, error) {
	payload, err := json.Marshal(SearchAccessPayload{PetID: petID})
	if err != nil {
		return nil, err
	}

	taskID := fmt.Sprintf("search:access:%s:%d", petID, time.Now().UnixNano())
	task := asynq.NewTask(TypeSearchAccess, payload,
		asynq.TaskID(taskID),
		asynq.Queue(QueueDefault),
		asynq.MaxRetry(10),
		asynq.Retention(24*time.Hour),
	)

	info, err := c.client.EnqueueContext(ctx, task)
	if err != nil {
		return nil, err
	}
	metricTasksEnqueued.WithLabelValues(TypeSearchAccess, QueueDefault).Inc()
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
