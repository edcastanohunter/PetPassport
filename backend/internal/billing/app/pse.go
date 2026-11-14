package app

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/edcastanohunter/petpassport/backend/internal/billing/infra"
	"github.com/edcastanohunter/petpassport/backend/internal/billing/providers"
)

// PayerActionRequired es el código con el que se marca fallido un cargo que el
// cliente debe pagar por su cuenta (PSE). Abre la gracia y el dunning como un
// rechazo, pero el aviso pide pagar en vez de anunciar un fallo (PRD-004).
const PayerActionRequired = "PAYER_ACTION_REQUIRED"

// Errores del pago por PSE.
var (
	// ErrChargeNotPayable: el cargo ya está pagado, es de facturación manual o no
	// tiene importe.
	ErrChargeNotPayable = errors.New("billing: el cargo no admite pago")
	// ErrPaymentInProgress: hay un pago del cargo en curso en el proveedor.
	ErrPaymentInProgress = errors.New("billing: hay un pago en curso para este cargo")
	// ErrPSEPayerIncomplete: faltan datos del pagador que el banco exige.
	ErrPSEPayerIncomplete = errors.New("billing: faltan datos del pagador de PSE")
	// ErrPSERedirectUnavailable: el proveedor creó el pago pero no dio la URL
	// del banco; el intento queda pendiente y lo resuelve el webhook.
	ErrPSERedirectUnavailable = errors.New("billing: el proveedor no devolvió la URL del banco")
)

// PSEPaymentCmd son los datos que el banco exige al pagador. Nunca credenciales
// bancarias: el pagador se autentica en su banco (SEC-001).
type PSEPaymentCmd struct {
	ChargeID        uuid.UUID
	BankCode        string
	PayerType       string // person | company
	LegalIDType     string
	LegalID         string
	FullName        string
	Phone           string
	AcceptanceToken string
	PersonalAuth    string
	ReturnURL       string
}

// PSEPayment es el pago abierto: la SPA redirige a RedirectURL.
type PSEPayment struct {
	Status      providers.ChargeStatus
	RedirectURL string
}

// StartPSEPayment abre el pago PSE de un cargo pendiente o fallido
// (PP-F5-PRD-004). Sirve para cuentas con PSE como método y para pagar a mano
// un cargo con tarjeta rechazada. Como CollectCharge, llama al proveedor fuera
// de la transacción y deja el desenlace a settle, el webhook y la conciliación.
func (s *PaymentService) StartPSEPayment(ctx context.Context, cmd PSEPaymentCmd) (PSEPayment, error) {
	now := s.now()
	ch, err := s.ledger.Charge(ctx, cmd.ChargeID)
	if err != nil {
		return PSEPayment{}, err
	}
	if ch == nil {
		return PSEPayment{}, ErrChargeNotReady
	}
	if !collectable(ch.Status) || ch.ManualBilling || ch.Total <= 0 {
		return PSEPayment{}, ErrChargeNotPayable
	}
	acc, err := s.reader.GetAccount(ctx, ch.AccountID)
	if err != nil {
		return PSEPayment{}, err
	}
	if acc == nil {
		return PSEPayment{}, ErrChargeNotReady
	}
	prov, err := s.router.Payment(ctx, acc.Country, false)
	if err != nil {
		return PSEPayment{}, err
	}
	pi, ok := prov.(providers.PayerInitiated)
	if !ok {
		return PSEPayment{}, ErrPSEUnavailable
	}
	req, err := pseRequest(cmd, acc, ch)
	if err != nil {
		return PSEPayment{}, err
	}

	// Un intento pendiente puede haberse resuelto sin que llegara el webhook:
	// se consulta antes de decidir que el pago sigue en curso.
	if open, err := s.ledger.OpenAttempt(ctx, ch.ID); err != nil {
		return PSEPayment{}, err
	} else if open != nil && open.Status == infra.AttemptPending {
		if err := s.pollAttempt(ctx, ch, *open, len(pollDelays)); err != nil {
			return PSEPayment{}, err
		}
		if ch, err = s.ledger.Charge(ctx, cmd.ChargeID); err != nil {
			return PSEPayment{}, err
		}
		if !collectable(ch.Status) {
			return PSEPayment{}, ErrChargeNotPayable
		}
	}
	at, err := s.ledger.BeginAttempt(ctx, ch.ID, prov.Name(), now)
	if err != nil {
		return PSEPayment{}, err
	}
	// Un intento "created" del mismo proveedor nunca obtuvo respuesta: se repite
	// con la misma referencia (el proveedor no crea un segundo pago).
	if at.Status != infra.AttemptCreated || at.Provider != prov.Name() {
		return PSEPayment{}, ErrPaymentInProgress
	}
	req.Attempt = at.Number

	res, err := pi.StartPayment(ctx, req)
	if err != nil {
		s.metrics.charge("error", prov.Name())
		return PSEPayment{}, fmt.Errorf("billing: pago PSE del cargo %s: %w", ch.ID, err)
	}
	if err := s.settle(ctx, ch.ID, at, providers.Payment{
		Ref: res.Ref, Reference: req.Reference(), Status: res.Status,
		Amount: ch.Total, Currency: ch.Currency, ErrorCode: res.ErrorCode,
	}, 0); err != nil {
		return PSEPayment{}, err
	}
	if res.Status == providers.ChargePending && res.RedirectURL == "" {
		return PSEPayment{}, ErrPSERedirectUnavailable
	}
	return PSEPayment{Status: res.Status, RedirectURL: res.RedirectURL}, nil
}

