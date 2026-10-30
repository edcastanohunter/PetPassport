//go:build integration

// Notifications de extremo a extremo contra Postgres (PP-F2-NOT-001..007,
// DAT-005, QA-005): encolar tres veces el mismo aviso deja una sola entrega;
// el emisor lo entrega una vez, en el idioma del destinatario, y purga el
// enlace secreto; una dirección rebotada deja de recibir; un recordatorio
// cuyo sujeto ya no existe se cancela sin enviar nada.
package integration

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"

	notificationsapp "github.com/edcastanohunter/petpassport/backend/internal/notifications/app"
	notificationsinfra "github.com/edcastanohunter/petpassport/backend/internal/notifications/infra"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/db"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/notify"
	"github.com/edcastanohunter/petpassport/backend/test/testkit"
)

// simClock es el reloj simulado de la prueba (avanza a mano).
type simClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *simClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *simClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

// capture es el canal falso: guarda lo que el SMTP real enviaría.
type capture struct {
	mu   sync.Mutex
	sent []notificationsapp.Email
}

func (c *capture) Send(_ context.Context, m notificationsapp.Email) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sent = append(c.sent, m)
	return "prov-" + uuid.NewString(), nil
}

func (c *capture) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.sent)
}

type notifEnv struct {
	pool     *db.Pool
	clock    *simClock
	channel  *capture
	enqueue  *notificationsinfra.Enqueuer
	deliver  *notificationsinfra.Deliverer
	dispatch *notificationsinfra.Dispatcher
	supp     *notificationsinfra.Suppressions
}

func newNotifEnv(t *testing.T) *notifEnv {
	t.Helper()
	pool, err := db.Open(context.Background(), testkit.Postgres(t), 8, prometheus.NewRegistry())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	clock := &simClock{t: time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)}
	secret := []byte("0123456789abcdef0123456789abcdef")
	metrics := notificationsinfra.NewMetrics(prometheus.NewRegistry())
	channel := &capture{}
	supp := notificationsinfra.NewSuppressions(pool.Pool)
	prefs := notificationsapp.NewPreferences(notificationsinfra.NewPrefStore(pool.Pool), secret, clock)
	sender := notificationsapp.NewSender(notificationsinfra.NewDirectory(pool.Pool), prefs, supp, supp, channel,
		notificationsapp.SenderConfig{
			BaseURL: "https://app.test", APIBaseURL: "https://api.test/api/v1",
			Address: "PetPassport, Calle 1", FromDomain: "petpassport.test", UnsubscribeSecret: secret,
		}, clock)
	return &notifEnv{
		pool: pool, clock: clock, channel: channel, supp: supp,
		enqueue:  notificationsinfra.NewEnqueuer(pool.Pool),
		deliver:  notificationsinfra.NewDeliverer(pool.Pool, sender, metrics, logger, clock),
		dispatch: notificationsinfra.NewDispatcher(pool.Pool, metrics, logger, clock),
	}
}

func inviteNotification(to, token string) notify.Notification {
	return notify.Notification{
		Rule: "transfer_invite", Template: "transfer_invite", Category: "account",
		To: to, Locale: "en", SubjectID: uuid.MustParse("00000000-0000-0000-0000-0000000000a1"),
		Occurrence: "occ-1",
		Payload: map[string]string{
			"from_name": "Ana", "pet_name": "Luna", "expires_at": "2026-11-09",
			"link_path": "/app/transfers/" + token,
		},
		Secret: []string{"link_path"},
	}
}

