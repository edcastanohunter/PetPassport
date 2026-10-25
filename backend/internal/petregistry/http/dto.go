package http

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"time"

	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/edcastanohunter/petpassport/backend/api/gen"
	"github.com/edcastanohunter/petpassport/backend/internal/petregistry/app"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/problem"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/storage"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/storage/image"
	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
)

// presigner firma URLs de foto (nil = sin storage; sin campo photo).
type presigner interface{ Presign(ctx context.Context, key string) (string, error) }

type minioPresigner struct{ o storage.ObjectStorage }

func (m minioPresigner) Presign(ctx context.Context, key string) (string, error) {
	return m.o.PresignGet(ctx, key, photoURLTTL)
}

func (h *PetHandlers) presign() presigner {
	if h.objects == nil {
		return nil
	}
	return minioPresigner{o: h.objects}
}

// toPet mapea la vista al DTO del contrato.
func toPet(v app.PetView, p presigner) gen.Pet {
	pet := gen.Pet{
		Id:         openapi_types.UUID(v.ID),
		Version:    v.Version,
		GuardianId: openapi_types.UUID(v.GuardianID),
		Name:       v.Name,
		Species:    gen.Species(v.Species),
		Sex:        gen.Sex(v.Sex),
		Neutered:   gen.Neutered(v.Neutered),
		Status:     gen.PetStatus(v.Status),
		CareLinks:  make([]gen.CareLink, 0, len(v.ActiveLinks)),
		CreatedAt:  v.CreatedAt,
		UpdatedAt:  v.UpdatedAt,
	}
	if v.SpeciesOther != "" {
		pet.SpeciesOther = &v.SpeciesOther
	}
	if v.Breed != "" {
		pet.Breed = &v.Breed
	}
	if v.Markings != "" {
		pet.Markings = &v.Markings
	}
	if v.Microchip != "" {
		pet.Microchip = &v.Microchip
	}
	if v.BirthDate != nil {
		pet.BirthDate = &gen.BirthDate{Date: openapiDate(v.BirthDate.Date.String()), Approximate: v.BirthDate.Approximate}
		years, months := app.Age(v.BirthDate.Date, kernel.CivilDateOf(time.Now()))
		pet.Age = &struct {
			Months int `json:"months"`
			Years  int `json:"years"`
		}{Months: months, Years: years}
	}
	if v.DeceasedAt != nil {
		pet.DeceasedAt = &openapi_types.Date{Time: v.DeceasedAt.Time()}
	}
	if v.Photo != nil && p != nil {
		fullURL, err := p.Presign(context.Background(), v.Photo.ObjectKey)
		thumbURL, err2 := p.Presign(context.Background(), v.Photo.ThumbKey)
		if err == nil && err2 == nil {
			pet.Photo = &gen.Photo{
				Url: fullURL, ThumbUrl: thumbURL,
				ExpiresAt: time.Now().Add(photoURLTTL),
			}
		}
	}
	for _, l := range v.ActiveLinks {
		link := gen.CareLink{
			OrganizationId: openapi_types.UUID(l.OrganizationID),
			DisplayName:    l.DisplayName,
			Status:         gen.CareLinkStatus(l.Status),
			OrgSuspended:   l.OrgSuspended,
			GrantedAt:      l.GrantedAt,
		}
		if l.City != "" {
			link.City = &l.City
		}
		if l.RevokedAt != nil {
			link.RevokedAt = l.RevokedAt
		}
		pet.CareLinks = append(pet.CareLinks, link)
	}
	return pet
}

// toListItem mapea la fila de lista al DTO.
func toListItem(it app.PetListItem) gen.PetListItem {
	item := gen.PetListItem{
		Id:          openapi_types.UUID(it.ID),
		Version:     it.Version,
		Name:        it.Name,
		Species:     gen.Species(it.Species),
		Status:      gen.PetListItemStatus(it.Status),
		ActiveLinks: it.Links,
		CreatedAt:   it.CreatedAt,
	}
	if it.Breed != "" {
		item.Breed = &it.Breed
	}
	if it.BirthDate != nil {
		item.BirthDate = &gen.BirthDate{Date: openapiDate(it.BirthDate.Date.String()), Approximate: it.BirthDate.Approximate}
	}
	return item
}

