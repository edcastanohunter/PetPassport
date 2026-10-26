package app

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/edcastanohunter/petpassport/backend/internal/partnernetwork/domain"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/audit"
)

type fakeAuditor struct {
	entries []audit.Entry
	fail    error
}

func (a *fakeAuditor) Record(_ context.Context, e audit.Entry) error {
	if a.fail != nil {
		return a.fail
	}
	a.entries = append(a.entries, e)
	return nil
}

func TestAdminReason(t *testing.T) {
	for _, tc := range []struct {
		in       string
		required bool
		want     string
		err      bool
	}{
		{"", false, "", false},
		{"   ", false, "", false},
		{"corto", false, "", true},
		{"", true, "", true},
		{"  Documentos verificados  ", true, "Documentos verificados", false},
	} {
		got, err := AdminReason(tc.in, tc.required)
		if (err != nil) != tc.err || got != tc.want {
			t.Errorf("AdminReason(%q,%v) = %q, %v", tc.in, tc.required, got, err)
		}
		if err != nil && !errors.Is(err, ErrReasonRequired) {
			t.Errorf("error inesperado: %v", err)
		}
	}
}

func TestAdmin_TransitionWritesAuditInSameUnitOfWork(t *testing.T) {
	e := newEnv(t, nil)
	auditor := &fakeAuditor{}
	e.svc.auditor = auditor
	org := e.register(t)
	actor := uuid.New()
	for _, verb := range []string{"verify", "suspend", "reactivate"} {
		cmd, err := AdminTransitionCommand(verb, org, "Motivo de la acción "+verb)
		if err != nil {
			t.Fatal(err)
		}
		ctx := WithAdminAudit(context.Background(), audit.Entry{ActorID: &actor, Action: "admin.org." + verb, Reason: "Motivo de la acción " + verb})
		if _, err := e.svc.Dispatch(ctx, cmd); err != nil {
			t.Fatalf("%s: %v", verb, err)
		}
	}
	if len(auditor.entries) != 3 {
		t.Fatalf("entradas: %+v", auditor.entries)
	}
	for _, en := range auditor.entries {
		if en.TargetType != "organization" || en.TargetID == nil || *en.TargetID != org || *en.ActorID != actor {
			t.Fatalf("entrada: %+v", en)
		}
	}
	o, _ := e.repo.Load(context.Background(), org)
	if o.Status() != domain.OrgActive {
		t.Fatalf("estado final: %s", o.Status())
	}
	// Comando no admin: no audita.
	if _, err := e.svc.Dispatch(context.Background(), domain.SuspendOrganization{OrganizationID: org, Reason: "Suspensión de sistema"}); err != nil {
		t.Fatal(err)
	}
	if len(auditor.entries) != 3 {
		t.Fatal("un comando sin WithAdminAudit no debe auditar")
	}
}

func TestAdmin_AuditFailureAbortsCommand(t *testing.T) {
	e := newEnv(t, nil)
	e.svc.auditor = &fakeAuditor{fail: errors.New("audit caído")}
	org := e.register(t)
	ctx := WithAdminAudit(context.Background(), audit.Entry{Action: "admin.org.verify"})
	if _, err := e.svc.Dispatch(ctx, domain.VerifyOrganization{OrganizationID: org}); err == nil {
		t.Fatal("si la auditoría falla, el comando debe fallar (misma tx)")
	}
	if len(e.mail.sent) != 0 {
		t.Fatal("sin commit no hay email de verificación")
	}
}

func TestAdmin_MissingAuditorIsAnError(t *testing.T) {
	e := newEnv(t, nil)
	org := e.register(t)
	ctx := WithAdminAudit(context.Background(), audit.Entry{Action: "admin.org.verify"})
	if _, err := e.svc.Dispatch(ctx, domain.VerifyOrganization{OrganizationID: org}); err == nil {
		t.Fatal("comando admin sin Auditor configurado debe fallar")
	}
}

func TestAdminTransitionCommand_Unknown(t *testing.T) {
	if _, err := AdminTransitionCommand("delete", uuid.New(), ""); err == nil {
		t.Fatal("acción desconocida")
	}
}
