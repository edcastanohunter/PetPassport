package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/edcastanohunter/petpassport/backend/internal/platform/eventstore"
	"github.com/edcastanohunter/petpassport/backend/internal/prescriptions/domain"
	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
)

// Options agrupa las dependencias necesarias para Service (PP-F4-ARQ-001).
type Options struct {
	Repo          Repository
	UoW           TxRunner
	Projects      ProjectionRunner
	Queries       PrescriptionQueries
	Credentials   domain.CredentialCheck
	Rules         domain.RegulatoryRules
	Catalogs      domain.CatalogEquivalence
	PlanReader    domain.EncounterPlanReader
	AccessChecker domain.PetAccessChecker
	PetReader     PetAccessReader
	OrgReader     OrgNameReader
	UserReader    UserNameReader
	PDFGen        PDFGenerator
	Logger        *slog.Logger
	Clock         kernel.Clock
	IDs           kernel.IDGenerator
}

// Service es el caso de uso central del módulo Prescriptions (PP-F4-ARQ-001, DOM-004).
type Service struct {
	repo          Repository
	uow           TxRunner
	projects      ProjectionRunner
	queries       PrescriptionQueries
	credentials   domain.CredentialCheck
	rules         domain.RegulatoryRules
	catalogs      domain.CatalogEquivalence
	planReader    domain.EncounterPlanReader
	accessChecker domain.PetAccessChecker
	petReader     PetAccessReader
	orgReader     OrgNameReader
	userReader    UserNameReader
	pdfGen        PDFGenerator
	logger        *slog.Logger
	clock         kernel.Clock
	ids           kernel.IDGenerator
}

// NewService construye la instancia de Service.
func NewService(opts Options) *Service {
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	clock := opts.Clock
	if clock == nil {
		clock = kernel.RealClock{}
	}
	ids := opts.IDs
	if ids == nil {
		ids = kernel.RealIDGenerator{}
	}
	return &Service{
		repo:          opts.Repo,
		uow:           opts.UoW,
		projects:      opts.Projects,
		queries:       opts.Queries,
		credentials:   opts.Credentials,
		rules:         opts.Rules,
		catalogs:      opts.Catalogs,
		planReader:    opts.PlanReader,
		accessChecker: opts.AccessChecker,
		petReader:     opts.PetReader,
		orgReader:     opts.OrgReader,
		userReader:    opts.UserReader,
		pdfGen:        opts.PDFGen,
		logger:        logger,
		clock:         clock,
		ids:           ids,
	}
}