func pseRequest(cmd PSEPaymentCmd, acc *infra.AccountView, ch *infra.ChargeRef) (providers.StartPaymentRequest, error) {
	name := strings.TrimSpace(cmd.FullName)
	if name == "" {
		name = acc.LegalEntity.LegalName
	}
	phone := strings.TrimSpace(cmd.Phone)
	if phone == "" && acc.LegalEntity.Phone != nil {
		phone = strings.TrimSpace(*acc.LegalEntity.Phone)
	}
	req := providers.StartPaymentRequest{
		ChargeID: ch.ID, Amount: ch.Total, Currency: ch.Currency, Email: acc.LegalEntity.Email,
		BankCode: strings.TrimSpace(cmd.BankCode), PayerType: cmd.PayerType,
		LegalIDType: cmd.LegalIDType, LegalID: strings.TrimSpace(cmd.LegalID),
		FullName: name, Phone: phone, Description: "PetPassport suscripcion",
		AcceptanceToken: cmd.AcceptanceToken, PersonalAuth: cmd.PersonalAuth, ReturnURL: cmd.ReturnURL,
	}
	if req.BankCode == "" || req.LegalID == "" || req.LegalIDType == "" || req.Phone == "" || req.AcceptanceToken == "" ||
		(req.PayerType != providers.PayerPerson && req.PayerType != providers.PayerCompany) {
		return providers.StartPaymentRequest{}, ErrPSEPayerIncomplete
	}
	return req, nil
}

// collectPSE es el "cobro" de una cuenta que paga por PSE: no hay nada que
// cobrar sin el cliente. Si hay un pago en curso se consulta; si no, el cargo
// queda fallido con PayerActionRequired, lo que abre la gracia de 14 días, avisa
// al cliente y programa los recordatorios del dunning (cada reintento vuelve aquí).
func (s *PaymentService) collectPSE(ctx context.Context, ch *infra.ChargeRef) error {
	open, err := s.ledger.OpenAttempt(ctx, ch.ID)
	if err != nil {
		return err
	}
	if open != nil && open.Status == infra.AttemptPending {
		return s.pollAttempt(ctx, ch, *open, len(pollDelays))
	}
	return s.uow.Do(ctx, func(ctx context.Context) error { return s.markFailed(ctx, ch, PayerActionRequired) })
}
