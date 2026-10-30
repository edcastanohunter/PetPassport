package domain

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

// ErrTokenInvalid cubre token mal formado, firma errónea y caducado: la API
// responde 404 sin distinguir (no revela si el usuario existe).
var ErrTokenInvalid = errors.New("notifications: token de baja inválido")

// UnsubscribeTTL es la vigencia de un enlace de baja en un correo.
const UnsubscribeTTL = 180 * 24 * time.Hour

// SignUnsubscribe firma user_id|category|exp con HMAC-SHA256 (NOT-004). El token
// es opaco para el cliente y no requiere sesión.
func SignUnsubscribe(secret []byte, user uuid.UUID, c Category, exp time.Time) string {
	body := base64.RawURLEncoding.EncodeToString([]byte(
		user.String() + "|" + string(c) + "|" + strconv.FormatInt(exp.Unix(), 10)))
	return body + "." + base64.RawURLEncoding.EncodeToString(mac(secret, body))
}

// VerifyUnsubscribe valida firma y vigencia y devuelve usuario y categoría.
// security nunca se puede dar de baja.
func VerifyUnsubscribe(secret []byte, token string, now time.Time) (uuid.UUID, Category, error) {
	body, sig, ok := strings.Cut(token, ".")
	if !ok {
		return uuid.Nil, "", ErrTokenInvalid
	}
	want, err := base64.RawURLEncoding.DecodeString(sig)
	if err != nil || !hmac.Equal(want, mac(secret, body)) {
		return uuid.Nil, "", ErrTokenInvalid
	}
	raw, err := base64.RawURLEncoding.DecodeString(body)
	if err != nil {
		return uuid.Nil, "", ErrTokenInvalid
	}
	parts := strings.Split(string(raw), "|")
	if len(parts) != 3 {
		return uuid.Nil, "", ErrTokenInvalid
	}
	user, err := uuid.Parse(parts[0])
	if err != nil {
		return uuid.Nil, "", ErrTokenInvalid
	}
	exp, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil || now.Unix() > exp {
		return uuid.Nil, "", ErrTokenInvalid
	}
	c := Category(parts[1])
	if !c.Optional() {
		return uuid.Nil, "", fmt.Errorf("%w: categoría %q", ErrTokenInvalid, parts[1])
	}
	return user, c, nil
}

func mac(secret []byte, body string) []byte {
	h := hmac.New(sha256.New, secret)
	h.Write([]byte(body))
	return h.Sum(nil)
}