// DraftIssuedPrescription inicia un borrador de receta médica emitida por un veterinario (PP-F4-DOM-004, DOM-013).
func (s *Service) DraftIssuedPrescription(ctx context.Context, cmd DraftIssuedPrescriptionCmd, vetID uuid.UUID) (uuid.UUID, error) {
	now := s.clock.Now().UTC()

	// 1. Invariante R1: credencial profesional verificada
	var credRef *domain.CredentialRef
	if s.credentials != nil {
		cr, verified, err := s.credentials.Verified(ctx, vetID, cmd.Country)
		if err != nil {
			return uuid.Nil, fmt.Errorf("check credentials: %w", err)
		}
		if !verified || cr == nil {
			return uuid.Nil, domain.ErrCredentialRequired
		}
		credRef = cr
	} else {
		credRef = &domain.CredentialRef{
			Country:        cmd.Country,
			RegistryNumber: "VET-DEFAULT",
			VerifiedAt:     now,
		}
	}

	// 2. Invariante R2: verificar acceso de la organización a la mascota
	if s.accessChecker != nil {
		hasAccess, err := s.accessChecker.HasActiveAccess(ctx, cmd.IssuerOrgID, cmd.PetID)
		if err != nil {
			return uuid.Nil, fmt.Errorf("check pet access: %w", err)
		}
		if !hasAccess {
			return uuid.Nil, domain.ErrOrgAccessDenied
		}
	}

	// 3. Resolver tutor y datos de mascota
	var guardianID uuid.UUID
	if s.petReader != nil {
		_, _, gID, err := s.petReader.GetPetInfo(ctx, cmd.PetID)
		if err != nil {
			return uuid.Nil, fmt.Errorf("get pet info: %w", err)
		}
		guardianID = gID
	}

	// 4. Si viene de encuentro firmado, importar o validar ítems de plan clínico (PP-F4-DOM-013)
	hasSignedPlan := false
	if cmd.EncounterID != nil && s.planReader != nil {
		planItems, valid, err := s.planReader.SignedPlanItems(ctx, *cmd.EncounterID, cmd.PetID, cmd.IssuerOrgID)
		if err != nil {
			return uuid.Nil, fmt.Errorf("read encounter plan: %w", err)
		}
		if !valid {
			return uuid.Nil, domain.ErrEncounterNotSigned
		}
		hasSignedPlan = true

		// Si no se pasaron ítems explícitos, pre-rellenar desde el encuentro firmado
		if len(cmd.Items) == 0 && len(planItems) > 0 {
			cmd.Items = make([]domain.PrescriptionItem, 0, len(planItems))
			for _, pi := range planItems {
				catID := pi.CatalogID
				var freeText *string
				if catID == nil {
					ft := pi.Name
					freeText = &ft
				}
				cmd.Items = append(cmd.Items, domain.PrescriptionItem{
					ItemID:              s.ids.NewID().String(),
					CatalogID:           catID,
					FreeText:            freeText,
					NameSnapshot:        pi.Name,
					Dose:                domain.DoseVO{Value: pi.DoseValue, Unit: pi.DoseUnit},
					Route:               pi.Route,
					Frequency:           pi.Frequency,
					DurationDays:        pi.DurationDays,
					QuantityToDispense:  domain.QuantityVO{Value: 1, Unit: "pack"},
					SubstitutionAllowed: true,
					Instructions:        pi.Instructions,
				})
			}
		}
	}

	// 5. Resolver reglas regulatorias del país
	rule, err := s.rules.ForCountry(ctx, cmd.Country)
	if err != nil {
		return uuid.Nil, fmt.Errorf("regulatory rules for country: %w", err)
	}

	// 6. Normalizar validez por defecto si no se especificó
	if cmd.ValidUntil == nil {
		defaultExp := now.AddDate(0, 0, rule.DefaultValidityDays)
		vu := kernel.CivilDateOf(defaultExp)
		cmd.ValidUntil = &vu
	}

	// 7. Normalizar ítems: asignar ItemID y verificar controlados
	for i := range cmd.Items {
		if cmd.Items[i].ItemID == "" {
			cmd.Items[i].ItemID = s.ids.NewID().String()
		}
		if cmd.Items[i].CatalogID == nil && cmd.Items[i].FreeText == nil {
			ft := cmd.Items[i].NameSnapshot
			cmd.Items[i].FreeText = &ft
		}
		if cmd.Items[i].QuantityToDispense.Value <= 0 {
			cmd.Items[i].QuantityToDispense = domain.QuantityVO{Value: 1, Unit: "pack"}
		}
		if cmd.Items[i].CatalogID != nil && s.catalogs != nil {
			isCtrl, err := s.catalogs.IsControlled(ctx, *cmd.Items[i].CatalogID)
			if err == nil && isCtrl {
				cmd.Items[i].Controlled = true
			}
		}
	}

	rxID := s.ids.NewID()
	if cmd.ID != nil && *cmd.ID != uuid.Nil {
		rxID = *cmd.ID
	}

	validity := domain.Validity{
		ValidUntil:     *cmd.ValidUntil,
		RefillsAllowed: cmd.RefillsAllowed,
	}

	rx := domain.NewPrescription()
	if _, err := rx.DraftIssuedPrescription(
		rxID,
		cmd.PetID,
		guardianID,
		vetID,
		cmd.IssuerOrgID,
		cmd.EncounterID,
		cmd.Country,
		cmd.Items,
		&validity,
		cmd.Notes,
		rule,
		true,
		credRef,
		hasSignedPlan,
		now,
	); err != nil {
		return uuid.Nil, err
	}

	err = s.uow.Do(ctx, func(txCtx context.Context) error {
		meta := eventstore.Metadata{
			ActorID: vetID.String(),
		}
		stored, err := s.repo.Save(txCtx, rx, meta)
		if err != nil {
			return err
		}
		if s.projects != nil {
			return s.projects.Apply(txCtx, stored)
		}
		return nil
	})
	if err != nil {
		return uuid.Nil, err
	}

	return rxID, nil
}

