package wompi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/edcastanohunter/petpassport/backend/internal/billing/providers"
)

var (
	now      = time.Date(2026, 11, 12, 15, 0, 0, 0, time.UTC)
	chargeID = uuid.MustParse("22222222-2222-4222-8222-222222222222")
)

func testConfig(url string) Config {
	return Config{BaseURL: url, PublicKey: "pub_test_x", PrivateKey: "prv_test_x", IntegritySecret: "integrity_x", EventsSecret: "events_x"}
}

// server levanta un Wompi simulado y devuelve el adaptador y las peticiones recibidas.
func server(t *testing.T, handler http.HandlerFunc) *Provider {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return New(testConfig(srv.URL), srv.Client())
}

func body(t *testing.T, r *http.Request) map[string]any {
	t.Helper()
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]any{}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("cuerpo ilegible: %s", raw)
	}
	return out
}

func TestIntegritySignature_PP_F5_SEC_001(t *testing.T) {
	sum := sha256.Sum256([]byte("ref-1" + "17850000" + "COP" + "secreto"))
	if got := IntegritySignature("ref-1", 17850000, "COP", "secreto"); got != hex.EncodeToString(sum[:]) {
		t.Fatalf("firma = %s", got)
	}
}

func TestCharge_PP_F5_ARQ_002_SendsSignedRecurrentTransaction(t *testing.T) {
	var got map[string]any
	var auth string
	p := server(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/transactions" {
			t.Errorf("petición inesperada %s %s", r.Method, r.URL.Path)
		}
		auth = r.Header.Get("Authorization")
		got = body(t, r)
		_, _ = w.Write([]byte(`{"data":{"id":"tx-1","status":"APPROVED","reference":"x","amount_in_cents":17850000,"currency":"COP"}}`))
	})
	req := providers.ChargeRequest{ChargeID: chargeID, Attempt: 2, Amount: 178500, Currency: "COP", Email: "pagos@example.test", Token: "4455"}
	res, err := p.Charge(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != providers.ChargePaid || res.Ref != "tx-1" {
		t.Fatalf("resultado = %+v", res)
	}
	ref := chargeID.String() + "-2"
	if auth != "Bearer prv_test_x" || got["reference"] != ref || got["amount_in_cents"] != float64(17850000) ||
		got["payment_source_id"] != float64(4455) || got["recurrent"] != true ||
		got["signature"] != IntegritySignature(ref, 17850000, "COP", "integrity_x") {
		t.Fatalf("petición = %v (auth %q)", got, auth)
	}
}

func TestCharge_PP_F5_ARQ_002_StatusAndErrorMapping(t *testing.T) {
	cases := []struct {
		name    string
		status  int
		body    string
		want    providers.ChargeStatus
		wantErr error
	}{
		{"declinada", 201, `{"data":{"id":"tx-2","status":"DECLINED"}}`, providers.ChargeFailed, nil},
		{"pendiente", 201, `{"data":{"id":"tx-3","status":"PENDING"}}`, providers.ChargePending, nil},
		{"referencia duplicada", 422, `{"error":{"type":"INPUT_VALIDATION_ERROR","messages":{"reference":["ya fue usada"]}}}`, providers.ChargePending, nil},
		{"petición inválida", 422, `{"error":{"type":"INPUT_VALIDATION_ERROR","messages":{"currency":["inválida"]}}}`, providers.ChargeFailed, nil},
		{"caído", 503, `oops`, "", providers.ErrUnavailable},
		{"limitado", 429, ``, "", providers.ErrUnavailable},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := server(t, func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(c.status)
				_, _ = w.Write([]byte(c.body))
			})
			res, err := p.Charge(context.Background(), providers.ChargeRequest{ChargeID: chargeID, Attempt: 1, Amount: 100, Currency: "COP", Token: "1"})
			if c.wantErr != nil {
				if !errors.Is(err, c.wantErr) {
					t.Fatalf("error = %v, quiero %v", err, c.wantErr)
				}
				return
			}
			if err != nil || res.Status != c.want {
				t.Fatalf("resultado = %+v, err = %v", res, err)
			}
		})
	}
}