// firstFile extrae el primer archivo del multipart (SEC-004: el nombre de
// archivo del cliente se ignora; el Content-Type se ignora).
func firstFile(r *multipart.Reader) (io.Reader, error) {
	if r == nil {
		return nil, image.ErrIncompleteBody
	}
	for {
		part, err := r.NextPart()
		if err == io.EOF {
			return nil, image.ErrIncompleteBody
		}
		if err != nil {
			return nil, fmt.Errorf("photo: multipart: %w", err)
		}
		if part.FileName() != "" {
			return part, nil
		}
	}
}

// photoProblem mapea errores del pipeline al catálogo.
func photoProblem(err error) error {
	var p *problem.Problem
	switch {
	case errors.Is(err, image.ErrTooLarge):
		p = problem.New(problem.PayloadTooLarge, "Archivo demasiado grande", "La foto no puede superar 5 MB.")
	case errors.Is(err, image.ErrUnsupported), errors.Is(err, image.ErrNotAnImage):
		p = problem.New(problem.UnsupportedMedia, "Formato no soportado", "Formatos admitidos: JPEG, PNG, WebP, HEIC.")
	case errors.Is(err, image.ErrTooManyPixels):
		p = problem.New(problem.UnsupportedMedia, "Imagen demasiado grande", "La imagen supera el máximo de píxeles.")
	case errors.Is(err, image.ErrIncompleteBody):
		p = problem.New(problem.ValidationFailed, "Datos inválidos", "Falta el archivo.")
	default:
		return err
	}
	return &problem.DirectError{P: p}
}

// openapiDate convierte YYYY-MM-DD al tipo del DTO generado.
func openapiDate(s string) openapi_types.Date {
	t, _ := time.Parse("2006-01-02", s)
	return openapi_types.Date{Time: t}
}

func ptr[T any](v T) *T { return &v }

// bytesReader adapta un []byte a io.Reader para storage.Put.
func bytesReader(b []byte) io.Reader { return &sliceReader{b: b} }

type sliceReader struct {
	b []byte
	i int
}

func (s *sliceReader) Read(p []byte) (int, error) {
	if s.i >= len(s.b) {
		return 0, io.EOF
	}
	n := copy(p, s.b[s.i:])
	s.i += n
	return n, nil
}

// cursor: base64 JSON de (created_at, id) — opaco para el cliente.
func encodeCursor(c app.PetCursor) string {
	raw, _ := json.Marshal(map[string]any{"t": c.CreatedAt.UTC().Format(time.RFC3339Nano), "id": c.ID.String()})
	return base64.RawURLEncoding.EncodeToString(raw)
}

func decodeCursor(s string) (app.PetCursor, error) {
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return app.PetCursor{}, err
	}
	var m struct {
		T  string `json:"t"`
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &m); err != nil {
		return app.PetCursor{}, err
	}
	t, err := time.Parse(time.RFC3339Nano, m.T)
	if err != nil {
		return app.PetCursor{}, err
	}
	id, err := uuid.Parse(m.ID)
	if err != nil {
		return app.PetCursor{}, err
	}
	return app.PetCursor{CreatedAt: t, ID: id}, nil
}

// replayedCreatePetResponse reproduce la respuesta almacenada con la cabecera
// Idempotent-Replayed: true (ARQ-008).
type replayedCreatePetResponse struct {
	status int
	body   []byte
}

func (r replayedCreatePetResponse) VisitCreatePetResponse(w http.ResponseWriter) error {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Idempotent-Replayed", "true")
	w.WriteHeader(r.status)
	_, err := w.Write(r.body)
	return err
}

var _ gen.CreatePetResponseObject = replayedCreatePetResponse{}