// UpdateDraft actualiza los datos de un borrador de receta (PP-F4-DOM-004).
func (s *Service) UpdateDraft(ctx context.Context, cmd UpdateDraftCmd, vetID uuid.UUID) error {
	now := s.clock.Now().UTC()

	return s.uow.Do(ctx, func(txCtx context.Context) error {
		rx, err := s.repo.Load(txCtx, cmd.PrescriptionID)
		if err != nil {
			return err
		}

		rule, err := s.rules.ForCountry(txCtx, rx.Country)
		if err != nil {
			return fmt.Errorf("rules: %w", err)
		}

		for i := range cmd.Items {
			if cmd.Items[i].ItemID == "" {
				cmd.Items[i].ItemID = s.ids.NewID().String()
			}
			if cmd.Items[i].CatalogID == nil && cmd.Items[i].FreeText == nil {
				ft := cmd.Items[i].NameSnapshot
				cmd.Items[i].FreeText = &ft
			}
			if cmd.Items[i].QuantityToDispense.Value <= 0 {
				cmd.Items[i].QuantityToDispense = domain.QuantityVO{Value: 1, Unit: "pack"}
			}
			if cmd.Items[i].CatalogID != nil && s.catalogs != nil {
				isCtrl, err := s.catalogs.IsControlled(txCtx, *cmd.Items[i].CatalogID)
				if err == nil && isCtrl {
					cmd.Items[i].Controlled = true
				}
			}
		}

		var val *domain.Validity
		if cmd.ValidUntil != nil || cmd.RefillsAllowed > 0 {
			v := domain.Validity{
				RefillsAllowed: cmd.RefillsAllowed,
			}
			if cmd.ValidUntil != nil {
				v.ValidUntil = *cmd.ValidUntil
			} else {
				v.ValidUntil = rx.Validity.ValidUntil
			}
			val = &v
		}

		if _, err := rx.UpdateDraft(cmd.Items, val, cmd.Notes, rule, now); err != nil {
			return err
		}

		meta := eventstore.Metadata{
			ActorID: vetID.String(),
		}
		stored, err := s.repo.Save(txCtx, rx, meta)
		if err != nil {
			return err
		}
		if s.projects != nil {
			return s.projects.Apply(txCtx, stored)
		}
		return nil
	})
}

