package app

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/edcastanohunter/petpassport/backend/internal/pharmacyorders/domain"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/eventstore"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/notify"
	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
)

// Options agrupa las dependencias necesarias para inicializar el Service de pharmacyorders.
type Options struct {
	UoW                   UnitOfWork
	Projects              ProjectionRunner
	QuoteRequests         QuoteRequestRepo
	Orders                OrderRepo
	Gateway               domain.PartnerGateway
	Prescriptions         domain.PrescriptionReader
	PrescriptionDispenser PrescriptionDispenser
	Catalogs              domain.CatalogEquivalence
	Directory             domain.PartnerDirectory
	QuotaChecker          domain.QuoteRequestQuotaChecker
	PetReader             PetReader
	UserReader            UserReader
	Notifier              notify.Enqueuer
	Broadcaster           SSEBroadcaster
	PublicBaseURL         string
	Logger                *slog.Logger
	Clock                 func() time.Time
}

// Service es el orquestador de casos de uso y de la saga de cotización y pedidos (ARQ-002, DOM-014).
type Service struct {
	uow           UnitOfWork
	proj          ProjectionRunner
	qrRepo        QuoteRequestRepo
	orderRepo     OrderRepo
	gateway       domain.PartnerGateway
	rxReader      domain.PrescriptionReader
	rxDispenser   PrescriptionDispenser
	catalogs      domain.CatalogEquivalence
	directory     domain.PartnerDirectory
	quotaChecker  domain.QuoteRequestQuotaChecker
	petReader     PetReader
	userReader    UserReader
	notifier      notify.Enqueuer
	broadcaster   SSEBroadcaster
	publicBaseURL string
	logger        *slog.Logger
	clock         func() time.Time
}

// NewService construye el servicio del módulo.
func NewService(opts Options) *Service {
	clk := opts.Clock
	if clk == nil {
		clk = time.Now
	}
	log := opts.Logger
	if log == nil {
		log = slog.Default()
	}
	return &Service{
		uow:           opts.UoW,
		proj:          opts.Projects,
		qrRepo:        opts.QuoteRequests,
		orderRepo:     opts.Orders,
		gateway:       opts.Gateway,
		rxReader:      opts.Prescriptions,
		rxDispenser:   opts.PrescriptionDispenser,
		catalogs:      opts.Catalogs,
		directory:     opts.Directory,
		quotaChecker:  opts.QuotaChecker,
		petReader:     opts.PetReader,
		userReader:    opts.UserReader,
		notifier:      opts.Notifier,
		broadcaster:   opts.Broadcaster,
		publicBaseURL: opts.PublicBaseURL,
		logger:        log,
		clock:         clk,
	}
}

