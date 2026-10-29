// Package app contiene los casos de uso de Clinical Records y los puertos
// que consume de otros contextos (PP-F2-DOM-014, docs/phase-2/02 §6).
package app

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/edcastanohunter/petpassport/backend/internal/clinical/domain"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/eventstore"
)

// AccessLevel es el nivel de acceso de una organización a una mascota.
type AccessLevel string

// Niveles (02 §6): linked = vínculo activo (lectura/escritura clínica);
// custody = sin vínculo pero con encuentros firmados por la organización
// (solo lectura de lo propio, encounter_custody); none = 404.
const (
	AccessNone    AccessLevel = "none"
	AccessLinked  AccessLevel = "linked"
	AccessCustody AccessLevel = "custody"
)

// PetSummary son los datos mínimos de la mascota que Clinical muestra.
type PetSummary struct {
	PetID    uuid.UUID
	Name     string
	Species  string
	Deceased bool
}

// PetAccess es la única puerta de Clinical a Pet Registry (SEC-007: IDOR por
// pet_id). Implementación sobre read_petregistry + read_clinical.encounter_custody.
type PetAccess interface {
	CanOrgAccess(ctx context.Context, petID, orgID uuid.UUID) (AccessLevel, error)
	GuardianOf(ctx context.Context, petID uuid.UUID) (uuid.UUID, error)
	PetSummary(ctx context.Context, petID uuid.UUID) (PetSummary, error)
	// LinkedOrgIDs devuelve las organizaciones con vínculo activo (org activa),
	// para calcular la visibilidad de una lista de consultas.
	LinkedOrgIDs(ctx context.Context, petID uuid.UUID) ([]uuid.UUID, error)
	// CustodyOrgIDs devuelve las organizaciones con custodia de encuentros
	// firmados de la mascota (ADR-010).
	CustodyOrgIDs(ctx context.Context, petID uuid.UUID) ([]uuid.UUID, error)
	// Alerts devuelve las alertas del perfil de la mascota (PRD-003/F2).
	Alerts(ctx context.Context, petID uuid.UUID) ([]string, error)
}

// OrgRole es el rol de un usuario en una organización (Partner Network).
type OrgRole string

// Membership es el puerto hacia Partner Network.
type Membership interface {
	RoleOf(ctx context.Context, userID, orgID uuid.UUID) (role OrgRole, active bool, err error)
	// OrganizationName devuelve el nombre comercial (verificación pública y PDF).
	OrganizationName(ctx context.Context, orgID uuid.UUID) (string, error)
}

// AppointmentAccess es el puerto hacia Scheduling (M2.2). En M2.1 no hay citas
// persistidas: el adaptador devuelve ErrAppointmentNotFound salvo que el
// agregado se abra sin cita.
type AppointmentAccess interface {
	// CheckedIn informa si la cita pertenece a la mascota y está checked_in.
	CheckedIn(ctx context.Context, appointmentID, petID uuid.UUID) (bool, error)
}

// Repository persiste y rehidrata el agregado Encounter (ARQ-004).
type Repository interface {
	// Load rehidrata por replay; agregado vacío (versión 0) si el stream no existe.
	Load(ctx context.Context, id uuid.UUID) (*domain.Encounter, error)
	// Save hace Append de los eventos no confirmados y los devuelve persistidos.
	Save(ctx context.Context, e *domain.Encounter, meta eventstore.Metadata) ([]eventstore.StoredEvent, error)
	// ContentHash calcula el hash de contenido (E8/ARQ-010) sobre los eventos
	// persistidos del stream hasta upToVersion, en orden de versión.
	ContentHash(ctx context.Context, id uuid.UUID, upToVersion int64) (string, error)
}

// TxRunner ejecuta una función dentro de la transacción del UoW.
type TxRunner interface {
	Do(ctx context.Context, fn func(ctx context.Context) error) error
}

// ProjectionRunner aplica los proyectores síncronos en la misma tx.
type ProjectionRunner interface {
	Apply(ctx context.Context, events []eventstore.StoredEvent) error
}

// EncounterFilter es el filtro de la lista por organización (API-001).
type EncounterFilter struct {
	OrganizationID uuid.UUID
	PetID          *uuid.UUID
	Status         string
	ProfessionalID *uuid.UUID
	From, To       *time.Time
	Cursor         Cursor
	Limit          int
}