// IssuePrescription firma y emite la receta médica veterinaria (PP-F4-DOM-004, DOM-005, SEC-006).
func (s *Service) IssuePrescription(ctx context.Context, cmd IssuePrescriptionCmd, vetID uuid.UUID) (uuid.UUID, error) {
	now := s.clock.Now().UTC()

	// 1. Cargar el borrador para obtener país
	rx, err := s.repo.Load(ctx, cmd.PrescriptionID)
	if err != nil {
		return uuid.Nil, err
	}

	// 2. Invariante R1: credencial profesional verificada
	var credRef *domain.CredentialRef
	if s.credentials != nil {
		cr, verified, err := s.credentials.Verified(ctx, vetID, rx.Country)
		if err != nil {
			return uuid.Nil, fmt.Errorf("verify credential: %w", err)
		}
		if !verified || cr == nil {
			return uuid.Nil, domain.ErrCredentialRequired
		}
		credRef = cr
	} else {
		credRef = &domain.CredentialRef{
			Country:        rx.Country,
			RegistryNumber: "VET-DEFAULT",
			VerifiedAt:     now,
		}
	}

	rule, err := s.rules.ForCountry(ctx, rx.Country)
	if err != nil {
		return uuid.Nil, fmt.Errorf("rules: %w", err)
	}

	var (
		newVersion int64
		pID        uuid.UUID
	)

	err = s.uow.Do(ctx, func(txCtx context.Context) error {
		rxTx, err := s.repo.Load(txCtx, cmd.PrescriptionID)
		if err != nil {
			return err
		}

		contentHash, err := s.repo.ContentHash(txCtx, cmd.PrescriptionID, rxTx.Version)
		if err != nil {
			return fmt.Errorf("content hash: %w", err)
		}

		if _, err := rxTx.IssuePrescription(vetID, credRef, rule, contentHash, now); err != nil {
			return err
		}

		meta := eventstore.Metadata{
			ActorID: vetID.String(),
		}
		stored, err := s.repo.Save(txCtx, rxTx, meta)
		if err != nil {
			return err
		}
		if s.projects != nil {
			if err := s.projects.Apply(txCtx, stored); err != nil {
				return err
			}
		}

		newVersion = rxTx.Version
		pID = rxTx.ID
		return nil
	})
	if err != nil {
		return uuid.Nil, err
	}

	// 3. Post-commit: Renderizar PDF y actualizar pdf_object_key en segundo plano
	if s.pdfGen != nil && s.queries != nil {
		go func(rxID uuid.UUID, ver int64) {
			bgCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second) // #nosec G118
			defer cancel()

			detail, err := s.queries.GetByID(bgCtx, rxID)
			if err != nil {
				s.logger.Error("load detail for pdf", "rx_id", rxID, "err", err)
				return
			}

			contentHash, _ := s.repo.ContentHash(bgCtx, rxID, ver)
			petName, petSpecies := "Mascota", "Canino"
			guardianName := "Tutor Responsable"
			if s.petReader != nil {
				if pn, ps, _, err := s.petReader.GetPetInfo(bgCtx, detail.PetID); err == nil {
					petName, petSpecies = pn, ps
				}
			}
			orgName := "Clínica Veterinaria"
			if detail.IssuerOrgID != nil && s.orgReader != nil {
				if on, err := s.orgReader.GetOrgName(bgCtx, *detail.IssuerOrgID); err == nil {
					orgName = on
				}
			}

			docItems := make([]PrescriptionDocItem, 0, len(detail.Items))
			for _, it := range detail.Items {
				dur := 0
				if it.DurationDays != nil {
					dur = *it.DurationDays
				}
				docItems = append(docItems, PrescriptionDocItem{
					Name:         it.NameSnapshot,
					Presentation: "",
					DoseValue:    it.Dose.Value,
					DoseUnit:     it.Dose.Unit,
					Route:        it.Route,
					Frequency:    it.Frequency,
					DurationDays: dur,
					Instructions: it.Instructions,
					Controlled:   it.Controlled,
				})
			}

			issuedAtStr := now.Format("2006-01-02 15:04")
			if detail.IssuedAt != nil {
				issuedAtStr = detail.IssuedAt.Format("2006-01-02 15:04")
			}
			validUntilStr := ""
			if detail.ValidUntil != nil {
				validUntilStr = *detail.ValidUntil
			}

			signerName := cmd.SignerName
			if signerName == "" {
				signerName = "Médico Veterinario"
			}

			doc := PrescriptionDocument{
				ID:                 rxID.String(),
				Version:            ver,
				PetID:              detail.PetID.String(),
				PetName:            petName,
				PetSpecies:         petSpecies,
				GuardianID:         detail.GuardianID.String(),
				GuardianName:       guardianName,
				OrgID:              detail.IssuerOrgID.String(),
				OrgName:            orgName,
				VetName:            signerName,
				VetRegistry:        credRef.RegistryNumber,
				Country:            detail.Country,
				IssuedAt:           issuedAtStr,
				ValidUntil:         validUntilStr,
				RefillsAllowed:     detail.RefillsAllowed,
				HasItems:           len(docItems) > 0,
				HasControlled:      detail.HasControlled,
				Items:              docItems,
				ContentHash:        contentHash,
				SignatureAlgorithm: "HMAC-SHA256-RFC8785",
				SignatureAt:        issuedAtStr,
			}

			pdfKey, err := s.pdfGen.GenerateAndStore(bgCtx, doc)
			if err != nil {
				s.logger.Error("generate and store prescription pdf", "rx_id", rxID, "err", err)
				return
			}

			_ = s.queries.SetPDFObjectKey(bgCtx, rxID, pdfKey, int(ver))
		}(pID, newVersion)
	}

	return pID, nil
}

