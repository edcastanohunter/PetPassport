package app

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"

	"github.com/edcastanohunter/petpassport/backend/internal/petregistry/domain"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/bus"
	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
)

// Solicitud de vínculo por la clínica (PP-F2-PRD-020, API-009): búsqueda con
// resultados mínimos y límite por usuario, comandos de la solicitud y bandeja
// del tutor.

// Límites de la búsqueda (PRD-020).
const (
	LookupLimitPerHour = 20
	LookupMaxResults   = 5
	// lookupFloor es el tiempo mínimo de respuesta: con o sin resultados la
	// búsqueda tarda lo mismo (sin oráculo de existencia por tiempo).
	lookupFloor = 150 * time.Millisecond
)

// ErrLookupRateLimited: más de 20 búsquedas en la última hora.
var ErrLookupRateLimited = kernel.NewDomainError("RATE_LIMITED", "Demasiadas búsquedas. Inténtalo más tarde.")

// PetLookupResult es el resultado mínimo de la búsqueda: sin datos de contacto
// ni identificadores del tutor (evita enumeración).
type PetLookupResult struct {
	PetID          uuid.UUID
	Name           string
	Species        string
	GuardianMasked string
	// GuardianDisplayName lo rellena la consulta; la búsqueda lo sustituye por
	// GuardianMasked antes de devolver nada.
	GuardianDisplayName string
}

// CareLinkRequestView es una solicitud de vínculo en el read model.
type CareLinkRequestView struct {
	ID             uuid.UUID
	PetID          uuid.UUID
	PetName        string
	OrganizationID uuid.UUID
	OrgName        string
	RequestedBy    uuid.UUID
	Message        string
	Status         string
	ExpiresAt      time.Time
	CreatedAt      time.Time
	ResolvedAt     *time.Time
}

// InboxItem es una entrada de la bandeja del tutor.
type InboxItem struct {
	ID             uuid.UUID
	Kind           string // care_link_request | appointment_proposal | follow_up
	RefID          uuid.UUID
	PetID          *uuid.UUID
	PetName        string
	OrganizationID *uuid.UUID
	OrgName        string
	Title          string
	Body           string
	Status         string
	CreatedAt      time.Time
	ExpiresAt      *time.Time
}

// InboxCursor pagina la bandeja (created_at desc, id desc).
type InboxCursor struct {
	CreatedAt time.Time
	ID        uuid.UUID
}

// InboxSource aporta entradas a la bandeja desde otro contexto (citas
// propuestas, controles). Cada módulo la implementa sobre su propio read
// model; se componen en cmd/ (ADR-001).
type InboxSource interface {
	InboxItems(ctx context.Context, guardianID uuid.UUID) ([]InboxItem, error)
}

// CareLinkRequestQueries es el lado de consulta de la solicitud de vínculo.
type CareLinkRequestQueries interface {
	// PetsByMicrochip y PetsByGuardian devuelven como máximo n mascotas activas.
	PetsByMicrochip(ctx context.Context, microchip string, n int) ([]PetLookupResult, error)
	PetsByGuardian(ctx context.Context, guardianID uuid.UUID, n int) ([]PetLookupResult, error)
	GetRequest(ctx context.Context, petID, requestID uuid.UUID) (CareLinkRequestView, error)
	GetOrgRequest(ctx context.Context, orgID, requestID uuid.UUID) (CareLinkRequestView, error)
	ListOrgRequests(ctx context.Context, orgID uuid.UUID, status string, limit int) ([]CareLinkRequestView, error)
	// ListInbox devuelve las entradas propias (care_link_request pendientes).
	ListInbox(ctx context.Context, guardianID uuid.UUID) ([]InboxItem, error)
}

// LookupLimiter cuenta las búsquedas por usuario en la última hora. Allow
// registra el intento y devuelve false si ya superó el límite.
type LookupLimiter interface {
	Allow(ctx context.Context, userID uuid.UUID, limit int, window time.Duration) (bool, error)
}

// LookupPetForRequest busca mascotas por microchip exacto o email del tutor
// (PRD-020). Devuelve a lo sumo 5 resultados mínimos. Los intentos cuentan
// para el límite aunque no haya resultados, y la respuesta nunca tarda menos
// de lookupFloor.
func (s *Service) LookupPetForRequest(ctx context.Context, userID uuid.UUID, microchip, guardianEmail string) ([]PetLookupResult, error) {
	start := s.clock.Now()
	ok, err := s.lookupLimiter.Allow(ctx, userID, LookupLimitPerHour, time.Hour)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrLookupRateLimited
	}
	var out []PetLookupResult
	switch {
	case strings.TrimSpace(microchip) != "":
		if chip, cerr := domain.NewMicrochip(microchip); cerr == nil {
			out, err = s.requestQueries.PetsByMicrochip(ctx, string(chip), LookupMaxResults)
		}
	case strings.TrimSpace(guardianEmail) != "":
		if email, eerr := kernel.NewEmail(guardianEmail); eerr == nil {
			if id, found := s.guardian.FindByEmail(ctx, string(email)); found {
				out, err = s.requestQueries.PetsByGuardian(ctx, id, LookupMaxResults)
			}
		}
	}
	if err != nil {
		return nil, err
	}
	for i := range out {
		out[i].GuardianMasked = MaskName(out[i].GuardianDisplayName)
		out[i].GuardianDisplayName = ""
	}
	if out == nil {
		out = []PetLookupResult{}
	}
	s.pad(ctx, start)
	return out, nil
}