func (e *notifEnv) count(t *testing.T, where string, args ...any) int {
	t.Helper()
	var n int
	if err := e.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM notifications.deliveries WHERE `+where, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// TestNotifications_TripleEnqueueOneDelivery: la misma notificación encolada
// tres veces (reintento del cliente, reentrega del outbox) deja una entrega y
// un solo correo, en inglés, con el enlace presente en el mensaje y ausente
// de la fila una vez enviado (NOT-006, DAT-005, ADR-026).
func TestNotifications_TripleEnqueueOneDelivery(t *testing.T) {
	e := newNotifEnv(t)
	ctx := context.Background()
	n := inviteNotification("beto@example.com", "tok-transfer-abcdefghijk")
	for i := 0; i < 3; i++ {
		if err := e.enqueue.Enqueue(ctx, n); err != nil {
			t.Fatal(err)
		}
	}
	if got := e.count(t, `template = 'transfer_invite'`); got != 1 {
		t.Fatalf("entregas tras 3 encolados: %d, quería 1", got)
	}
	for i := 0; i < 3; i++ { // varias pasadas del emisor tampoco duplican
		if _, err := e.deliver.Tick(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if e.channel.count() != 1 {
		t.Fatalf("correos enviados: %d, quería 1", e.channel.count())
	}
	m := e.channel.sent[0]
	if !strings.EqualFold(m.To, "beto@example.com") || !strings.Contains(m.Text, "https://app.test/app/transfers/tok-transfer-abcdefghijk") {
		t.Fatalf("mensaje: %+v", m)
	}
	if got := e.count(t, `status = 'sent' AND payload::text NOT LIKE '%tok-transfer%'`); got != 1 {
		t.Fatal("el enlace secreto debe purgarse de la fila al enviarse")
	}
}

// TestNotifications_BouncedAddressIsSuppressed: tras un rebote, la dirección
// no vuelve a recibir correo (NOT-007).
func TestNotifications_BouncedAddressIsSuppressed(t *testing.T) {
	e := newNotifEnv(t)
	ctx := context.Background()
	if err := e.supp.Suppress(ctx, notificationsapp.AddressHash("rebota@example.com"), "bounce"); err != nil {
		t.Fatal(err)
	}
	if err := e.enqueue.Enqueue(ctx, inviteNotification("rebota@example.com", "tok-rebote-abcdefghijkl")); err != nil {
		t.Fatal(err)
	}
	if _, err := e.deliver.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	if e.channel.count() != 0 {
		t.Fatal("no debe enviarse a una dirección suprimida")
	}
	if got := e.count(t, `status = 'suppressed'`); got != 1 {
		t.Fatalf("entregas suprimidas: %d", got)
	}
}

// TestNotifications_DispatchCancelsOrphanReminder: un recordatorio vencido
// cuyo sujeto ya no existe se cancela sin generar entrega (NOT-002), y con el
// reloj antes del vencimiento no se toca.
func TestNotifications_DispatchCancelsOrphanReminder(t *testing.T) {
	e := newNotifEnv(t)
	ctx := context.Background()
	id, user, subject := uuid.New(), uuid.New(), uuid.New()
	due := e.clock.Now().Add(24 * time.Hour)
	if _, err := e.pool.Exec(ctx,
		`INSERT INTO notifications.scheduled (id, dedup_key, rule, category, template, recipient_id, subject_type, subject_id, due_at, payload, status)
		 VALUES ($1, 'appointment_confirmed|x|T-24h', 'appointment_confirmed', 'appointments', 'appointment_reminder', $2, 'appointment', $3, $4, '{}', 'pending')`,
		id, user, subject, due); err != nil {
		t.Fatal(err)
	}
	if n, err := e.dispatch.Tick(ctx); err != nil || n != 0 {
		t.Fatalf("antes del vencimiento no se despacha: n=%d err=%v", n, err)
	}
	e.clock.Advance(25 * time.Hour)
	if n, err := e.dispatch.Tick(ctx); err != nil || n != 1 {
		t.Fatalf("tras el vencimiento: n=%d err=%v", n, err)
	}
	var status string
	if err := e.pool.QueryRow(ctx, `SELECT status FROM notifications.scheduled WHERE id = $1`, id).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "cancelled" || e.count(t, `template = 'appointment_reminder'`) != 0 {
		t.Fatalf("recordatorio huérfano: status=%s", status)
	}
}
