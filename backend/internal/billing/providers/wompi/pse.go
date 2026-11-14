package wompi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"
	"unicode/utf8"

	"github.com/edcastanohunter/petpassport/backend/internal/billing/providers"
)

// PSE en Wompi (PP-F5-PRD-004): pago de un solo uso que el pagador completa en
// su banco. No admite fuente de pago reutilizable, así que cada cargo abre su
// propia transacción y el desenlace llega por webhook (transaction.updated),
// igual que un cobro con tarjeta pendiente.
//
// Fuentes: docs.wompi.co (métodos de pago, datos de prueba en sandbox) y la
// referencia OpenAPI publicada en SwaggerHub (waybox/wompi 1.2.0). Lo que
// ninguna de las dos fija está marcado "SIN VERIFICAR" (QA-002, sandbox).

var _ providers.PayerInitiated = (*Provider)(nil)

// redirectPolls es cuántas veces se consulta la transacción esperando la URL
// del banco antes de rendirse; redirectEvery es la pausa entre consultas.
const (
	redirectPolls = 6
	redirectEvery = time.Second
	// maxDescription: la documentación da 30 caracteres en sandbox y 64 en la
	// sección de PSE; se respeta el menor.
	maxDescription = 30
)

type bank struct {
	Code string `json:"financial_institution_code"`
	Name string `json:"financial_institution_name"`
}

// Banks lista las entidades de PSE. Requiere la llave pública.
func (p *Provider) Banks(ctx context.Context) ([]providers.Bank, error) {
	var raw json.RawMessage
	if err := p.call(ctx, http.MethodGet, "/pse/financial_institutions", false,
		map[string]string{"Authorization": "Bearer " + p.cfg.PublicKey}, nil, &raw); err != nil {
		return nil, err
	}
	// La referencia OpenAPI describe un arreglo sin envoltorio; el resto de la
	// API envuelve en "data". SIN VERIFICAR: se aceptan las dos formas.
	var list []bank
	if err := json.Unmarshal(raw, &list); err != nil {
		var env struct {
			Data []bank `json:"data"`
		}
		if err := json.Unmarshal(raw, &env); err != nil {
			return nil, errors.New("wompi: lista de bancos PSE ilegible")
		}
		list = env.Data
	}
	out := make([]providers.Bank, 0, len(list))
	for _, b := range list {
		// Wompi incluye un "0" de cabecera ("A continuación seleccione su banco")
		// en algunas integraciones. SIN VERIFICAR para la API; se descarta.
		if b.Code == "" || b.Code == "0" {
			continue
		}
		out = append(out, providers.Bank{Code: b.Code, Name: b.Name})
	}
	return out, nil
}

func userType(payer string) (int, bool) {
	switch payer {
	case providers.PayerPerson:
		return 0, true
	case providers.PayerCompany:
		return 1, true
	}
	return 0, false
}

func truncate(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n])
}

// StartPayment crea la transacción PSE y espera la URL del banco.
func (p *Provider) StartPayment(ctx context.Context, req providers.StartPaymentRequest) (providers.StartedPayment, error) {
	ut, ok := userType(req.PayerType)
	if !ok || req.BankCode == "" || req.LegalID == "" || req.LegalIDType == "" || req.AcceptanceToken == "" {
		return providers.StartedPayment{Status: providers.ChargeFailed, ErrorCode: "INVALID_REQUEST"}, nil
	}
	amount := cents(req.Amount)
	reference := req.Reference()
	in := map[string]any{
		"acceptance_token": req.AcceptanceToken,
		"amount_in_cents":  amount,
		"currency":         req.Currency,
		"customer_email":   req.Email,
		"reference":        reference,
		"signature":        IntegritySignature(reference, amount, req.Currency, p.cfg.IntegritySecret),
		"payment_method": map[string]any{
			"type":                       "PSE",
			"user_type":                  ut,
			"user_legal_id_type":         req.LegalIDType,
			"user_legal_id":              req.LegalID,
			"financial_institution_code": req.BankCode,
			"payment_description":        truncate(req.Description, maxDescription),
		},
		"customer_data": map[string]any{"full_name": req.FullName, "phone_number": req.Phone},
	}
	if req.ReturnURL != "" {
		in["redirect_url"] = req.ReturnURL
	}
	if req.PersonalAuth != "" {
		in["accept_personal_auth"] = req.PersonalAuth
	}
	var out transactionResponse
	if err := p.call(ctx, http.MethodPost, "/transactions", true, nil, in, &out); err != nil {
		var ae *apiError
		if !errors.As(err, &ae) || ae.Status != http.StatusUnprocessableEntity {
			return providers.StartedPayment{}, err
		}
		if _, dup := ae.Messages["reference"]; dup {
			// El intento ya llegó a Wompi: lo resolverá el webhook o la conciliación.
			return providers.StartedPayment{Status: providers.ChargePending}, nil
		}
		return providers.StartedPayment{Status: providers.ChargeFailed, ErrorCode: "INVALID_REQUEST"}, nil
	}
	tx := out.Data
	for i := 0; ; i++ {
		st, err := status(tx.Status)
		if err != nil {
			return providers.StartedPayment{}, err
		}
		res := providers.StartedPayment{Ref: tx.ID, Status: st, RedirectURL: tx.PaymentMethod.Extra.AsyncPaymentURL}
		if st == providers.ChargeFailed || st == providers.ChargeVoided {
			res.ErrorCode = tx.Status
			return res, nil
		}
		if res.RedirectURL != "" || st == providers.ChargePaid || i >= redirectPolls || tx.ID == "" {
			return res, nil
		}
		// SIN VERIFICAR: la documentación solo describe async_payment_url (tras
		// consultar la transacción) para la transferencia Bancolombia; se asume
		// el mismo campo y la misma espera para PSE.
		if err := p.sleep(ctx, redirectEvery); err != nil {
			return res, nil
		}
		var again transactionResponse
		if err := p.call(ctx, http.MethodGet, "/transactions/"+tx.ID, false, nil, nil, &again); err != nil {
			return res, nil // el pago existe; sin URL, quien llama lo informa
		}
		tx = again.Data
	}
}

func (p *Provider) sleep(ctx context.Context, d time.Duration) error {
	if p.wait != nil {
		return p.wait(ctx, d)
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// pseClientConfig son los valores públicos del flujo PSE: no hay widget de
// tokenización, solo las aceptaciones que el pagador debe aprobar.
func pseClientConfig(publicKey string, m merchantResponse) map[string]string {
	return map[string]string{
		"flow":                    "redirect",
		"public_key":              publicKey,
		"acceptance_token":        m.Data.PresignedAcceptance.AcceptanceToken,
		"acceptance_permalink":    m.Data.PresignedAcceptance.Permalink,
		"personal_auth_token":     m.Data.PresignedPersonalDataAuth.AcceptanceToken,
		"personal_auth_permalink": m.Data.PresignedPersonalDataAuth.Permalink,
	}
}