// pad espera hasta completar lookupFloor desde start.
func (s *Service) pad(ctx context.Context, start time.Time) {
	left := lookupFloor - s.clock.Now().Sub(start)
	if left <= 0 {
		return
	}
	t := time.NewTimer(left)
	defer t.Stop()
	select {
	case <-ctx.Done():
	case <-t.C:
	}
}

// MaskName enmascara un nombre: inicial de cada palabra y asteriscos
// («Ana María Pérez» → «A*** M*** P***»).
func MaskName(name string) string {
	words := strings.Fields(name)
	for i, w := range words {
		r := []rune(w)
		words[i] = string(unicode.ToUpper(r[0])) + "***"
	}
	return strings.Join(words, " ")
}

// ListInbox devuelve la bandeja del tutor: sus entradas propias más las de
// otros contextos, de la más reciente a la más antigua.
func (s *Service) ListInbox(ctx context.Context, guardianID uuid.UUID) ([]InboxItem, error) {
	items, err := s.requestQueries.ListInbox(ctx, guardianID)
	if err != nil {
		return nil, err
	}
	for _, src := range s.inboxSources {
		more, err := src.InboxItems(ctx, guardianID)
		if err != nil {
			return nil, err
		}
		items = append(items, more...)
	}
	sortInbox(items)
	return items, nil
}

func sortInbox(items []InboxItem) {
	for i := 1; i < len(items); i++ {
		for j := i; j > 0 && newer(items[j], items[j-1]); j-- {
			items[j], items[j-1] = items[j-1], items[j]
		}
	}
}

func newer(a, b InboxItem) bool {
	if !a.CreatedAt.Equal(b.CreatedAt) {
		return a.CreatedAt.After(b.CreatedAt)
	}
	return a.ID.String() > b.ID.String()
}

// ------------------------------------------------------------- comandos

// handleRequestCareLink exige organización clínica activa (P1, vía puerto).
func (s *Service) handleRequestCareLink(ctx context.Context, cmd bus.Command) (bus.Result, error) {
	c := cmd.(domain.RequestCareLink)
	if s.orgs == nil {
		return bus.Result{}, errors.New("petregistry: OrganizationLookup no configurado")
	}
	if _, err := s.orgs.ActiveClinic(ctx, c.OrganizationID); err != nil {
		if errors.Is(err, ErrOrganizationNotEligible) {
			return bus.Result{}, domain.ErrOrganizationNotEligible
		}
		return bus.Result{}, err
	}
	if c.RequestID == uuid.Nil {
		c.RequestID = s.ids.NewID()
	}
	if c.Now.IsZero() {
		c.Now = s.clock.Now()
	}
	res, err := s.simple(ctx, c.PetID, c)
	if err == nil {
		res.AggregateID = c.RequestID // el llamador necesita el id de la solicitud
	}
	return res, err
}

func (s *Service) handleApproveCareLinkRequest(ctx context.Context, cmd bus.Command) (bus.Result, error) {
	c := cmd.(domain.ApproveCareLinkRequest)
	if c.Now.IsZero() {
		c.Now = s.clock.Now()
	}
	return s.simple(ctx, c.PetID, c)
}

func (s *Service) handleDeclineCareLinkRequest(ctx context.Context, cmd bus.Command) (bus.Result, error) {
	c := cmd.(domain.DeclineCareLinkRequest)
	if c.Now.IsZero() {
		c.Now = s.clock.Now()
	}
	return s.simple(ctx, c.PetID, c)
}

func (s *Service) handleExpireCareLinkRequest(ctx context.Context, cmd bus.Command) (bus.Result, error) {
	c := cmd.(domain.ExpireCareLinkRequest)
	if c.Now.IsZero() {
		c.Now = s.clock.Now()
	}
	return s.simple(ctx, c.PetID, c)
}

func (s *Service) handleUpdatePetAlerts(ctx context.Context, cmd bus.Command) (bus.Result, error) {
	c := cmd.(domain.UpdatePetAlerts)
	return s.simple(ctx, c.PetID, c)
}

// ------------------------------------------------------------- consultas

// GetOrgRequest devuelve una solicitud hecha por la organización.
func (s *Service) GetOrgRequest(ctx context.Context, orgID, requestID uuid.UUID) (CareLinkRequestView, error) {
	return s.requestQueries.GetOrgRequest(ctx, orgID, requestID)
}

// ListOrgRequests lista las solicitudes de la organización (status vacío = todas).
func (s *Service) ListOrgRequests(ctx context.Context, orgID uuid.UUID, status string) ([]CareLinkRequestView, error) {
	return s.requestQueries.ListOrgRequests(ctx, orgID, status, 50)
}