// RevokePrescription revoca una receta médica emitida (PP-F4-DOM-004, DOM-006).
func (s *Service) RevokePrescription(ctx context.Context, cmd RevokePrescriptionCmd, vetID uuid.UUID) error {
	return s.uow.Do(ctx, func(txCtx context.Context) error {
		rx, err := s.repo.Load(txCtx, cmd.PrescriptionID)
		if err != nil {
			return err
		}

		if _, err := rx.RevokePrescription(cmd.Reason, vetID, false); err != nil {
			return err
		}

		meta := eventstore.Metadata{
			ActorID: vetID.String(),
		}
		stored, err := s.repo.Save(txCtx, rx, meta)
		if err != nil {
			return err
		}
		if s.projects != nil {
			return s.projects.Apply(txCtx, stored)
		}
		return nil
	})
}

// UploadExternalPrescription registra la subida de una receta externa por el tutor (PP-F4-DOM-006).
func (s *Service) UploadExternalPrescription(ctx context.Context, cmd UploadExternalPrescriptionCmd, guardianID uuid.UUID) (uuid.UUID, error) {
	rxID := s.ids.NewID()
	if cmd.ID != nil && *cmd.ID != uuid.Nil {
		rxID = *cmd.ID
	}

	rx := domain.NewPrescription()
	if _, err := rx.UploadExternalPrescription(
		rxID,
		cmd.PetID,
		guardianID,
		cmd.Images,
		cmd.Country,
	); err != nil {
		return uuid.Nil, err
	}

	err := s.uow.Do(ctx, func(txCtx context.Context) error {
		meta := eventstore.Metadata{
			ActorID: guardianID.String(),
		}
		stored, err := s.repo.Save(txCtx, rx, meta)
		if err != nil {
			return err
		}
		if s.projects != nil {
			return s.projects.Apply(txCtx, stored)
		}
		return nil
	})
	if err != nil {
		return uuid.Nil, err
	}

	return rxID, nil
}

// ConfirmExternalPrescription confirma los datos de una receta externa validada (PP-F4-DOM-006).
func (s *Service) ConfirmExternalPrescription(ctx context.Context, cmd ConfirmExternalPrescriptionCmd, guardianID uuid.UUID) error {
	now := s.clock.Now().UTC()

	return s.uow.Do(ctx, func(txCtx context.Context) error {
		rx, err := s.repo.Load(txCtx, cmd.PrescriptionID)
		if err != nil {
			return err
		}

		rule, err := s.rules.ForCountry(txCtx, rx.Country)
		if err != nil {
			return err
		}

		val := domain.Validity{
			ValidUntil:     cmd.ValidUntil,
			RefillsAllowed: cmd.RefillsAllowed,
		}
		extMeta := domain.ExternalMeta{}
		diff := map[string]any{"notes": cmd.Notes}

		if _, err := rx.ConfirmExternalPrescription(cmd.Items, val, extMeta, diff, rule, now); err != nil {
			return err
		}

		meta := eventstore.Metadata{
			ActorID: guardianID.String(),
		}
		stored, err := s.repo.Save(txCtx, rx, meta)
		if err != nil {
			return err
		}
		if s.projects != nil {
			return s.projects.Apply(txCtx, stored)
		}
		return nil
	})
}

