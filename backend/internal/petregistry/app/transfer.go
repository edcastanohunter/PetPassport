package app

import (
	"context"
	"errors"
	"net/url"
	"strings"

	"github.com/google/uuid"

	"github.com/edcastanohunter/petpassport/backend/internal/petregistry/domain"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/bus"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/mail"
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
	s.sendMail(ctx, c.ToEmail, mail.TemplateTransferInitiated, map[string]string{
		"from_name": s.guardian.DisplayName(ctx, guardianID), "pet_name": petName,
		"token": url.PathEscape(token), "expires_at": c.ExpiresAt.Format("2006-01-02"),
	})
	return bus.Result{AggregateID: c.PetID, Version: versionOf(stored)}, nil
}

func (s *Service) handleCancelTransfer(ctx context.Context, cmd bus.Command) (bus.Result, error) {
	c := cmd.(domain.CancelGuardianshipTransfer)
	if c.Now.IsZero() {
		c.Now = s.clock.Now()
	}
	return s.simple(ctx, c.PetID, c)
}

// handleAcceptTransfer cambia el tutor y avisa por email a ambos (DOM-011).
func (s *Service) handleAcceptTransfer(ctx context.Context, cmd bus.Command) (bus.Result, error) {
	c := cmd.(domain.AcceptGuardianshipTransfer)
	if c.Now.IsZero() {
		c.Now = s.clock.Now()
	}
	var from uuid.UUID
	var petName string
	stored, err := s.execute(ctx, c.PetID, func(pet *domain.Pet) ([]any, error) {
		from = pet.GuardianID()
		petName = pet.Name()
		return pet.HandleCommand(c)
	})
	if err != nil {
		return bus.Result{}, err
	}
	toName := s.guardian.DisplayName(ctx, c.ActorUserID)
	data := map[string]string{"pet_name": petName, "to_name": toName}
	if email, ok := s.guardian.EmailOf(ctx, from); ok {
		s.sendMail(ctx, email, mail.TemplateTransferCompleted, data)
	}
	s.sendMail(ctx, c.ActorEmail, mail.TemplateTransferCompleted, data)
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

// sendMail envía best-effort (DOM-011): el error se registra sin PII y no
// revierte el comando ya confirmado.
func (s *Service) sendMail(ctx context.Context, to, template string, data map[string]string) {
	if s.mailer == nil || strings.TrimSpace(to) == "" {
		return
	}
	subject, body := mail.Render(template, "es", data, s.baseURL)
	if err := s.mailer.Send(context.WithoutCancel(ctx), mail.Message{To: to, Subject: subject, Body: body}); err != nil {
		s.logger.WarnContext(ctx, "mail best-effort falló", "template", template, "error", err.Error())
	}
}