// Cursor es la paginación por (started_at, id), descendente.
type Cursor struct {
	StartedAt time.Time
	ID        uuid.UUID
}

// EncounterListItem es una fila de las listas de consultas.
type EncounterListItem struct {
	ID             uuid.UUID
	Version        int64
	PetID          uuid.UUID
	PetName        string
	PetSpecies     string
	OrganizationID uuid.UUID
	Type           string
	Status         string
	StartedAt      time.Time
	Reason         string
	AttendingVetID *uuid.UUID
	SignedAt       *time.Time
	Abandoned      bool
	LastActivityAt time.Time
	// Para tarjetas de historial (FE-009).
	AttendingVetName string
	OrganizationName string
	Diagnoses        []string
	AddendaCount     int
}

// AttachmentView es un adjunto proyectado.
type AttachmentView struct {
	ID                uuid.UUID
	ObjectKey         string
	ContentType       string
	SizeBytes         int64
	Description       string
	VisibleToGuardian bool
	AddendumSeq       *int
	CreatedAt         time.Time
}

// PDFView describe el PDF del encuentro (ARQ-010).
type PDFView struct {
	Status    string
	ObjectKey string
	Version   int
}

// EncounterView es la vista completa de un encuentro.
type EncounterView struct {
	ID               uuid.UUID
	Version          int64
	PetID            uuid.UUID
	PetName          string
	PetSpecies       string
	OrganizationID   uuid.UUID
	AppointmentID    *uuid.UUID
	Type             string
	Status           string
	StartedAt        time.Time
	Reason           string
	AttendingVetID   *uuid.UUID
	AttendingVetName string
	Vitals           *domain.Vitals
	Subjective       *string
	Objective        *domain.Objective
	Diagnoses        []domain.Diagnosis
	Plan             domain.Plan
	FollowUp         *domain.FollowUpRecommendation
	Attachments      []AttachmentView
	Signature        *domain.Signature
	Addenda          []domain.Addendum
	PDF              *PDFView
	LastActivityAt   time.Time
	CreatedAt        time.Time
	UpdatedAt        time.Time
	Abandoned        bool
}

// WeightPoint es una medición del historial de peso (PRD-025).
type WeightPoint struct {
	MeasuredAt     time.Time
	WeightKg       float64
	Source         string
	RefID          uuid.UUID
	OrganizationID *uuid.UUID
}

// WeightTrend es la tendencia calculada en la consulta (PRD-025: aviso de
// variación > 10 % en 90 días).
type WeightTrend struct {
	DeltaKg     *float64
	DeltaPct    *float64
	WindowDays  int
	Alert       bool
	AlertReason string
}

// SummaryView es el resumen de la ficha de paciente (API-003).
type SummaryView struct {
	Alerts        []string
	LastEncounter *EncounterListItem
	NextDue       []FollowUpItem
	CurrentWeight *WeightPoint
	OpenFollowUps []FollowUpItem
}

// FollowUpItem es un control recomendado pendiente.
type FollowUpItem struct {
	EncounterID    uuid.UUID
	OrganizationID uuid.UUID
	PetID          uuid.UUID
	DueDate        string
	Reason         string
}

// TimelineFilter filtra la línea de tiempo (API-003).
type TimelineFilter struct {
	PetID          uuid.UUID
	Kind           string
	OrganizationID *uuid.UUID
	From, To       *time.Time
	Cursor         Cursor
	Limit          int
	// Audience filtra por visibilidad: "guardian" (guardian|both), "org"
	// (org|both) o "" (todo, super_admin).
	Audience string
	// OrgIDs limita lo visible para un miembro clínico sin vínculo (custodia:
	// solo lo propio).
	OrgIDs []uuid.UUID
}

// TimelineItem es una entrada de la línea de tiempo.
type TimelineItem struct {
	OccurredAt     time.Time
	Kind           string
	RefID          uuid.UUID
	OrganizationID *uuid.UUID
	Title          string
	Summary        string
	Position       int64
	Visibility     string
}

