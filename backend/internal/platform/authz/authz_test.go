package authz

import (
	"testing"

	"github.com/google/uuid"

	"github.com/edcastanohunter/petpassport/backend/internal/platform/auth"
)

func TestCan_DenyByDefault(t *testing.T) {
	p := auth.Principal{UserID: uuid.Must(uuid.NewV7())}
	if d := Can(p, Action("pet:read"), nil); d.Allow {
		t.Fatal("acción sin política debe denegar")
	}
}

func TestCan_OwnAccountActions(t *testing.T) {
	p := auth.Principal{UserID: uuid.Must(uuid.NewV7())}
	for _, a := range []Action{MeRead, MeUpdate, OnboardingComplete} {
		if d := Can(p, a, nil); !d.Allow {
			t.Fatalf("%s debe permitir: %s", a, d.Reason)
		}
		if d := Can(auth.Principal{}, a, nil); d.Allow {
			t.Fatalf("%s sin principal debe denegar", a)
		}
		if d := Can(auth.Principal{UserID: p.UserID, Account: auth.AccountState{Suspended: true}}, a, nil); d.Allow {
			t.Fatalf("%s con cuenta suspendida debe denegar", a)
		}
	}
}

func TestRegister(t *testing.T) {
	Register(Action("test:action"), func(auth.Principal, Resource) Decision { return Decision{Allow: true} })
	defer delete(policies, Action("test:action"))
	if !Can(auth.Principal{}, Action("test:action"), nil).Allow {
		t.Fatal("política registrada no aplicada")
	}
	defer func() {
		if recover() == nil {
			t.Fatal("registro duplicado debe hacer panic")
		}
	}()
	Register(Action("test:action"), nil)
}

func TestActions(t *testing.T) {
	if len(Actions()) < 3 {
		t.Fatal("faltan acciones")
	}
}
