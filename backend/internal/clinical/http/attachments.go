package http

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/edcastanohunter/petpassport/backend/api/gen"
	"github.com/edcastanohunter/petpassport/backend/internal/clinical/app"
	"github.com/edcastanohunter/petpassport/backend/internal/clinical/domain"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/authz"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/problem"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/storage"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/storage/document"
	"github.com/edcastanohunter/petpassport/backend/internal/shared/kernel"
)

// upload es el formulario multipart ya leído.
type upload struct {
	file        []byte
	description string
	visible     bool
}

// maxFieldBytes acota los campos de texto del formulario.
const maxFieldBytes = 1 << 10

// readUpload lee las partes `file`, `description` y `visible_to_guardian`
// (visible por defecto, PRD-004). El archivo se acota a 10 MB + 1 byte para
// detectar el exceso.
func readUpload(r *multipart.Reader) (upload, error) {
	u := upload{visible: true}
	if r == nil {
		return u, document.ErrEmpty
	}
	for {
		part, err := r.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			return u, fmt.Errorf("attachment: multipart: %w", err)
		}
		switch part.FormName() {
		case "file":
			data, err := io.ReadAll(io.LimitReader(part, document.MaxBytes+1))
			if err != nil {
				return u, fmt.Errorf("attachment: leer archivo: %w", err)
			}
			u.file = data
		case "description":
			data, err := io.ReadAll(io.LimitReader(part, maxFieldBytes))
			if err != nil {
				return u, err
			}
			u.description = strings.TrimSpace(string(data))
		case "visible_to_guardian":
			data, err := io.ReadAll(io.LimitReader(part, 16))
			if err != nil {
				return u, err
			}
			if v, err := strconv.ParseBool(strings.TrimSpace(string(data))); err == nil {
				u.visible = v
			}
		}
		_ = part.Close()
	}
	if len(u.file) == 0 {
		return u, document.ErrEmpty
	}
	return u, nil
}

// documentProblem mapea los errores de validación al catálogo (413/415/422).
func documentProblem(err error) error {
	var p *problem.Problem
	switch {
	case errors.Is(err, document.ErrTooLarge):
		p = problem.New(problem.PayloadTooLarge, "Archivo demasiado grande", "El adjunto no puede superar 10 MB.")
	case errors.Is(err, document.ErrUnsupported):
		p = problem.New(problem.UnsupportedMedia, "Formato no soportado", "Formatos admitidos: JPEG, PNG, WebP y PDF.")
	case errors.Is(err, document.ErrTooManyPixels):
		p = problem.New(problem.UnsupportedMedia, "Imagen demasiado grande", "La imagen supera el máximo de píxeles.")
	case errors.Is(err, document.ErrInvalidPDF):
		p = problem.New(problem.UnsupportedMedia, "PDF inválido", "El PDF no es válido.")
	case errors.Is(err, document.ErrUnsafePDF):
		p = problem.New(problem.UnsupportedMedia, "PDF no admitido", "El PDF contiene JavaScript, formularios, cifrado o archivos embebidos.")
	case errors.Is(err, document.ErrTooManyPages):
		p = problem.New(problem.ValidationFailed, "PDF demasiado largo", "El PDF no puede superar 50 páginas.")
		p.Errors = []problem.FieldError{{Field: "file", Code: "TOO_MANY_PAGES"}}
	case errors.Is(err, document.ErrEmpty):
		p = problem.New(problem.ValidationFailed, "Datos inválidos", "Falta el archivo.")
		p.Errors = []problem.FieldError{{Field: "file", Code: "REQUIRED"}}
	default:
		return err
	}
	return &problem.DirectError{P: p}
}

func storageUnavailable() error {
	return &problem.DirectError{P: problem.New(problem.Unavailable, "No disponible", "El almacenamiento de adjuntos no está configurado.")}
}

