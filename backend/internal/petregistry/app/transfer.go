package app

import (
	"context"
	"encoding/hex"
	"errors"
	"net/url"
	"strings"

	"github.com/google/uuid"

	"github.com/edcastanohunter/petpassport/backend/internal/petregistry/domain"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/bus"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/notify"
	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
)

// Transferencia de tutela (PRD-010, DOM-003). El token (32 bytes) solo sale
// en el email; su hash se guarda en read_petregistry.pet_transfers en la
// misma tx (viaja por el contexto hasta el proyector). Si el destinatario no
// tiene cuenta, el email le invita a registrarse con esa dirección y la
// transferencia queda pendiente hasta que caduque (7 días).

func (s *Service) handleInitiateTransfer(ctx context.Context, cmd bus.Command) (bus.Result, error) {
	c := cmd.(domain.InitiateGuardianshipTransfer)
	if c.Now.IsZero() {
		c.Now = s.clock.Now()
	}
	if c.ExpiresAt.IsZero() {
		c.ExpiresAt = c.Now.Add(domain.TransferTTL)
	}
	token := s.tokens.NewToken()
	c.TokenHash = kernel.HashToken(token)
	ctx = WithTransferTokenHash(ctx, c.PetID, c.TokenHash)
	var petName string
	var guardianID uuid.UUID
	stored, err := s.execute(ctx, c.PetID, func(pet *domain.Pet) ([]any, error) {
		if pet.Version() == 0 {
			return nil, &kernel.NotFoundError{Resource: "pet"}
		}
		guardianID = pet.GuardianID()
		email, ok := s.guardian.EmailOf(ctx, guardianID)
		if !ok {
			return nil, errors.New("petregistry: email del tutor no disponible")
		}
		c.CurrentGuardianEmail = email
		petName = pet.Name()
		return pet.HandleCommand(c)
	})
	if err != nil {
		return bus.Result{}, err
	}
	s.enqueueTransferInvite(ctx, c, guardianID, petName, token)
	return bus.Result{AggregateID: c.PetID, Version: versionOf(stored)}, nil
}

func (s *Service) handleCancelTransfer(ctx context.Context, cmd bus.Command) (bus.Result, error) {
	c := cmd.(domain.CancelGuardianshipTransfer)
	if c.Now.IsZero() {
		c.Now = s.clock.Now()
	}
	return s.simple(ctx, c.PetID, c)
}

// handleAcceptTransfer cambia el tutor; el aviso a ambos (transfer_done) lo
// emite el PM de Notifications desde PetGuardianshipTransferred (ARQ-014).
func (s *Service) handleAcceptTransfer(ctx context.Context, cmd bus.Command) (bus.Result, error) {
	c := cmd.(domain.AcceptGuardianshipTransfer)
	if c.Now.IsZero() {
		c.Now = s.clock.Now()
	}
	stored, err := s.execute(ctx, c.PetID, func(pet *domain.Pet) ([]any, error) {
		return pet.HandleCommand(c)
	})
	if err != nil {
		return bus.Result{}, err
	}
	return bus.Result{AggregateID: c.PetID, Version: versionOf(stored)}, nil
}

// AcceptTransferToken resuelve el token y acepta como el principal del
// contexto. Token desconocido, caducado o ya usado → TRANSFER_TOKEN_INVALID
// (404 sin distinguir, API-004).
func (s *Service) AcceptTransferToken(ctx context.Context, token string) (bus.Result, error) {
	cc := commandContext(ctx)
	tr, err := s.queries.TransferByTokenHash(ctx, kernel.HashToken(token), s.clock.Now())
	if err != nil {
		return bus.Result{}, domain.ErrTransferTokenInvalid
	}
	res, err := s.Dispatch(ctx, domain.AcceptGuardianshipTransfer{
		PetID: tr.PetID, ActorEmail: cc.Principal.Email, ActorUserID: cc.Principal.UserID,
	})
	if errors.Is(err, domain.ErrTransferNotPending) {
		return bus.Result{}, domain.ErrTransferTokenInvalid
	}
	return res, err
}

// enqueueTransferInvite encola el correo de la transferencia (regla
// transfer_invite). El token en claro no viaja en el evento (solo su hash);
// se purga de la fila al salir el correo (ADR-026, propuesta). El fallo se
// registra sin PII y no revierte el comando.
func (s *Service) enqueueTransferInvite(ctx context.Context, c domain.InitiateGuardianshipTransfer, guardianID uuid.UUID, petName, token string) {
	if s.notifier == nil || strings.TrimSpace(c.ToEmail) == "" {
		return
	}
	err := s.notifier.Enqueue(context.WithoutCancel(ctx), notify.Notification{
		Rule: "transfer_invite", Template: "transfer_invite", Category: "account",
		To: c.ToEmail, Locale: "es", SubjectID: c.PetID,
		Occurrence: hex.EncodeToString(kernel.HashToken(token)[:8]),
		Payload: map[string]string{
			"from_name": s.guardian.DisplayName(ctx, guardianID), "pet_name": petName,
			"expires_at": c.ExpiresAt.Format("2006-01-02"), "link_path": "/app/transfers/" + url.PathEscape(token),
		},
		Secret: []string{"link_path"},
	})
	if err != nil {
		s.logger.WarnContext(ctx, "no se pudo encolar la transferencia", "pet_id", c.PetID.String(), "error", err.Error())
	}
}