// RequestQuotes inicia una nueva solicitud de cotización multi-farmacia (PP-F4-PRD-008, DOM-008).
func (s *Service) RequestQuotes(ctx context.Context, cmd RequestQuotesCmd) (uuid.UUID, error) {
	now := s.clock()

	rx, err := s.rxReader.GetPrescription(ctx, cmd.PrescriptionID)
	if err != nil {
		return uuid.Nil, err
	}

	var animal domain.AnimalSnapshot
	if s.petReader != nil {
		if a, err := s.petReader.GetAnimalSnapshot(ctx, rx.PetID); err == nil {
			animal = a
		}
	}

	qrID := uuid.New()
	qr := domain.NewQuoteRequest()

	if err := qr.Request(
		ctx,
		qrID,
		rx,
		cmd.GuardianID,
		cmd.Items,
		cmd.Mode,
		cmd.Area,
		cmd.OrganizationIDs,
		cmd.ExpiresInHours,
		s.catalogs,
		s.quotaChecker,
		s.directory,
		now,
	); err != nil {
		return uuid.Nil, err
	}

	// Fanout a farmacias invitadas (ARQ-002, ARQ-003)
	payload := domain.QuoteRequestPayload{
		QuoteRequestID: qrID,
		PrescriptionID: rx.ID,
		Source:         rx.Source,
		Items:          qr.Items,
		Animal:         animal,
		Mode:           qr.Mode,
		Area:           qr.Area,
		ExpiresAt:      qr.ExpiresAt,
	}
	if rx.Source == "issued" {
		url := fmt.Sprintf("%s/verify/prescriptions/%s", s.publicBaseURL, rx.ID)
		payload.PrescriptionURL = &url
	}

	for orgID := range qr.Invited {
		res, err := s.gateway.SendQuoteRequest(ctx, orgID, payload)
		if err != nil {
			qr.MarkInvitationFailed(orgID, err.Error(), now)
		} else {
			qr.MarkInvitationSent(orgID, res.Channel, res.SentAt)
		}
	}

	if err := s.uow.Do(ctx, func(txCtx context.Context) error {
		events, err := s.qrRepo.Save(txCtx, qr)
		if err != nil {
			return err
		}
		if s.proj != nil {
			if err := s.proj.Apply(txCtx, events); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return uuid.Nil, err
	}

	if s.broadcaster != nil {
		s.broadcaster.PublishQuoteRequestEvent(qrID, "QuoteRequested", qr)
	}

	return qrID, nil
}

// RecordQuote registra la oferta remitida por una farmacia (PP-F4-PRD-012, DOM-008).
func (s *Service) RecordQuote(ctx context.Context, cmd RecordQuoteCmd) (uuid.UUID, error) {
	now := s.clock()
	quoteID := uuid.New()

	if err := s.uow.Do(ctx, func(txCtx context.Context) error {
		qr, err := s.qrRepo.Load(txCtx, cmd.QuoteRequestID)
		if err != nil {
			return err
		}

		if err := qr.RecordQuote(
			txCtx,
			quoteID,
			cmd.PharmacyOrgID,
			cmd.Lines,
			cmd.DeliveryFee,
			cmd.ETA,
			cmd.Pickup,
			cmd.ValidUntil,
			cmd.Notes,
			s.catalogs,
			now,
		); err != nil {
			return err
		}

		events, err := s.qrRepo.Save(txCtx, qr)
		if err != nil {
			return err
		}
		if s.proj != nil {
			if err := s.proj.Apply(txCtx, events); err != nil {
				return err
			}
		}

		// Notificación al tutor (PRD-019)
		if s.notifier != nil {
			_ = s.notifier.Enqueue(txCtx, notify.Notification{
				Rule:        "quote_received",
				Template:    "quote_received",
				Category:    "clinical",
				RecipientID: qr.GuardianID,
				Locale:      "es",
				SubjectID:   qr.ID,
				Occurrence:  quoteID.String(),
				Payload: map[string]string{
					"quote_request_id": qr.ID.String(),
					"quote_id":         quoteID.String(),
				},
			})
		}
		return nil
	}); err != nil {
		return uuid.Nil, err
	}

	if s.broadcaster != nil {
		s.broadcaster.PublishQuoteRequestEvent(cmd.QuoteRequestID, "QuoteReceived", map[string]any{
			"quote_id":        quoteID,
			"organization_id": cmd.PharmacyOrgID,
		})
	}

	return quoteID, nil
}

// DeclineQuote registra cuando una farmacia no puede cotizar la solicitud.
func (s *Service) DeclineQuote(ctx context.Context, qrID uuid.UUID, orgID uuid.UUID, reason string) error {
	now := s.clock()

	if err := s.uow.Do(ctx, func(txCtx context.Context) error {
		qr, err := s.qrRepo.Load(txCtx, qrID)
		if err != nil {
			return err
		}

		if err := qr.DeclineQuote(orgID, reason, now); err != nil {
			return err
		}

		events, err := s.qrRepo.Save(txCtx, qr)
		if err != nil {
			return err
		}
		if s.proj != nil {
			return s.proj.Apply(txCtx, events)
		}
		return nil
	}); err != nil {
		return err
	}

	if s.broadcaster != nil {
		s.broadcaster.PublishQuoteRequestEvent(qrID, "QuoteDeclined", map[string]any{
			"organization_id": orgID,
			"reason":          reason,
		})
	}
	return nil
}

// SelectQuote selecciona la cotización deseada y coloca el pedido (PP-F4-PRD-009, PRD-010, DOM-009).
func (s *Service) SelectQuote(ctx context.Context, cmd SelectQuoteCmd) (uuid.UUID, error) {
	now := s.clock()
	orderID := uuid.New()

	var selectedQuote *domain.Quote
	var guardianName string

	if s.userReader != nil {
		name, err := s.userReader.GetUserName(ctx, cmd.GuardianID)
		if err == nil && name != "" {
			guardianName = name
		} else {
			guardianName = "Tutor PetPassport"
		}
	} else {
		guardianName = "Tutor PetPassport"
	}

	if err := s.uow.Do(ctx, func(txCtx context.Context) error {
		qr, err := s.qrRepo.Load(txCtx, cmd.QuoteRequestID)
		if err != nil {
			return err
		}

		if qr.GuardianID != cmd.GuardianID {
			return domain.ErrNotGuardian
		}

		q, err := qr.SelectQuote(cmd.QuoteID, cmd.SubstitutionDecisions, now)
		if err != nil {
			return err
		}
		selectedQuote = q

		// Crear agregado Order (DOM-009)
		order := domain.NewOrder()
		if err := order.Place(
			orderID,
			qr,
			selectedQuote,
			qr.Mode,
			cmd.DeliveryAddress,
			cmd.ContactPhone,
			cmd.SubstitutionDecisions,
			now,
		); err != nil {
			return err
		}

		// Cerrar la saga QuoteRequest
		if err := qr.Close(&orderID, "order placed", now); err != nil {
			return err
		}

		orderEvents, err := s.orderRepo.Save(txCtx, order)
		if err != nil {
			return err
		}
		qrEvents, err := s.qrRepo.Save(txCtx, qr)
		if err != nil {
			return err
		}

		if s.proj != nil {
			if err := s.proj.Apply(txCtx, append(orderEvents, qrEvents...)); err != nil {
				return err
			}
		}

		// Notificación a farmacia de nuevo pedido (PRD-019)
		if s.notifier != nil {
			_ = s.notifier.Enqueue(txCtx, notify.Notification{
				Rule:        "order_placed",
				Template:    "pharmacy_new_order",
				Category:    "clinical",
				RecipientID: selectedQuote.OrgID,
				Locale:      "es",
				SubjectID:   orderID,
				Occurrence:  orderID.String(),
				Payload: map[string]string{
					"order_id": orderID.String(),
				},
			})
		}

		return nil
	}); err != nil {
		return uuid.Nil, err
	}

	// Notificar al partner via Gateway
	rx, _ := s.rxReader.GetPrescription(ctx, selectedQuote.ID)
	order, _ := s.orderRepo.Load(ctx, orderID)
	if order != nil && rx != nil {
		payload := domain.OrderPayload{
			OrderID:         order.ID,
			QuoteRequestID:  order.QuoteRequestID,
			QuoteID:         order.QuoteID,
			PrescriptionID:  order.PrescriptionID,
			Source:          rx.Source,
			GuardianName:    guardianName,
			ContactPhone:    order.ContactPhone,
			DeliveryAddress: order.DeliveryAddress,
			Mode:            order.Mode,
			Lines:           order.Lines,
			Totals:          order.Totals,
			PlacedAt:        now,
		}
		if rx.Source == "issued" {
			u := fmt.Sprintf("%s/verify/prescriptions/%s", s.publicBaseURL, rx.ID)
			payload.PrescriptionURL = &u
		}
		_, _ = s.gateway.NotifyOrder(ctx, order.PharmacyOrgID, payload)
	}

	if s.broadcaster != nil {
		s.broadcaster.PublishQuoteRequestEvent(cmd.QuoteRequestID, "QuoteSelected", map[string]any{
			"quote_id": cmd.QuoteID,
			"order_id": orderID,
		})
		s.broadcaster.PublishOrderEvent(orderID, "OrderPlaced", map[string]any{
			"order_id": orderID,
			"status":   domain.OrderStatusPlaced,
		})
	}

	return orderID, nil
}

// CancelQuoteRequest cancela una cotización abierta por petición del tutor.
func (s *Service) CancelQuoteRequest(ctx context.Context, qrID uuid.UUID, guardianID uuid.UUID, reason string) error {
	now := s.clock()

	if err := s.uow.Do(ctx, func(txCtx context.Context) error {
		qr, err := s.qrRepo.Load(txCtx, qrID)
		if err != nil {
			return err
		}

		if err := qr.Cancel(reason, guardianID, now); err != nil {
			return err
		}

		events, err := s.qrRepo.Save(txCtx, qr)
		if err != nil {
			return err
		}
		if s.proj != nil {
			return s.proj.Apply(txCtx, events)
		}
		return nil
	}); err != nil {
		return err
	}

	if s.broadcaster != nil {
		s.broadcaster.PublishQuoteRequestEvent(qrID, "QuoteRequestCancelled", map[string]any{
			"reason": reason,
		})
	}
	return nil
}

// ConfirmOrder confirma el pedido por la farmacia (PP-F4-PRD-013).
func (s *Service) ConfirmOrder(ctx context.Context, orderID uuid.UUID, orgID uuid.UUID, partnerRef *string, eta *string) error {
	now := s.clock()

	var guardianID uuid.UUID
	if err := s.uow.Do(ctx, func(txCtx context.Context) error {
		order, err := s.orderRepo.Load(txCtx, orderID)
		if err != nil {
			return err
		}
		if order.PharmacyOrgID != orgID {
			return kernel.NewDomainError("FORBIDDEN_ORG", "Organización no autorizada para gestionar este pedido.")
		}

		if err := order.Confirm(partnerRef, eta, now); err != nil {
			return err
		}

		guardianID = order.GuardianID
		events, err := s.orderRepo.Save(txCtx, order)
		if err != nil {
			return err
		}
		if s.proj != nil {
			if err := s.proj.Apply(txCtx, events); err != nil {
				return err
			}
		}

		if s.notifier != nil {
			_ = s.notifier.Enqueue(txCtx, notify.Notification{
				Rule:        "order_confirmed",
				Template:    "order_confirmed",
				Category:    "clinical",
				RecipientID: order.GuardianID,
				Locale:      "es",
				SubjectID:   orderID,
				Occurrence:  orderID.String(),
				Payload: map[string]string{
					"order_id": orderID.String(),
				},
			})
		}
		return nil
	}); err != nil {
		return err
	}

	if s.broadcaster != nil {
		s.broadcaster.PublishOrderEvent(orderID, "OrderConfirmed", map[string]any{
			"status":      domain.OrderStatusConfirmed,
			"guardian_id": guardianID,
		})
	}
	return nil
}

// RejectOrder rechaza un pedido y reabre la saga para permitir otra cotización (Invariante O7, ARQ-002).
func (s *Service) RejectOrder(ctx context.Context, orderID uuid.UUID, orgID uuid.UUID, reason string) error {
	now := s.clock()

	if err := s.uow.Do(ctx, func(txCtx context.Context) error {
		order, err := s.orderRepo.Load(txCtx, orderID)
		if err != nil {
			return err
		}
		if order.PharmacyOrgID != orgID {
			return kernel.NewDomainError("FORBIDDEN_ORG", "Organización no autorizada para gestionar este pedido.")
		}

		if err := order.Reject(reason, now); err != nil {
			return err
		}

		orderEvents, err := s.orderRepo.Save(txCtx, order)
		if err != nil {
			return err
		}

		// Reabrir QuoteRequest si tiene cotizaciones vigentes (DOM-014, O7)
		qr, err := s.qrRepo.Load(txCtx, order.QuoteRequestID)
		var qrEvents []eventstore.StoredEvent
		if err == nil && qr != nil {
			_ = qr.Reopen("Pedido rechazado por la farmacia", now)
			qrEvents, _ = s.qrRepo.Save(txCtx, qr)
		}

		if s.proj != nil {
			allEvents := append(orderEvents, qrEvents...)
			if err := s.proj.Apply(txCtx, allEvents); err != nil {
				return err
			}
		}

		if s.notifier != nil {
			_ = s.notifier.Enqueue(txCtx, notify.Notification{
				Rule:        "order_rejected",
				Template:    "order_rejected",
				Category:    "clinical",
				RecipientID: order.GuardianID,
				Locale:      "es",
				SubjectID:   orderID,
				Occurrence:  orderID.String(),
				Payload: map[string]string{
					"order_id": orderID.String(),
					"reason":   reason,
				},
			})
		}
		return nil
	}); err != nil {
		return err
	}

	if s.broadcaster != nil {
		s.broadcaster.PublishOrderEvent(orderID, "OrderRejected", map[string]any{
			"status": domain.OrderStatusRejected,
			"reason": reason,
		})
	}
	return nil
}

// PrepareOrder transiciona el pedido a preparación.
func (s *Service) PrepareOrder(ctx context.Context, orderID uuid.UUID, orgID uuid.UUID) error {
	now := s.clock()

	if err := s.uow.Do(ctx, func(txCtx context.Context) error {
		order, err := s.orderRepo.Load(txCtx, orderID)
		if err != nil {
			return err
		}
		if order.PharmacyOrgID != orgID {
			return kernel.NewDomainError("FORBIDDEN_ORG", "Organización no autorizada.")
		}

		if err := order.Prepare(now); err != nil {
			return err
		}

		events, err := s.orderRepo.Save(txCtx, order)
		if err != nil {
			return err
		}
		if s.proj != nil {
			return s.proj.Apply(txCtx, events)
		}
		return nil
	}); err != nil {
		return err
	}

	if s.broadcaster != nil {
		s.broadcaster.PublishOrderEvent(orderID, "OrderPreparing", map[string]any{
			"status": domain.OrderStatusPreparing,
		})
	}
	return nil
}

// ReadyOrder marca el pedido como listo para recogida en farmacia.
func (s *Service) ReadyOrder(ctx context.Context, orderID uuid.UUID, orgID uuid.UUID) error {
	now := s.clock()

	if err := s.uow.Do(ctx, func(txCtx context.Context) error {
		order, err := s.orderRepo.Load(txCtx, orderID)
		if err != nil {
			return err
		}
		if order.PharmacyOrgID != orgID {
			return kernel.NewDomainError("FORBIDDEN_ORG", "Organización no autorizada.")
		}

		if err := order.Ready(now); err != nil {
			return err
		}

		events, err := s.orderRepo.Save(txCtx, order)
		if err != nil {
			return err
		}
		if s.proj != nil {
			if err := s.proj.Apply(txCtx, events); err != nil {
				return err
			}
		}

		if s.notifier != nil {
			_ = s.notifier.Enqueue(txCtx, notify.Notification{
				Rule:        "order_ready",
				Template:    "order_ready",
				Category:    "clinical",
				RecipientID: order.GuardianID,
				Locale:      "es",
				SubjectID:   orderID,
				Occurrence:  orderID.String(),
				Payload: map[string]string{
					"order_id": orderID.String(),
				},
			})
		}
		return nil
	}); err != nil {
		return err
	}

	if s.broadcaster != nil {
		s.broadcaster.PublishOrderEvent(orderID, "OrderReady", map[string]any{
			"status": domain.OrderStatusReady,
		})
	}
	return nil
}

// OutForDeliveryOrder marca el pedido como en camino a domicilio.
func (s *Service) OutForDeliveryOrder(ctx context.Context, orderID uuid.UUID, orgID uuid.UUID, eta *string) error {
	now := s.clock()

	if err := s.uow.Do(ctx, func(txCtx context.Context) error {
		order, err := s.orderRepo.Load(txCtx, orderID)
		if err != nil {
			return err
		}
		if order.PharmacyOrgID != orgID {
			return kernel.NewDomainError("FORBIDDEN_ORG", "Organización no autorizada.")
		}

		if err := order.OutForDelivery(eta, now); err != nil {
			return err
		}

		events, err := s.orderRepo.Save(txCtx, order)
		if err != nil {
			return err
		}
		if s.proj != nil {
			if err := s.proj.Apply(txCtx, events); err != nil {
				return err
			}
		}

		if s.notifier != nil {
			_ = s.notifier.Enqueue(txCtx, notify.Notification{
				Rule:        "order_out_for_delivery",
				Template:    "order_out_for_delivery",
				Category:    "clinical",
				RecipientID: order.GuardianID,
				Locale:      "es",
				SubjectID:   orderID,
				Occurrence:  orderID.String(),
				Payload: map[string]string{
					"order_id": orderID.String(),
				},
			})
		}
		return nil
	}); err != nil {
		return err
	}

	if s.broadcaster != nil {
		s.broadcaster.PublishOrderEvent(orderID, "OrderOutForDelivery", map[string]any{
			"status": domain.OrderStatusOutForDelivery,
		})
	}
	return nil
}

// DeliverOrder marca el pedido como entregado y propaga la dispensación a la receta (PP-F4-PRD-010, PRD-013, DOM-014).
func (s *Service) DeliverOrder(ctx context.Context, cmd DeliverOrderCmd) error {
	now := s.clock()

	var (
		rxID        uuid.UUID
		guardianID  uuid.UUID
		pharmacyOrg uuid.UUID
		dispLines   []domain.DispensationLine
	)

	if err := s.uow.Do(ctx, func(txCtx context.Context) error {
		order, err := s.orderRepo.Load(txCtx, cmd.OrderID)
		if err != nil {
			return err
		}
		if order.PharmacyOrgID != cmd.PharmacyOrgID {
			return kernel.NewDomainError("FORBIDDEN_ORG", "Organización no autorizada.")
		}

		dispRecord := domain.DispensationRecord{
			Lines: cmd.Lines,
			At:    now,
		}

		if err := order.Deliver(dispRecord, now); err != nil {
			return err
		}

		rxID = order.PrescriptionID
		guardianID = order.GuardianID
		pharmacyOrg = order.PharmacyOrgID
		dispLines = cmd.Lines

		orderEvents, err := s.orderRepo.Save(txCtx, order)
		if err != nil {
			return err
		}

		// Registrar dispensación contra el agregado Prescription (DOM-014, PRD-010)
		if s.rxDispenser != nil {
			if err := s.rxDispenser.RecordDispensation(txCtx, rxID, order.ID, pharmacyOrg, dispLines); err != nil {
				return fmt.Errorf("recording dispensation on prescription: %w", err)
			}
		}

		if s.proj != nil {
			if err := s.proj.Apply(txCtx, orderEvents); err != nil {
				return err
			}
		}

		if s.notifier != nil {
			_ = s.notifier.Enqueue(txCtx, notify.Notification{
				Rule:        "order_delivered",
				Template:    "order_delivered",
				Category:    "clinical",
				RecipientID: guardianID,
				Locale:      "es",
				SubjectID:   cmd.OrderID,
				Occurrence:  cmd.OrderID.String(),
				Payload: map[string]string{
					"order_id": cmd.OrderID.String(),
				},
			})
		}
		return nil
	}); err != nil {
		return err
	}

	if s.broadcaster != nil {
		s.broadcaster.PublishOrderEvent(cmd.OrderID, "OrderDelivered", map[string]any{
			"status": domain.OrderStatusDelivered,
		})
	}
	return nil
}

// CancelOrder cancela un pedido por petición del tutor o de la farmacia (Invariante O6, DOM-009).
func (s *Service) CancelOrder(ctx context.Context, orderID uuid.UUID, by string, reason string) error {
	now := s.clock()

	if err := s.uow.Do(ctx, func(txCtx context.Context) error {
		order, err := s.orderRepo.Load(txCtx, orderID)
		if err != nil {
			return err
		}

		if err := order.Cancel(by, reason, now); err != nil {
			return err
		}

		orderEvents, err := s.orderRepo.Save(txCtx, order)
		if err != nil {
			return err
		}

		var qrEvents []eventstore.StoredEvent
		if by == "pharmacy" {
			// Farmacia canceló -> reabre saga para que tutor elija otra cotización
			qr, err := s.qrRepo.Load(txCtx, order.QuoteRequestID)
			if err == nil && qr != nil {
				_ = qr.Reopen("Pedido cancelado por la farmacia", now)
				qrEvents, _ = s.qrRepo.Save(txCtx, qr)
			}
		}

		if s.proj != nil {
			allEvents := append(orderEvents, qrEvents...)
			if err := s.proj.Apply(txCtx, allEvents); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return err
	}

	if s.broadcaster != nil {
		s.broadcaster.PublishOrderEvent(orderID, "OrderCancelled", map[string]any{
			"status": domain.OrderStatusCancelled,
			"by":     by,
			"reason": reason,
		})
	}
	return nil
}
