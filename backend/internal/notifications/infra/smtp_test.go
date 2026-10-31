package infra

import (
	"context"
	"errors"
	"net/textproto"
	"strings"
	"testing"

	"github.com/edcastanohunter/petpassport/backend/internal/notifications/app"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/mail"
)

type failMailer struct{ err error }

func (f failMailer) Send(context.Context, mail.Message) error { return f.err }

func TestSMTPChannel_ClassifiesFailures(t *testing.T) {
	cases := []struct {
		name      string
		err       error
		permanent bool
		invalid   bool
	}{
		{"buzón inexistente", &textproto.Error{Code: 550, Msg: "no such user ana@example.com"}, true, true},
		{"dirección no válida", &textproto.Error{Code: 553, Msg: "bad"}, true, true},
		{"rechazo del mensaje", &textproto.Error{Code: 554, Msg: "spam"}, true, false},
		{"temporal", &textproto.Error{Code: 451, Msg: "try later"}, false, false},
		{"transporte", errors.New("dial tcp: connection refused"), false, false},
	}
	for _, c := range cases {
		_, err := NewSMTPChannel(failMailer{c.err}).Send(context.Background(), app.Email{To: "ana@example.com"})
		if err == nil {
			t.Fatalf("%s: esperaba error", c.name)
		}
		if got := errors.Is(err, app.ErrPermanent); got != c.permanent {
			t.Errorf("%s: permanente=%v, quería %v", c.name, got, c.permanent)
		}
		if got := errors.Is(err, app.ErrInvalidAddress); got != c.invalid {
			t.Errorf("%s: dirección inválida=%v, quería %v", c.name, got, c.invalid)
		}
		if strings.Contains(err.Error(), "ana@example.com") {
			t.Errorf("%s: el error no debe contener la dirección (PII): %v", c.name, err)
		}
	}
}

func TestSMTPChannel_ReturnsOwnMessageID(t *testing.T) {
	id, err := NewSMTPChannel(failMailer{}).Send(context.Background(), app.Email{MessageID: "<m1@pp.test>"})
	if err != nil || id != "<m1@pp.test>" {
		t.Fatalf("id=%q err=%v", id, err)
	}
}