// UploadEncounterAttachment sube un adjunto (PRD-004, ARQ-011). En borrador
// se incorpora al cuerpo; en una consulta firmada queda pendiente para la
// próxima adenda (E9), y solo puede subirlo quien puede adendar.
func (h *ClinicalHandlers) UploadEncounterAttachment(ctx context.Context, req gen.UploadEncounterAttachmentRequestObject) (gen.UploadEncounterAttachmentResponseObject, error) {
	p, err := h.principal(ctx)
	if err != nil {
		return nil, err
	}
	id := uuid.UUID(req.EncounterId)
	acc, res, err := h.encounter(ctx, id)
	if err != nil {
		return nil, err
	}
	signed := acc.Status == string(domain.StatusSigned)
	action := authz.EncounterEdit
	if signed {
		action = authz.EncounterSign
	}
	if err := authorize(p, action, res); err != nil {
		return nil, err
	}
	if h.Objects == nil {
		return nil, storageUnavailable()
	}
	if signed && h.Uploads != nil {
		pending, err := h.Uploads.Pending(ctx, id)
		if err != nil {
			return nil, err
		}
		if len(pending) >= domain.MaxAddendumAttach {
			return nil, domain.ErrAttachmentLimit
		}
	}
	form, err := readUpload(req.Body)
	if err != nil {
		return nil, documentProblem(err)
	}
	doc, err := document.Process(ctx, bytes.NewReader(form.file))
	if err != nil {
		return nil, documentProblem(err)
	}
	attID, err := uuid.NewV7()
	if err != nil {
		return nil, err
	}
	att := domain.Attachment{
		ID: attID, ObjectKey: domain.AttachmentKey(acc.PetID, id, attID),
		ContentType: domain.ContentType(doc.Kind), SizeBytes: int64(len(doc.Data)),
		Description: form.description, VisibleToGuardian: form.visible,
	}
	if err := h.Objects.Put(ctx, att.ObjectKey, doc.Kind.MIME(), bytes.NewReader(doc.Data), att.SizeBytes); err != nil {
		return nil, err
	}
	now := h.now().UTC()
	if signed {
		err = h.Uploads.Stage(ctx, app.StagedUpload{Attachment: att, EncounterID: id, UploadedBy: p.UserID, CreatedAt: now})
	} else {
		_, err = h.Commands.Dispatch(h.meta(ctx, p, "", 0), domain.AddAttachment{EncounterID: id, Attachment: att})
	}
	if err != nil {
		if derr := h.Objects.Delete(ctx, att.ObjectKey); derr != nil {
			h.Logger.WarnContext(ctx, "clinical: objeto huérfano tras fallo", "encounter_id", id, "error", derr)
		}
		return nil, mapErr(err)
	}
	dto := toAttachment(app.AttachmentView{
		ID: att.ID, ContentType: string(att.ContentType), SizeBytes: att.SizeBytes,
		Description: att.Description, VisibleToGuardian: att.VisibleToGuardian, CreatedAt: now,
	})
	if signed {
		dto.PendingAddendum = ptr(true)
	}
	return gen.UploadEncounterAttachment201JSONResponse(dto), nil
}

// findAttachment busca el adjunto en el read model o, para el personal, entre
// los pendientes de adenda.
func (h *ClinicalHandlers) findAttachment(ctx context.Context, encounterID, attID uuid.UUID, staff bool) (app.AttachmentView, bool, error) {
	atts, err := h.Queries.Attachments(ctx, encounterID)
	if err != nil {
		return app.AttachmentView{}, false, err
	}
	for _, a := range atts {
		if a.ID == attID {
			return a, false, nil
		}
	}
	if staff && h.Uploads != nil {
		s, err := h.Uploads.Get(ctx, encounterID, attID)
		if err == nil {
			return stagedView(s), true, nil
		}
		var nf *kernel.NotFoundError
		if !errors.As(err, &nf) {
			return app.AttachmentView{}, false, err
		}
	}
	return app.AttachmentView{}, false, &kernel.NotFoundError{Resource: "attachment"}
}

// GetEncounterAttachment redirige a una URL prefirmada de 15 min (descarga
// auditada; tutor solo adjuntos visibles, SEC-003).
func (h *ClinicalHandlers) GetEncounterAttachment(ctx context.Context, req gen.GetEncounterAttachmentRequestObject) (gen.GetEncounterAttachmentResponseObject, error) {
	p, err := h.principal(ctx)
	if err != nil {
		return nil, err
	}
	acc, res, err := h.load(ctx, p, uuid.UUID(req.EncounterId), authz.EncounterRead)
	if err != nil {
		return nil, err
	}
	guardian := p.UserID == acc.GuardianID
	a, _, err := h.findAttachment(ctx, acc.ID, uuid.UUID(req.AttachmentId), !guardian)
	if err != nil {
		return nil, err
	}
	if guardian && !a.VisibleToGuardian {
		return nil, &kernel.NotFoundError{Resource: "attachment"}
	}
	if h.Objects == nil {
		return nil, storageUnavailable()
	}
	org := actingOrg(p, append([]uuid.UUID{acc.OrganizationID}, res.LinkedOrgIDs...)...)
	if err := h.audit(ctx, p, acc.GuardianID, org, acc.PetID, "attachment", a.ID); err != nil {
		return nil, err
	}
	url, err := h.presignAttachment(ctx, a)
	if err != nil {
		return nil, err
	}
	if wantsJSON(req.Params.Accept) {
		return gen.GetEncounterAttachment200JSONResponse{Url: url, ExpiresAt: h.now().Add(presignTTL)}, nil
	}
	return gen.GetEncounterAttachment302Response{Headers: gen.GetEncounterAttachment302ResponseHeaders{Location: &url}}, nil
}