// EncounterQueries es el lado de consulta (ARQ-009) sobre read_clinical.
type EncounterQueries interface {
	ListByOrg(ctx context.Context, f EncounterFilter) ([]EncounterListItem, *Cursor, error)
	ListByPet(ctx context.Context, petID uuid.UUID, orgIDs []uuid.UUID, onlyPublished bool, cursor Cursor, limit int) ([]EncounterListItem, *Cursor, error)
	Get(ctx context.Context, id uuid.UUID) (EncounterView, error)
	// Attachments devuelve los adjuntos de un encuentro (cuerpo y adendas).
	Attachments(ctx context.Context, encounterID uuid.UUID) ([]AttachmentView, error)
	// WeightHistory y Summary aceptan orgIDs (nil = sin restricción) para la
	// vista de custodia: solo datos de las organizaciones indicadas.
	WeightHistory(ctx context.Context, petID uuid.UUID, orgIDs []uuid.UUID) ([]WeightPoint, *WeightTrend, error)
	Timeline(ctx context.Context, f TimelineFilter) ([]TimelineItem, *Cursor, error)
	Summary(ctx context.Context, petID uuid.UUID, orgIDs []uuid.UUID) (SummaryView, error)
	// Access devuelve la vista mínima para autorizar y detectar 404.
	Access(ctx context.Context, id uuid.UUID) (EncounterAccess, error)
}

// EncounterAccess es la vista mínima de autorización de un encuentro.
type EncounterAccess struct {
	ID             uuid.UUID
	PetID          uuid.UUID
	OrganizationID uuid.UUID
	Status         string
	AttendingVetID *uuid.UUID
	GuardianID     uuid.UUID
}

// VerifyView es el resultado de la verificación pública (API-013).
type VerifyView struct {
	Valid            bool
	SignedAt         time.Time
	StreamVersion    int64
	ContentHash      string
	Organization     uuid.UUID
	OrganizationName string
	PetID            uuid.UUID
}

// ErrAppointmentNotFound: la cita no existe o no es de la mascota (404).
type ErrAppointmentNotFound struct{}

func (ErrAppointmentNotFound) Error() string { return "appointment not found" }

// Nombres estables de las proyecciones asíncronas de Clinical (ARQ-012): los
// usan el worker (checkpoints) y http (X-PP-Freshness).
const (
	ProjectionTimeline = "clinical.medical_timeline"
	ProjectionWeight   = "clinical.weight_history"
	ProjectionCustody  = "clinical.encounter_custody"
)

// Freshness informa el retraso de una proyección asíncrona (API-013).
type Freshness interface {
	LagSeconds(ctx context.Context, projection string) (float64, error)
}

// DocumentJobKindEncounterPDF es el tipo de trabajo del PDF de consulta.
const DocumentJobKindEncounterPDF = "encounter_pdf"

// DocumentJobView es un trabajo de document_jobs (ARQ-010, INF-004).
type DocumentJobView struct {
	ID        uuid.UUID
	Kind      string
	RefID     uuid.UUID
	Version   int
	Status    string // pending | rendering | ready | failed
	ObjectKey string
	UpdatedAt time.Time
}

// Documents es el lado de lectura de document_jobs.
type Documents interface {
	Job(ctx context.Context, id uuid.UUID) (DocumentJobView, error)
	// LatestForRef devuelve el trabajo de mayor versión del recurso.
	LatestForRef(ctx context.Context, kind string, refID uuid.UUID) (DocumentJobView, error)
}

// StagedUpload es un adjunto subido a un encuentro firmado, pendiente de
// incorporarse a una adenda (E9: el cuerpo firmado no admite adjuntos).
type StagedUpload struct {
	Attachment  domain.Attachment
	EncounterID uuid.UUID
	UploadedBy  uuid.UUID
	CreatedAt   time.Time
}

// StagedUploads es el almacén operacional de adjuntos pendientes de adenda
// (no es un read model: no se reconstruye desde eventos).
type StagedUploads interface {
	Stage(ctx context.Context, s StagedUpload) error
	Pending(ctx context.Context, encounterID uuid.UUID) ([]StagedUpload, error)
	Get(ctx context.Context, encounterID, attachmentID uuid.UUID) (StagedUpload, error)
	Delete(ctx context.Context, encounterID uuid.UUID, ids []uuid.UUID) error
}

// ReadAudit es una entrada de auditoría de acceso clínico (SEC-005).
type ReadAudit struct {
	ActorID        uuid.UUID
	ActorRoles     []string
	OrganizationID *uuid.UUID
	PetID          uuid.UUID
	Resource       string // encounter | attachment | record | pdf
	ResourceID     uuid.UUID
	Reason         string
	CorrelationID  string
}

// AccessAuditor registra lecturas clínicas en platform.audit_log.
type AccessAuditor interface {
	RecordRead(ctx context.Context, e ReadAudit) error
}
