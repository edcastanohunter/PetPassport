package app

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/argon2"

	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
)

// Errores de enlaces compartidos (catálogo API-012).
var (
	ErrShareExpired = kernel.NewDomainError("SHARE_EXPIRED", "El enlace caducó o fue revocado.")
	ErrPinRequired  = kernel.NewDomainError("PIN_REQUIRED", "El enlace requiere un PIN.")
	ErrPinInvalid   = kernel.NewDomainError("PIN_INVALID", "PIN incorrecto.")
	ErrShareLocked  = kernel.NewDomainError("SHARE_LOCKED", "Demasiados intentos: el enlace está bloqueado temporalmente.")
)

var pinRe = regexp.MustCompile(`^[0-9]{4,6}$`)

// Parámetros Argon2id (OWASP: m=19 MiB, t=2, p=1).
const (
	argonTime    = 2
	argonMemKiB  = 19 * 1024
	argonThreads = 1
	argonKeyLen  = 32
)

// PublicPet son los datos públicos de la mascota (SEC-002: sin contacto del
// tutor, microchip enmascarado).
type PublicPet struct {
	Name            string
	Species         string
	Breed           *string
	Sex             string
	BirthDate       *string
	MicrochipMasked *string
	HasPhoto        bool
}

// PublicPets lee los datos públicos de una mascota.
type PublicPets interface {
	PublicPet(ctx context.Context, petID uuid.UUID) (PublicPet, error)
}

// ShareService gestiona los enlaces compartidos del carnet (ARQ-013).
type ShareService struct {
	shares Shares
	cards  CardQueries
	pets   PublicPets
	clock  kernel.Clock
	ids    kernel.IDGenerator
}

// NewShareService construye el servicio.
func NewShareService(shares Shares, cards CardQueries, pets PublicPets, clock kernel.Clock, ids kernel.IDGenerator) *ShareService {
	if clock == nil {
		clock = kernel.RealClock{}
	}
	if ids == nil {
		ids = kernel.RealIDGenerator{}
	}
	return &ShareService{shares: shares, cards: cards, pets: pets, clock: clock, ids: ids}
}

// HashToken es el SHA-256 del token que se guarda en BD.
func HashToken(token string) []byte {
	h := sha256.Sum256([]byte(token))
	return h[:]
}

// HashPIN devuelve "argon2id$<salt>$<hash>" (base64 sin relleno).
func HashPIN(pin string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	sum := argon2.IDKey([]byte(pin), salt, argonTime, argonMemKiB, argonThreads, argonKeyLen)
	enc := base64.RawStdEncoding
	return "argon2id$" + enc.EncodeToString(salt) + "$" + enc.EncodeToString(sum), nil
}

// VerifyPIN compara en tiempo constante.
func VerifyPIN(pin, stored string) bool {
	parts := strings.Split(stored, "$")
	if len(parts) != 3 || parts[0] != "argon2id" {
		return false
	}
	enc := base64.RawStdEncoding
	salt, err1 := enc.DecodeString(parts[1])
	want, err2 := enc.DecodeString(parts[2])
	if err1 != nil || err2 != nil {
		return false
	}
	if len(want) != int(argonKeyLen) {
		return false
	}
	got := argon2.IDKey([]byte(pin), salt, argonTime, argonMemKiB, argonThreads, argonKeyLen)
	return subtle.ConstantTimeCompare(got, want) == 1
}

// CreatedShare es el resultado de crear un enlace: el token solo se ve aquí.
type CreatedShare struct {
	ID        uuid.UUID
	Token     string
	ExpiresAt time.Time
}