func TestCharge_PP_F5_ARQ_002_CredentialErrorIsNotADecline(t *testing.T) {
	p := server(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"type":"INVALID_ACCESS_TOKEN"}}`))
	})
	res, err := p.Charge(context.Background(), providers.ChargeRequest{ChargeID: chargeID, Attempt: 1, Amount: 100, Currency: "COP", Token: "1"})
	if err == nil || errors.Is(err, providers.ErrUnavailable) || res.Status != "" {
		t.Fatalf("un 401 debe ser un error de configuración, no un rechazo: %+v %v", res, err)
	}
	if strings.Contains(err.Error(), "prv_test_x") {
		t.Fatal("el error no debe contener la llave privada")
	}
}

func TestCharge_PP_F5_ARQ_002_NetworkFailureIsUnavailable(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	p := New(testConfig(srv.URL), srv.Client())
	srv.Close()
	_, err := p.Charge(context.Background(), providers.ChargeRequest{ChargeID: chargeID, Attempt: 1, Amount: 100, Currency: "COP", Token: "1"})
	if !errors.Is(err, providers.ErrUnavailable) {
		t.Fatalf("error = %v", err)
	}
}

func TestAttachPaymentMethod_PP_F5_SEC_001_ExchangesTokenForSource(t *testing.T) {
	var got map[string]any
	p := server(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/payment_sources" {
			t.Errorf("ruta %s", r.URL.Path)
		}
		got = body(t, r)
		_, _ = w.Write([]byte(`{"data":{"id":4455,"status":"AVAILABLE","type":"CARD","public_data":{"last_four":"4242","brand":"VISA","exp_month":"06","exp_year":"29"}}}`))
	})
	pm, err := p.AttachPaymentMethod(context.Background(), providers.AttachRequest{
		Email: "pagos@example.test", ProviderToken: "tok_test_1", AcceptanceToken: "acc", PersonalAuth: "auth",
	})
	if err != nil {
		t.Fatal(err)
	}
	if pm.Token != "4455" || pm.Last4 != "4242" || pm.Brand != "VISA" || pm.ExpiresAt == nil ||
		!pm.ExpiresAt.Equal(time.Date(2029, 6, 30, 23, 59, 59, 0, time.UTC)) {
		t.Fatalf("método = %+v", pm)
	}
	if got["type"] != "CARD" || got["token"] != "tok_test_1" || got["acceptance_token"] != "acc" || got["accept_personal_auth"] != "auth" {
		t.Fatalf("petición = %v", got)
	}
	if _, err := p.AttachPaymentMethod(context.Background(), providers.AttachRequest{ProviderToken: "tok"}); !errors.Is(err, providers.ErrPaymentMethodInvalid) {
		t.Fatalf("sin aceptación de términos: %v", err)
	}
}

func TestSetupPaymentMethod_PP_F5_SEC_001_OnlyPublicValues(t *testing.T) {
	p := server(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-merchant-public-key") != "pub_test_x" || r.Header.Get("Authorization") != "" {
			t.Errorf("cabeceras inesperadas: %v", r.Header)
		}
		_, _ = w.Write([]byte(`{"data":{"presigned_acceptance":{"acceptance_token":"acc","permalink":"https://wompi.co/t.pdf"},"presigned_personal_data_auth":{"acceptance_token":"auth","permalink":"https://wompi.co/d.pdf"}}}`))
	})
	cfg, err := p.SetupPaymentMethod(context.Background(), providers.SetupRequest{Kind: "card"})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ClientConfig["public_key"] != "pub_test_x" || cfg.ClientConfig["acceptance_token"] != "acc" || cfg.ClientConfig["personal_auth_token"] != "auth" {
		t.Fatalf("config = %v", cfg.ClientConfig)
	}
	for k, v := range cfg.ClientConfig {
		if strings.Contains(v, "prv_test_x") || strings.Contains(v, "integrity_x") || strings.Contains(v, "events_x") {
			t.Fatalf("la configuración del widget expone un secreto en %s", k)
		}
	}
}

func TestGetPaymentAndRefund_PP_F5_ARQ_002(t *testing.T) {
	var paths []string
	p := server(t, func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.Method+" "+r.URL.Path)
		switch r.URL.Path {
		case "/transactions/tx-9":
			_, _ = w.Write([]byte(`{"data":{"id":"tx-9","status":"VOIDED","reference":"r-1","amount_in_cents":17850000,"currency":"COP"}}`))
		case "/refunds":
			_, _ = w.Write([]byte(`{"data":{"id":77}}`))
		default:
			_, _ = w.Write([]byte(`{"data":{}}`))
		}
	})
	pay, err := p.GetPayment(context.Background(), "tx-9")
	if err != nil || pay.Status != providers.ChargeVoided || pay.Amount != 178500 || pay.Reference != "r-1" {
		t.Fatalf("pago = %+v, err = %v", pay, err)
	}
	if _, err := p.GetPayment(context.Background(), "tx/../x"); err == nil {
		t.Fatal("un id con '/' no debe llegar a la URL")
	}
	ref, err := p.Refund(context.Background(), providers.RefundRequest{PaymentRef: "tx-9", Amount: 178500, Full: true})
	if err != nil || ref != "void:tx-9" {
		t.Fatalf("anulación = %q, %v", ref, err)
	}
	ref, err = p.Refund(context.Background(), providers.RefundRequest{PaymentRef: "tx-9", Amount: 500, Reason: "parcial"})
	if err != nil || ref != "refund:77" {
		t.Fatalf("reembolso parcial = %q, %v", ref, err)
	}
	want := "GET /transactions/tx-9,POST /transactions/tx-9/void,POST /refunds"
	if strings.Join(paths, ",") != want {
		t.Fatalf("peticiones = %v", paths)
	}
}

// signedEvent construye un evento transaction.updated firmado como lo hace Wompi.
func signedEvent(status string, amount int64, ts time.Time, secret string) []byte {
	ref := chargeID.String() + "-1"
	sum := sha256.Sum256([]byte("tx-1" + status + fmt.Sprint(amount) + fmt.Sprint(ts.Unix()) + secret))
	return []byte(fmt.Sprintf(`{"event":"transaction.updated","data":{"transaction":{"id":"tx-1","status":%q,"amount_in_cents":%d,"reference":%q,"currency":"COP"}},"timestamp":%d,"signature":{"properties":["transaction.id","transaction.status","transaction.amount_in_cents"],"checksum":%q}}`,
		status, amount, ref, ts.Unix(), strings.ToUpper(hex.EncodeToString(sum[:]))))
}

func TestParseWebhook_PP_F5_SEC_002_ValidEvent(t *testing.T) {
	p := New(testConfig("http://unused"), http.DefaultClient)
	ev, err := p.ParseWebhook(http.Header{}, signedEvent("APPROVED", 17850000, now, "events_x"), now)
	if err != nil {
		t.Fatal(err)
	}
	if ev.ID != "tx-1:APPROVED" || ev.Payment.Status != providers.ChargePaid || ev.Payment.Amount != 178500 ||
		ev.Payment.Reference != chargeID.String()+"-1" || ev.Payment.Ref != "tx-1" {
		t.Fatalf("evento = %+v", ev)
	}
	// El mismo cambio de estado reenviado produce el mismo id (idempotencia).
	again, err := p.ParseWebhook(http.Header{}, signedEvent("APPROVED", 17850000, now.Add(-time.Hour), "events_x"), now)
	if err != nil || again.ID != ev.ID {
		t.Fatalf("reenvío = %+v, %v", again, err)
	}
}

func TestParseWebhook_PP_F5_SEC_002_Rejections(t *testing.T) {
	p := New(testConfig("http://unused"), http.DefaultClient)
	good := signedEvent("APPROVED", 17850000, now, "events_x")
	cases := map[string][]byte{
		"otro secreto":      signedEvent("APPROVED", 17850000, now, "otro"),
		"importe alterado":  []byte(strings.Replace(string(good), `"amount_in_cents":17850000`, `"amount_in_cents":1`, 1)),
		"estado alterado":   []byte(strings.Replace(string(good), `"status":"APPROVED"`, `"status":"DECLINED"`, 1)),
		"fuera de ventana":  signedEvent("APPROVED", 17850000, now.Add(-72*time.Hour), "events_x"),
		"del futuro":        signedEvent("APPROVED", 17850000, now.Add(time.Hour), "events_x"),
		"sin firma":         []byte(`{"event":"transaction.updated","data":{"transaction":{"id":"tx-1","status":"APPROVED"}},"timestamp":1}`),
		"no es json":        []byte(`hola`),
		"propiedad ausente": []byte(strings.Replace(string(good), `"transaction.id"`, `"transaction.nope"`, 1)),
	}
	for name, payload := range cases {
		if _, err := p.ParseWebhook(http.Header{}, payload, now); !errors.Is(err, providers.ErrInvalidSignature) {
			t.Errorf("%s: error = %v, quiero firma inválida", name, err)
		}
	}
	h := http.Header{}
	h.Set("X-Event-Checksum", "deadbeef")
	if _, err := p.ParseWebhook(h, good, now); !errors.Is(err, providers.ErrInvalidSignature) {
		t.Errorf("cabecera de checksum distinta: %v", err)
	}
}

func TestParseWebhook_PP_F5_SEC_002_ChecksumMustCoverWhatWeUse(t *testing.T) {
	p := New(testConfig("http://unused"), http.DefaultClient)
	// Evento con checksum válido que solo firma el id: el estado y el importe
	// podrían haberse cambiado, así que no se acepta.
	sum := sha256.Sum256([]byte("tx-1" + fmt.Sprint(now.Unix()) + "events_x"))
	payload := []byte(fmt.Sprintf(`{"event":"transaction.updated","data":{"transaction":{"id":"tx-1","status":"APPROVED","amount_in_cents":1}},"timestamp":%d,"signature":{"properties":["transaction.id"],"checksum":%q}}`,
		now.Unix(), hex.EncodeToString(sum[:])))
	if _, err := p.ParseWebhook(http.Header{}, payload, now); !errors.Is(err, providers.ErrInvalidSignature) {
		t.Fatalf("error = %v", err)
	}
}

func TestParseWebhook_PP_F5_SEC_002_OtherEventsAreIgnored(t *testing.T) {
	p := New(testConfig("http://unused"), http.DefaultClient)
	sum := sha256.Sum256([]byte("abc" + fmt.Sprint(now.Unix()) + "events_x"))
	payload := []byte(fmt.Sprintf(`{"event":"nequi_token.updated","data":{"nequi_token":{"id":"abc"}},"timestamp":%d,"signature":{"properties":["nequi_token.id"],"checksum":%q}}`,
		now.Unix(), hex.EncodeToString(sum[:])))
	if _, err := p.ParseWebhook(http.Header{}, payload, now); !errors.Is(err, providers.ErrIgnoredEvent) {
		t.Fatalf("error = %v", err)
	}
}
