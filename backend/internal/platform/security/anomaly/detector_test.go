package anomaly_test

import (
	"context"
	"testing"
	"time"

	"github.com/edcastanohunter/petpassport/backend/internal/platform/security/anomaly"
)

type memoryMailer struct {
	sent []string
}

func (m *memoryMailer) Send(ctx context.Context, to, subject, body string) error {
	m.sent = append(m.sent, subject)
	return nil
}

func TestDetector_SEC006_InstantiatesAndRuns(t *testing.T) {
	ctx := context.Background()
	mailer := &memoryMailer{}
	detector := anomaly.NewDetector(nil, mailer, "security@petpassport.app", nil)

	alerts, err := detector.CheckAnomalies(ctx, time.Now())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(alerts) != 0 {
		t.Fatalf("expected 0 alerts without DB, got %d", len(alerts))
	}
}