// RecordDispensation registra la dispensación parcial o total de la receta en farmacia (PP-F4-DOM-004, DOM-006).
func (s *Service) RecordDispensation(ctx context.Context, cmd RecordDispensationCmd, actorID uuid.UUID) error {
	now := s.clock.Now().UTC()
	orderID := s.ids.NewID()
	pharmacyOrgID := uuid.Nil
	if cmd.PartnerIntegrationID != nil {
		pharmacyOrgID = *cmd.PartnerIntegrationID
	}

	return s.uow.Do(ctx, func(txCtx context.Context) error {
		rx, err := s.repo.Load(txCtx, cmd.PrescriptionID)
		if err != nil {
			return err
		}

		if _, err := rx.RecordDispensation(orderID, pharmacyOrgID, cmd.Lines, now); err != nil {
			return err
		}

		meta := eventstore.Metadata{
			ActorID: actorID.String(),
		}
		stored, err := s.repo.Save(txCtx, rx, meta)
		if err != nil {
			return err
		}
		if s.projects != nil {
			return s.projects.Apply(txCtx, stored)
		}
		return nil
	})
}

// Archive archiva una receta de la vista del tutor (PP-F4-API-004).
func (s *Service) Archive(ctx context.Context, id uuid.UUID, guardianID uuid.UUID) error {
	return s.queries.Archive(ctx, id, guardianID)
}

// PublicVerify ejecuta la verificación pública recomputando el hash canónico del stream (PP-F4-SEC-006, SEC-007, ARQ-011).
func (s *Service) PublicVerify(ctx context.Context, prescriptionID uuid.UUID, givenHash string, ipAddr string) (*VerificationResult, error) {
	now := s.clock.Now().UTC()

	var ipHash []byte
	if ipAddr != "" {
		h := sha256.Sum256([]byte(ipAddr))
		ipHash = h[:]
	}
	_ = s.queries.RecordVerification(ctx, prescriptionID, ipHash, "public")

	detail, err := s.queries.GetByID(ctx, prescriptionID)
	if err != nil {
		if errors.Is(err, domain.ErrPrescriptionNotFound) {
			return &VerificationResult{
				PrescriptionID: prescriptionID,
				Valid:          false,
				Status:         "not_found",
				VerifiedAt:     now,
				ErrorReason:    "La receta médica no existe en el sistema.",
			}, nil
		}
		return nil, fmt.Errorf("get prescription: %w", err)
	}

	computedHash, err := s.repo.ContentHash(ctx, prescriptionID, detail.Version)
	if err != nil {
		computedHash = ""
	}

	result := &VerificationResult{
		PrescriptionID:     prescriptionID,
		Valid:              false,
		Status:             detail.Status,
		ContentHash:        computedHash,
		VerifiedAt:         now,
		Country:            detail.Country,
		ValidUntil:         detail.ValidUntil,
		RefillsAllowed:     detail.RefillsAllowed,
		RefillsUsed:        detail.RefillsUsed,
		Items:              detail.Items,
		DispensationsCount: len(detail.Dispensations),
	}

	if detail.IssuedAt != nil {
		result.IssuedAt = detail.IssuedAt
	}
	if detail.Credential != nil {
		result.VetRegistry = detail.Credential.RegistryNumber
	}

	if s.petReader != nil {
		if pn, ps, _, err := s.petReader.GetPetInfo(ctx, detail.PetID); err == nil {
			result.PetName = pn
			result.PetSpecies = ps
		}
	}
	if detail.IssuerOrgID != nil && s.orgReader != nil {
		if on, err := s.orgReader.GetOrgName(ctx, *detail.IssuerOrgID); err == nil {
			result.IssuerOrgName = on
		}
	}
	if detail.IssuerID != nil && s.userReader != nil {
		if un, err := s.userReader.GetUserName(ctx, *detail.IssuerID); err == nil {
			result.IssuerName = un
		}
	}

	if givenHash != "" && computedHash != "" {
		if !strings.EqualFold(strings.TrimPrefix(givenHash, "0x"), strings.TrimPrefix(computedHash, "0x")) {
			result.Valid = false
			result.ErrorReason = "La huella digital de la receta no coincide con el registro original."
			return result, nil
		}
	}

	switch detail.Status {
	case "issued", "confirmed":
		result.Valid = true
	case "revoked":
		result.Valid = false
		result.ErrorReason = "Esta receta ha sido formalmente revocada por el profesional emisor."
	case "expired":
		result.Valid = false
		result.ErrorReason = "Esta receta ha expirado y ya no tiene validez para dispensación."
	case "exhausted":
		result.Valid = false
		result.ErrorReason = "Esta receta ha agotado todas las dispensaciones permitidas."
	default:
		result.Valid = false
		result.ErrorReason = fmt.Sprintf("Estado no dispensable: %s", detail.Status)
	}

	return result, nil
}

