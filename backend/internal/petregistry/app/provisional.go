package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/csv"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/edcastanohunter/petpassport/backend/internal/petregistry/domain"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/bus"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/notify"
	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
)

// ClaimTTL es la vigencia de la invitación de reclamación (14 días, PP-F3-DOM-007).
const ClaimTTL = 14 * 24 * time.Hour

// MaxResends es el número máximo de reenvíos permitidos en 7 días (PP-F3-API-007).
const MaxResends = 3

// RegisterProvisional crea una mascota provisional y opcionalmente su invitación (PP-F3-API-007, PP-F3-DOM-007).
func (s *Service) RegisterProvisional(ctx context.Context, cmd domain.RegisterProvisionalPet, sendInvitation bool) (bus.Result, *time.Time, string, error) {
	if cmd.PetID == uuid.Nil {
		cmd.PetID = s.ids.NewID()
	}
	if cmd.Now.IsZero() {
		cmd.Now = s.clock.Now()
	}

	var token string
	var expiresAt *time.Time

	if sendInvitation {
		contact := strings.TrimSpace(cmd.GuardianEmail)
		contactKind := "email"
		if contact == "" && strings.TrimSpace(cmd.GuardianPhone) != "" {
			contact = strings.TrimSpace(cmd.GuardianPhone)
			contactKind = "phone"
		}
		if contact != "" {
			token = s.tokens.NewToken()
			tokenHash := kernel.HashToken(token)
			exp := cmd.Now.Add(ClaimTTL)
			expiresAt = &exp

			ctx = WithClaimTokenInfo(ctx, cmd.PetID, ClaimTokenInfo{
				TokenHash:   tokenHash,
				ContactHash: hashContact(contact),
				ContactKind: contactKind,
			})
		}
	}

	res, err := s.Dispatch(ctx, cmd)
	if err != nil {
		return bus.Result{}, nil, "", err
	}

	if sendInvitation && token != "" && s.notifier != nil && cmd.GuardianEmail != "" {
		_ = s.notifier.Enqueue(context.WithoutCancel(ctx), notify.Notification{
			Rule:      "pet_claim_invite",
			Template:  "pet_claim_invite",
			Category:  "account",
			To:        cmd.GuardianEmail,
			Locale:    "es",
			SubjectID: cmd.PetID,
			Payload: map[string]string{
				"pet_name": cmd.Name,
				"token":    token,
			},
		})
	}

	return res, expiresAt, token, nil
}

// ResendClaim genera un nuevo token de invitación y reenvía (PP-F3-API-007: máx 3 / 7 días).
func (s *Service) ResendClaim(ctx context.Context, orgID, petID uuid.UUID) error {
	claim, err := s.queries.ClaimByPetID(ctx, petID)
	if err != nil {
		return err
	}
	if claim.OrganizationID != orgID {
		return kernel.NewDomainError("FORBIDDEN", "La organización no coincide con la creadora de la mascota.")
	}
	if claim.ResendCount >= MaxResends {
		return kernel.NewDomainError("RATE_LIMITED", "Límite de reenvíos de invitación alcanzado (máx 3).")
	}

	now := s.clock.Now()
	token := s.tokens.NewToken()
	tokenHash := kernel.HashToken(token)
	exp := now.Add(ClaimTTL)

	ctx = WithClaimTokenInfo(ctx, petID, ClaimTokenInfo{
		TokenHash:   tokenHash,
		ContactHash: claim.ContactHash,
		ContactKind: claim.ContactKind,
	})

	_, err = s.Dispatch(ctx, domain.InvitePetClaim{
		PetID:          petID,
		OrganizationID: orgID,
		ExpiresAt:      exp,
		ResendCount:    claim.ResendCount + 1,
	})
	return err
}

// GetClaimPreview resuelve el token y devuelve los datos de previsualización para el tutor (PP-F3-API-007).
func (s *Service) GetClaimPreview(ctx context.Context, token string, currentUserID uuid.UUID) (ClaimPreviewView, error) {
	now := s.clock.Now()
	tokenHash := kernel.HashToken(token)
	claim, err := s.queries.ClaimByTokenHash(ctx, tokenHash, now)
	if err != nil {
		return ClaimPreviewView{}, err
	}

	pet, err := s.queries.GetPet(ctx, claim.PetID)
	if err != nil {
		return ClaimPreviewView{}, err
	}

	orgName := "Clínica Veterinaria"
	if s.orgs != nil {
		if org, err := s.orgs.ActiveClinic(ctx, claim.OrganizationID); err == nil {
			orgName = org.DisplayName
		}
	}

	var match *MatchingPetView
	if pet.Microchip != "" && currentUserID != uuid.Nil {
		m, err := s.queries.FindMatchingPetByMicrochip(ctx, currentUserID, pet.Microchip)
		if err == nil {
			match = m
		}
	}

	contactMasked := "***"
	if claim.ContactKind == "email" {
		contactMasked = "tutor***@email"
	}

	var photoURL string
	if pet.Photo != nil {
		photoURL = pet.Photo.ObjectKey
	}

	return ClaimPreviewView{
		Pet: PetPreviewView{
			Name:     pet.Name,
			Species:  pet.Species,
			Breed:    pet.Breed,
			Sex:      pet.Sex,
			PhotoURL: photoURL,
		},
		Organization:  OrgPreviewView{DisplayName: orgName},
		ContactMasked: contactMasked,
		ExistingMatch: match,
	}, nil
}