func (h *ClinicalHandlers) presignAttachment(ctx context.Context, a app.AttachmentView) (string, error) {
	kind := document.Kind(a.ContentType)
	if dp, ok := h.Objects.(storage.DownloadPresigner); ok {
		return dp.PresignDownload(ctx, a.ObjectKey, presignTTL, a.ID.String()+"."+extension(kind), kind.MIME())
	}
	return h.Objects.PresignGet(ctx, a.ObjectKey, presignTTL)
}

func extension(k document.Kind) string {
	switch k {
	case document.KindJPEG:
		return "jpg"
	case document.KindPNG:
		return "png"
	case document.KindPDF:
		return "pdf"
	}
	return "bin"
}

// SetEncounterAttachmentVisibility cambia la visibilidad para el tutor
// (ocultar exige motivo; queda en el evento, SEC-003).
func (h *ClinicalHandlers) SetEncounterAttachmentVisibility(ctx context.Context, req gen.SetEncounterAttachmentVisibilityRequestObject) (gen.SetEncounterAttachmentVisibilityResponseObject, error) {
	p, err := h.principal(ctx)
	if err != nil {
		return nil, err
	}
	id, attID := uuid.UUID(req.EncounterId), uuid.UUID(req.AttachmentId)
	if _, _, err := h.load(ctx, p, id, authz.EncounterClinicalEdit); err != nil {
		return nil, err
	}
	cmd := domain.SetTutorVisibility{EncounterID: id, AttachmentID: attID}
	if req.Body != nil {
		cmd.Visible = req.Body.VisibleToGuardian
		cmd.Reason = strOr(req.Body.Reason)
	}
	if _, err := h.Commands.Dispatch(h.meta(ctx, p, "", 0), cmd); err != nil {
		return nil, mapErr(err)
	}
	a, _, err := h.findAttachment(ctx, id, attID, true)
	if err != nil {
		return nil, err
	}
	return gen.SetEncounterAttachmentVisibility200JSONResponse(toAttachment(a)), nil
}

// DeleteEncounterAttachment quita un adjunto del borrador o uno pendiente de
// adenda en una consulta firmada.
func (h *ClinicalHandlers) DeleteEncounterAttachment(ctx context.Context, req gen.DeleteEncounterAttachmentRequestObject) (gen.DeleteEncounterAttachmentResponseObject, error) {
	p, err := h.principal(ctx)
	if err != nil {
		return nil, err
	}
	id, attID := uuid.UUID(req.EncounterId), uuid.UUID(req.AttachmentId)
	acc, res, err := h.encounter(ctx, id)
	if err != nil {
		return nil, err
	}
	var key string
	if acc.Status == string(domain.StatusSigned) {
		if err := authorize(p, authz.EncounterSign, res); err != nil {
			return nil, err
		}
		s, err := h.Uploads.Get(ctx, id, attID)
		if err != nil {
			return nil, err
		}
		if err := h.Uploads.Delete(ctx, id, []uuid.UUID{attID}); err != nil {
			return nil, err
		}
		key = s.Attachment.ObjectKey
	} else {
		if err := authorize(p, authz.EncounterEdit, res); err != nil {
			return nil, err
		}
		if _, err := h.Commands.Dispatch(h.meta(ctx, p, "", 0), domain.RemoveAttachment{EncounterID: id, AttachmentID: attID}); err != nil {
			return nil, mapErr(err)
		}
		key = domain.AttachmentKey(acc.PetID, id, attID)
	}
	if h.Objects != nil {
		if err := h.Objects.Delete(ctx, key); err != nil {
			h.Logger.WarnContext(ctx, "clinical: no se pudo borrar el objeto del adjunto", "encounter_id", id, "error", err)
		}
	}
	return gen.DeleteEncounterAttachment204Response{}, nil
}
