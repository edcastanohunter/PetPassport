package http

import (
	"context"
	"errors"
	"strings"

	"github.com/edcastanohunter/petpassport/backend/api/gen"
	"github.com/edcastanohunter/petpassport/backend/internal/billing/app"
	"github.com/edcastanohunter/petpassport/backend/internal/billing/infra"
	"github.com/edcastanohunter/petpassport/backend/internal/billing/providers"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/authz"
	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
)

// ListPseBanks lista los bancos PSE del proveedor del país de la cuenta (PP-F5-PRD-004).
func (h *BillingHandlers) ListPseBanks(ctx context.Context, req gen.ListPseBanksRequestObject) (gen.ListPseBanksResponseObject, error) {
	if _, err := h.authorize(ctx, authz.BillingManage, h.accountRes(ctx, req.Id)); err != nil {
		return nil, err
	}
	if h.svc == nil {
		return gen.ListPseBanks200JSONResponse{Data: []gen.PseBank{}}, nil
	}
	banks, err := h.svc.PSEBanks(ctx, req.Id)
	if err != nil {
		return nil, mapPaymentError(err)
	}
	out := make([]gen.PseBank, 0, len(banks))
	for _, b := range banks {
		out = append(out, gen.PseBank{Code: b.Code, Name: b.Name})
	}
	return gen.ListPseBanks200JSONResponse{Data: out}, nil
}

// validReturnPath acepta solo rutas de la propia SPA: ni otro origen ("//x",
// "https://x") ni barras invertidas que algunos navegadores tratan como "/".
func validReturnPath(p string) bool {
	return strings.HasPrefix(p, "/") && !strings.HasPrefix(p, "//") && !strings.ContainsAny(p, "\\\r\n")
}

// StartPsePayment abre el pago PSE de un cargo y devuelve la URL del banco
// (PP-F5-PRD-004). Un cargo inexistente o de otra cuenta no es visible.
func (h *BillingHandlers) StartPsePayment(ctx context.Context, req gen.StartPsePaymentRequestObject) (gen.StartPsePaymentResponseObject, error) {
	var ch *infra.ChargeRef
	if h.ledger != nil {
		var err error
		if ch, err = h.ledger.Charge(ctx, req.Id); err != nil {
			return nil, err
		}
	}
	// Sin cargo, el recurso queda sin cuenta: solo super_admin pasa y recibe 404.
	res := authz.BillingResource{}
	if ch != nil {
		res = h.accountRes(ctx, ch.AccountID)
	}
	if _, err := h.authorize(ctx, authz.BillingManage, res); err != nil {
		return nil, err
	}
	if ch == nil || h.pay == nil {
		return nil, &kernel.NotFoundError{Resource: "Cargo"}
	}
	if req.Body == nil {
		return nil, &kernel.ValidationError{Fields: []kernel.FieldViolation{{Field: "body", Code: "REQUIRED"}}}
	}
	b := req.Body
	if !validReturnPath(b.ReturnPath) {
		return nil, &kernel.ValidationError{Fields: []kernel.FieldViolation{{Field: "return_path", Code: "INVALID"}}}
	}
	cmd := app.PSEPaymentCmd{
		ChargeID: req.Id, BankCode: b.BankCode, PayerType: string(b.PayerType),
		LegalIDType: string(b.LegalIdType), LegalID: b.LegalId, AcceptanceToken: b.AcceptanceToken,
		ReturnURL: h.publicBaseURL + b.ReturnPath,
	}
	if b.FullName != nil {
		cmd.FullName = *b.FullName
	}
	if b.Phone != nil {
		cmd.Phone = *b.Phone
	}
	if b.PersonalAuthToken != nil {
		cmd.PersonalAuth = *b.PersonalAuthToken
	}
	out, err := h.pay.StartPSEPayment(ctx, cmd)
	if err != nil {
		if errors.Is(err, app.ErrPSEPayerIncomplete) {
			// El esquema exige el resto; aquí solo faltan valores en blanco o el
			// teléfono, que es opcional si la cuenta ya tiene uno.
			field := "phone"
			if strings.TrimSpace(b.LegalId) == "" {
				field = "legal_id"
			} else if strings.TrimSpace(b.BankCode) == "" {
				field = "bank_code"
			}
			return nil, &kernel.ValidationError{Fields: []kernel.FieldViolation{{Field: field, Code: "REQUIRED"}}}
		}
		return nil, mapPaymentError(err)
	}
	resp := gen.StartPsePayment200JSONResponse{Status: pseStatus(out.Status)}
	if out.RedirectURL != "" && out.Status == providers.ChargePending {
		resp.RedirectUrl = &out.RedirectURL
	}
	return resp, nil
}

func pseStatus(s providers.ChargeStatus) gen.PsePaymentStatus {
	switch s {
	case providers.ChargePaid:
		return gen.PsePaymentStatusPaid
	case providers.ChargePending:
		return gen.PsePaymentStatusPending
	}
	return gen.PsePaymentStatusFailed
}