// Create crea un enlace de solo lectura. Solo el tutor (lo autoriza http).
func (s *ShareService) Create(ctx context.Context, petID, userID uuid.UUID, scope string, days int, pin string) (CreatedShare, error) {
	ve := &kernel.ValidationError{}
	if scope != "vaccination_card" && scope != "full_record" {
		ve.Add("scope", "INVALID")
	}
	if days < MinShareDays || days > MaxShareDays {
		ve.Add("expires_in_days", "OUT_OF_RANGE")
	}
	if pin != "" && !pinRe.MatchString(pin) {
		ve.Add("pin", "INVALID")
	}
	if err := ve.ErrOrNil(); err != nil {
		return CreatedShare{}, err
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return CreatedShare{}, err
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	now := s.clock.Now()
	rec := ShareRecord{
		ShareView: ShareView{ID: s.ids.NewID(), PetID: petID, Scope: scope, HasPIN: pin != "",
			ExpiresAt: now.Add(time.Duration(days) * 24 * time.Hour), CreatedAt: now},
		TokenHash: HashToken(token), CreatedBy: userID,
	}
	if pin != "" {
		h, err := HashPIN(pin)
		if err != nil {
			return CreatedShare{}, err
		}
		rec.PinHash = h
	}
	if err := s.shares.Create(ctx, rec); err != nil {
		return CreatedShare{}, err
	}
	return CreatedShare{ID: rec.ID, Token: token, ExpiresAt: rec.ExpiresAt}, nil
}

// List devuelve los enlaces activos de la mascota.
func (s *ShareService) List(ctx context.Context, petID uuid.UUID) ([]ShareView, error) {
	return s.shares.List(ctx, petID)
}

// Revoke revoca un enlace.
func (s *ShareService) Revoke(ctx context.Context, petID, id uuid.UUID) error {
	return s.shares.Revoke(ctx, petID, id, s.clock.Now())
}

// unlockCookie firma "<exp>.<mac>" con el hash del PIN como clave: solo quien
// conoce el PIN (o el servidor) puede producirlo, y caduca con el enlace.
func unlockCookie(rec ShareRecord, exp time.Time) string {
	msg := rec.ID.String() + "|" + strconv.FormatInt(exp.Unix(), 10)
	mac := hmac.New(sha256.New, []byte(rec.PinHash))
	mac.Write([]byte(msg))
	return strconv.FormatInt(exp.Unix(), 10) + "." + hex.EncodeToString(mac.Sum(nil))
}

func validUnlock(rec ShareRecord, cookie string, now time.Time) bool {
	i := strings.IndexByte(cookie, '.')
	if i < 0 {
		return false
	}
	exp, err := strconv.ParseInt(cookie[:i], 10, 64)
	if err != nil || now.Unix() > exp {
		return false
	}
	want := unlockCookie(rec, time.Unix(exp, 0))
	return hmac.Equal([]byte(want), []byte(cookie))
}

// UnlockTTL es la vida de la sesión de enlace tras introducir el PIN.
const UnlockTTL = 15 * time.Minute

func (s *ShareService) active(ctx context.Context, token string) (ShareRecord, error) {
	rec, err := s.shares.ByTokenHash(ctx, HashToken(token))
	if err != nil {
		return rec, err
	}
	if rec.RevokedAt != nil || !s.clock.Now().Before(rec.ExpiresAt) {
		return rec, ErrShareExpired
	}
	return rec, nil
}

// Unlock valida el PIN y devuelve el valor de la cookie de sesión de enlace.
// Cinco fallos bloquean el enlace una hora (SEC-002).
func (s *ShareService) Unlock(ctx context.Context, token, pin string) (string, time.Time, error) {
	rec, err := s.active(ctx, token)
	if err != nil {
		return "", time.Time{}, err
	}
	now := s.clock.Now()
	if rec.LockedUntil != nil && now.Before(*rec.LockedUntil) {
		return "", time.Time{}, ErrShareLocked
	}
	if !rec.HasPIN {
		return "", time.Time{}, nil
	}
	if !VerifyPIN(pin, rec.PinHash) {
		locked, err := s.shares.PinFailed(ctx, rec.ID, now.Add(PinLockDuration), MaxPinAttempts)
		if err != nil {
			return "", time.Time{}, err
		}
		if locked {
			return "", time.Time{}, ErrShareLocked
		}
		return "", time.Time{}, ErrPinInvalid
	}
	if err := s.shares.PinOK(ctx, rec.ID); err != nil {
		return "", time.Time{}, err
	}
	exp := now.Add(UnlockTTL)
	if exp.After(rec.ExpiresAt) {
		exp = rec.ExpiresAt
	}
	return unlockCookie(rec, exp), exp, nil
}

// PublicShare es lo que ve quien abre el enlace (sin datos del tutor).
type PublicShare struct {
	Scope      string
	Pet        PublicPet
	Card       CardView
	VerifiedAt time.Time
}

// View devuelve el contenido público. Con PIN exige la cookie de sesión de
// enlace; cuenta el acceso (el ip_hash lo audita http).
func (s *ShareService) View(ctx context.Context, token, unlockCookieValue string) (PublicShare, uuid.UUID, error) {
	rec, err := s.active(ctx, token)
	if err != nil {
		return PublicShare{}, uuid.Nil, err
	}
	now := s.clock.Now()
	if rec.LockedUntil != nil && now.Before(*rec.LockedUntil) {
		return PublicShare{}, uuid.Nil, ErrShareLocked
	}
	if rec.HasPIN && !validUnlock(rec, unlockCookieValue, now) {
		return PublicShare{}, uuid.Nil, ErrPinRequired
	}
	pet, err := s.pets.PublicPet(ctx, rec.PetID)
	if err != nil {
		return PublicShare{}, uuid.Nil, err
	}
	card, err := s.cards.Card(ctx, rec.PetID, false)
	if err != nil {
		return PublicShare{}, uuid.Nil, fmt.Errorf("share card: %w", err)
	}
	if err := s.shares.RecordAccess(ctx, rec.ID, now); err != nil {
		return PublicShare{}, uuid.Nil, err
	}
	return PublicShare{Scope: rec.Scope, Pet: pet, Card: card, VerifiedAt: now}, rec.ID, nil
}
