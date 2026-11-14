package wompi

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/edcastanohunter/petpassport/backend/internal/billing/providers"
)

func pseRequest() providers.StartPaymentRequest {
	return providers.StartPaymentRequest{
		ChargeID: chargeID, Attempt: 1, Amount: 178500, Currency: "COP", Email: "pagos@example.test",
		BankCode: "1", PayerType: providers.PayerCompany, LegalIDType: "NIT", LegalID: "900123456",
		FullName: "Clínica Demo SAS", Phone: "573001112233",
		Description: "PetPassport suscripción mensual noviembre", AcceptanceToken: "acc-1",
		ReturnURL: "https://app.example.test/org/x/settings/billing?pse=return",
	}
}

func noWait(p *Provider) *Provider {
	p.wait = func(context.Context, time.Duration) error { return nil }
	return p
}

func TestBanks_PP_F5_PRD_004_ListsInstitutionsWithPublicKey(t *testing.T) {
	for name, payload := range map[string]string{
		"arreglo":    `[{"financial_institution_code":"0","financial_institution_name":"Seleccione"},{"financial_institution_code":"1","financial_institution_name":"Banco que aprueba"}]`,
		"envoltorio": `{"data":[{"financial_institution_code":"1","financial_institution_name":"Banco que aprueba"}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			var auth string
			p := server(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet || r.URL.Path != "/pse/financial_institutions" {
					t.Errorf("petición inesperada %s %s", r.Method, r.URL.Path)
				}
				auth = r.Header.Get("Authorization")
				_, _ = w.Write([]byte(payload))
			})
			banks, err := p.Banks(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if auth != "Bearer pub_test_x" {
				t.Fatalf("la lista de bancos usa la llave pública; Authorization = %q", auth)
			}
			if len(banks) != 1 || banks[0] != (providers.Bank{Code: "1", Name: "Banco que aprueba"}) {
				t.Fatalf("bancos = %+v", banks)
			}
		})
	}
}

func TestStartPayment_PP_F5_PRD_004_SendsSignedPSETransactionAndReturnsBankURL(t *testing.T) {
	var got map[string]any
	var auth string
	polls := 0
	p := noWait(server(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/transactions":
			auth = r.Header.Get("Authorization")
			got = body(t, r)
			_, _ = w.Write([]byte(`{"data":{"id":"tx-pse","status":"PENDING","amount_in_cents":17850000,"currency":"COP","payment_method":{"type":"PSE"}}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/transactions/tx-pse":
			polls++
			url := ""
			if polls == 2 {
				url = "https://bank.example.test/pse/123"
			}
			_, _ = w.Write([]byte(`{"data":{"id":"tx-pse","status":"PENDING","payment_method":{"type":"PSE","extra":{"async_payment_url":"` + url + `"}}}}`))
		default:
			t.Errorf("petición inesperada %s %s", r.Method, r.URL.Path)
		}
	}))
	req := pseRequest()
	res, err := p.StartPayment(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != providers.ChargePending || res.Ref != "tx-pse" || res.RedirectURL != "https://bank.example.test/pse/123" || polls != 2 {
		t.Fatalf("resultado = %+v tras %d consultas", res, polls)
	}
	ref := chargeID.String() + "-1"
	if auth != "Bearer prv_test_x" || got["reference"] != ref || got["amount_in_cents"] != float64(17850000) ||
		got["acceptance_token"] != "acc-1" || got["redirect_url"] != req.ReturnURL ||
		got["signature"] != IntegritySignature(ref, 17850000, "COP", "integrity_x") {
		t.Fatalf("transacción enviada = %v (auth %q)", got, auth)
	}
	pm, _ := got["payment_method"].(map[string]any)
	if pm["type"] != "PSE" || pm["user_type"] != float64(1) || pm["user_legal_id_type"] != "NIT" ||
		pm["user_legal_id"] != "900123456" || pm["financial_institution_code"] != "1" {
		t.Fatalf("payment_method = %v", pm)
	}
	if d, _ := pm["payment_description"].(string); len([]rune(d)) > maxDescription {
		t.Fatalf("descripción de %d caracteres", len([]rune(d)))
	}
	cd, _ := got["customer_data"].(map[string]any)
	if cd["full_name"] != "Clínica Demo SAS" || cd["phone_number"] != "573001112233" {
		t.Fatalf("customer_data = %v", cd)
	}
	if _, ok := got["payment_source_id"]; ok {
		t.Fatal("PSE no usa fuente de pago")
	}
}

func TestStartPayment_PP_F5_PRD_004_Outcomes(t *testing.T) {
	cases := map[string]struct {
		status  int
		payload string
		want    providers.StartedPayment
	}{
		"rechazo inmediato del banco": {200, `{"data":{"id":"tx-2","status":"DECLINED"}}`,
			providers.StartedPayment{Ref: "tx-2", Status: providers.ChargeFailed, ErrorCode: "DECLINED"}},
		"referencia ya usada": {422, `{"error":{"type":"INPUT_VALIDATION_ERROR","messages":{"reference":["ya existe"]}}}`,
			providers.StartedPayment{Status: providers.ChargePending}},
		"datos inválidos": {422, `{"error":{"type":"INPUT_VALIDATION_ERROR","messages":{"payment_method":["inválido"]}}}`,
			providers.StartedPayment{Status: providers.ChargeFailed, ErrorCode: "INVALID_REQUEST"}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			p := noWait(server(t, func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.payload))
			}))
			res, err := p.StartPayment(context.Background(), pseRequest())
			if err != nil {
				t.Fatal(err)
			}
			if res != tc.want {
				t.Fatalf("resultado = %+v, quiero %+v", res, tc.want)
			}
		})
	}
}

func TestStartPayment_PP_F5_PRD_004_GivesUpWithoutBankURL(t *testing.T) {
	polls := 0
	p := noWait(server(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			polls++
		}
		_, _ = w.Write([]byte(`{"data":{"id":"tx-3","status":"PENDING"}}`))
	}))
	res, err := p.StartPayment(context.Background(), pseRequest())
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != providers.ChargePending || res.Ref != "tx-3" || res.RedirectURL != "" || polls != redirectPolls {
		t.Fatalf("resultado = %+v tras %d consultas", res, polls)
	}
}

func TestStartPayment_PP_F5_PRD_004_RejectsIncompletePayerWithoutCallingWompi(t *testing.T) {
	p := server(t, func(http.ResponseWriter, *http.Request) { t.Error("no debe llamar a Wompi") })
	req := pseRequest()
	req.PayerType = "otro"
	res, err := p.StartPayment(context.Background(), req)
	if err != nil || res.Status != providers.ChargeFailed || res.ErrorCode != "INVALID_REQUEST" {
		t.Fatalf("resultado = %+v, %v", res, err)
	}
}

func TestSetupPaymentMethod_PP_F5_PRD_004_PSEHasNoWidget(t *testing.T) {
	p := server(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":{"presigned_acceptance":{"acceptance_token":"acc","permalink":"https://x/terms"}}}`))
	})
	cfg, err := p.SetupPaymentMethod(context.Background(), providers.SetupRequest{Kind: "pse"})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ClientConfig["flow"] != "redirect" || cfg.ClientConfig["acceptance_token"] != "acc" {
		t.Fatalf("config = %v", cfg.ClientConfig)
	}
	if _, ok := cfg.ClientConfig["widget_url"]; ok {
		t.Fatal("PSE no abre el widget de tokenización")
	}
	for k := range cfg.ClientConfig {
		if k == "private_key" || k == "integrity_secret" || k == "events_secret" {
			t.Fatalf("secreto expuesto: %s", k)
		}
	}
}