// ExpirePrescriptions localiza recetas vencidas y emite PrescriptionExpired (PP-F4-DOM-004).
func (s *Service) ExpirePrescriptions(ctx context.Context) (int, error) {
	now := s.clock.Now().UTC()
	expiredIDs, err := s.queries.FindExpiredPrescriptions(ctx, now)
	if err != nil {
		return 0, err
	}

	count := 0
	for _, id := range expiredIDs {
		err := s.uow.Do(ctx, func(txCtx context.Context) error {
			rx, err := s.repo.Load(txCtx, id)
			if err != nil {
				return err
			}
			if _, err := rx.ExpirePrescription(now); err != nil {
				return nil
			}
			meta := eventstore.Metadata{
				ActorID: "system.scheduler",
			}
			stored, err := s.repo.Save(txCtx, rx, meta)
			if err != nil {
				return err
			}
			if s.projects != nil {
				return s.projects.Apply(txCtx, stored)
			}
			return nil
		})
		if err == nil {
			count++
		} else {
			s.logger.Warn("failed to expire prescription", "id", id, "err", err)
		}
	}

	return count, nil
}

// GetByID consulta el detalle de la receta en el read model.
func (s *Service) GetByID(ctx context.Context, id uuid.UUID) (*ReadPrescriptionDetail, error) {
	return s.queries.GetByID(ctx, id)
}

// ListByOrg lista recetas emitidas por una organización.
func (s *Service) ListByOrg(ctx context.Context, orgID uuid.UUID, status string, petID *uuid.UUID, issuerID *uuid.UUID, limit int) ([]ReadPrescriptionSummary, error) {
	return s.queries.ListByOrg(ctx, orgID, status, petID, issuerID, limit)
}

// ListByGuardian lista recetas para la vista del tutor.
func (s *Service) ListByGuardian(ctx context.Context, guardianID uuid.UUID, petID *uuid.UUID, status string, source string, limit int) ([]ReadPrescriptionSummary, error) {
	return s.queries.ListByGuardian(ctx, guardianID, petID, status, source, limit)
}

// ListByEncounter lista recetas asociadas a una consulta clínica firmada.
func (s *Service) ListByEncounter(ctx context.Context, encounterID uuid.UUID) ([]ReadPrescriptionSummary, error) {
	return s.queries.ListByEncounter(ctx, encounterID)
}

// HashIP es una función utilitaria para anonimizar IPs en registros de auditoría.
func HashIP(ip string) []byte {
	if ip == "" {
		return nil
	}
	h := sha256.Sum256([]byte(ip))
	return h[:]
}

// FormatHashHex formatea un hash en hexadecimal legible.
func FormatHashHex(b []byte) string {
	return hex.EncodeToString(b)
}