// AcceptClaim procesa la aceptación de una mascota provisional con o sin fusión (PP-F3-API-007, PP-F3-DOM-007).
func (s *Service) AcceptClaim(ctx context.Context, token string, mergeIntoPetID *uuid.UUID) (bus.Result, error) {
	cc := commandContext(ctx)
	if cc.Principal.UserID == uuid.Nil {
		return bus.Result{}, kernel.NewDomainError("UNAUTHENTICATED", "Usuario no autenticado.")
	}

	now := s.clock.Now()
	tokenHash := kernel.HashToken(token)
	claim, err := s.queries.ClaimByTokenHash(ctx, tokenHash, now)
	if err != nil {
		return bus.Result{}, kernel.NewDomainError("CLAIM_EXPIRED", "La invitación de reclamación no es válida o ha caducado.")
	}

	// Verificar coincidencia de contacto (email o teléfono verificado)
	if claim.ContactKind == "email" && cc.Principal.Email != "" {
		expectedHash := hashContact(cc.Principal.Email)
		if !bytes.Equal(claim.ContactHash, expectedHash) {
			return bus.Result{}, kernel.NewDomainError("CLAIM_CONTACT_MISMATCH", "El correo verificado del usuario no coincide con la invitación.")
		}
	}

	if mergeIntoPetID != nil && *mergeIntoPetID != uuid.Nil {
		// Modo fusión: fusionar la provisional en targetPet y conceder CareLink a la clínica en targetPet
		targetPet, err := s.queries.GetPet(ctx, *mergeIntoPetID)
		if err != nil {
			return bus.Result{}, fmt.Errorf("target pet: %w", err)
		}
		if targetPet.GuardianID != cc.Principal.UserID {
			return bus.Result{}, kernel.NewDomainError("FORBIDDEN", "La mascota destino no pertenece al tutor autenticado.")
		}

		res, err := s.Dispatch(ctx, domain.MergeProvisionalInto{
			PetID:       claim.PetID,
			TargetPetID: *mergeIntoPetID,
			Now:         now,
		})
		if err != nil {
			return bus.Result{}, err
		}

		// Otorgar vínculo de atención en la mascota destino a la clínica creadora
		_, _ = s.Dispatch(ctx, domain.GrantCareLink{
			PetID:          *mergeIntoPetID,
			OrganizationID: claim.OrganizationID,
			ActorID:        cc.Principal.UserID,
		})

		return res, nil
	}

	// Modo reclamo directo: asignar el tutor a la mascota provisional
	return s.Dispatch(ctx, domain.ClaimPet{
		PetID:      claim.PetID,
		GuardianID: cc.Principal.UserID,
		Now:        now,
	})
}

// ListProvisionalPets lista los pacientes provisionales de una clínica (PP-F3-API-007).
func (s *Service) ListProvisionalPets(ctx context.Context, orgID uuid.UUID, status string) ([]ProvisionalPetView, error) {
	return s.queries.ListProvisionalPets(ctx, orgID, status)
}

// ImportResult resultado de la importación CSV de pacientes provisionales (PP-F3-PRD-013).
type ImportResult struct {
	Total   int      `json:"total"`
	Created int      `json:"created"`
	Errors  []string `json:"errors,omitempty"`
}

// ImportProvisionalPetsCSV procesa un CSV con pacientes provisionales (PP-F3-PRD-013).
func (s *Service) ImportProvisionalPetsCSV(ctx context.Context, orgID, createdBy uuid.UUID, reader io.Reader) (ImportResult, error) {
	r := csv.NewReader(reader)
	records, err := r.ReadAll()
	if err != nil {
		return ImportResult{}, fmt.Errorf("error leyendo CSV: %w", err)
	}
	if len(records) <= 1 {
		return ImportResult{Total: 0, Created: 0}, nil
	}

	var res ImportResult
	res.Total = len(records) - 1

	for idx, row := range records[1:] {
		rowNum := idx + 2
		if len(row) < 3 {
			res.Errors = append(res.Errors, fmt.Sprintf("fila %d: formato incompleto", rowNum))
			continue
		}
		name := strings.TrimSpace(row[0])
		species := strings.TrimSpace(row[1])
		contact := strings.TrimSpace(row[2])

		if name == "" || species == "" {
			res.Errors = append(res.Errors, fmt.Sprintf("fila %d: nombre y especie son obligatorios", rowNum))
			continue
		}

		cmd := domain.RegisterProvisionalPet{
			PetID:         s.ids.NewID(),
			OrgID:         orgID,
			CreatedBy:     createdBy,
			Name:          name,
			Species:       species,
			Sex:           "unknown",
			Neutered:      "unknown",
			GuardianEmail: contact,
			Now:           s.clock.Now(),
		}
		if len(row) > 3 && strings.TrimSpace(row[3]) != "" {
			breed := strings.TrimSpace(row[3])
			cmd.Breed = &breed
		}
		if len(row) > 4 && strings.TrimSpace(row[4]) != "" {
			chip := strings.TrimSpace(row[4])
			cmd.Microchip = &chip
		}

		_, _, _, err := s.RegisterProvisional(ctx, cmd, contact != "")
		if err != nil {
			res.Errors = append(res.Errors, fmt.Sprintf("fila %d (%s): %v", rowNum, name, err))
		} else {
			res.Created++
		}
	}

	return res, nil
}

func hashContact(contact string) []byte {
	c := strings.ToLower(strings.TrimSpace(contact))
	h := sha256.Sum256([]byte(c))
	return h[:]
}
