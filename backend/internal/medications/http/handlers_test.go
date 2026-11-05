package http_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/edcastanohunter/petpassport/backend/internal/medications/app"
	"github.com/edcastanohunter/petpassport/backend/internal/medications/domain"
	medhttp "github.com/edcastanohunter/petpassport/backend/internal/medications/http"
	"github.com/edcastanohunter/petpassport/backend/internal/platform/auth"
	searchapp "github.com/edcastanohunter/petpassport/backend/internal/search/app"
)

type mockRepo struct {
	meds        map[uuid.UUID]domain.Medication
	suggestions map[uuid.UUID]domain.MedicationSuggestion
	batches     map[uuid.UUID]domain.ImportBatch
}

func newMockRepo() *mockRepo {
	return &mockRepo{
		meds:        make(map[uuid.UUID]domain.Medication),
		suggestions: make(map[uuid.UUID]domain.MedicationSuggestion),
		batches:     make(map[uuid.UUID]domain.ImportBatch),
	}
}

func (m *mockRepo) CreateMedication(ctx context.Context, actor uuid.UUID, med domain.Medication, reason string) (*domain.Medication, error) {
	if med.ID == uuid.Nil {
		med.ID = uuid.New()
	}
	med.Status = domain.StatusActive
	m.meds[med.ID] = med
	return &med, nil
}

func (m *mockRepo) UpdateMedication(ctx context.Context, actor uuid.UUID, med domain.Medication, reason string) (*domain.Medication, error) {
	m.meds[med.ID] = med
	return &med, nil
}

func (m *mockRepo) DiscontinueMedication(ctx context.Context, actor uuid.UUID, id uuid.UUID, reason string) error {
	med := m.meds[id]
	med.Status = domain.StatusDiscontinued
	m.meds[id] = med
	return nil
}

func (m *mockRepo) MergeMedication(ctx context.Context, actor uuid.UUID, sourceID, targetID uuid.UUID, reason string) error {
	src := m.meds[sourceID]
	src.Status = domain.StatusMerged
	src.MergedInto = &targetID
	m.meds[sourceID] = src
	return nil
}

func (m *mockRepo) GetMedication(ctx context.Context, id uuid.UUID) (*domain.Medication, *uuid.UUID, error) {
	med, ok := m.meds[id]
	if !ok {
		return nil, nil, nil
	}
	if med.Status == domain.StatusMerged && med.MergedInto != nil {
		target := m.meds[*med.MergedInto]
		return &target, &id, nil
	}
	return &med, nil, nil
}

func (m *mockRepo) ResolveActiveMedication(ctx context.Context, id uuid.UUID) (uuid.UUID, string, error) {
	med, ok := m.meds[id]
	if !ok {
		return uuid.Nil, "", nil
	}
	if med.Status == domain.StatusMerged && med.MergedInto != nil {
		target := m.meds[*med.MergedInto]
		return target.ID, target.Name, nil
	}
	return med.ID, med.Name, nil
}

func (m *mockRepo) ListMedications(ctx context.Context, filter app.MedicationFilter) ([]domain.Medication, error) {
	var list []domain.Medication
	for _, med := range m.meds {
		list = append(list, med)
	}
	return list, nil
}

func (m *mockRepo) CreateSuggestion(ctx context.Context, sug domain.MedicationSuggestion) (*domain.MedicationSuggestion, error) {
	if sug.ID == uuid.Nil {
		sug.ID = uuid.New()
	}
	sug.Status = domain.SuggestionPending
	m.suggestions[sug.ID] = sug
	return &sug, nil
}

func (m *mockRepo) ListSuggestions(ctx context.Context, orgID *uuid.UUID, status *string) ([]domain.MedicationSuggestion, error) {
	var list []domain.MedicationSuggestion
	for _, s := range m.suggestions {
		list = append(list, s)
	}
	return list, nil
}

func (m *mockRepo) ApproveSuggestion(ctx context.Context, actor uuid.UUID, suggestionID uuid.UUID, med domain.Medication) (*domain.Medication, error) {
	sug := m.suggestions[suggestionID]
	sug.Status = domain.SuggestionApproved
	m.suggestions[suggestionID] = sug
	return m.CreateMedication(ctx, actor, med, "approved suggestion")
}

func (m *mockRepo) RejectSuggestion(ctx context.Context, actor uuid.UUID, suggestionID uuid.UUID, reason string) error {
	sug := m.suggestions[suggestionID]
	sug.Status = domain.SuggestionRejected
	m.suggestions[suggestionID] = sug
	return nil
}

func (m *mockRepo) SaveImportBatch(ctx context.Context, batch domain.ImportBatch, rows []domain.ImportRow) error {
	m.batches[batch.ID] = batch
	return nil
}

func (m *mockRepo) GetImportBatch(ctx context.Context, id uuid.UUID) (*domain.ImportBatch, error) {
	b := m.batches[id]
	return &b, nil
}

func (m *mockRepo) RevertImportBatch(ctx context.Context, actor uuid.UUID, batchID uuid.UUID) error {
	b := m.batches[batchID]
	b.Status = "reverted"
	m.batches[batchID] = b
	return nil
}

func (m *mockRepo) UsageStats(ctx context.Context, from, to *time.Time) (*app.UsageStats, error) {
	return &app.UsageStats{
		ItemsTotal:    100,
		WithCatalogID: 85,
		Ratio:         0.85,
		Top: []app.MedicationUsage{
			{CatalogID: uuid.New(), Name: "Amoxicilina", Count: 50},
		},
	}, nil
}

func setupTestRouter(repo *mockRepo) (http.Handler, *app.CatalogService) {
	svc := app.NewCatalogService(repo)
	handler := medhttp.NewMedicationHandler(svc, nil)
	r := chi.NewRouter()
	handler.MountRoutes(r)
	return r, svc
}

func withVetContext(req *http.Request, orgID uuid.UUID) *http.Request {
	p := auth.Principal{
		UserID: uuid.New(),
		Memberships: []auth.Membership{
			{OrganizationID: orgID, OrgType: "clinic", Role: "veterinarian", OrgStatus: "active"},
		},
	}
	return req.WithContext(auth.WithPrincipal(req.Context(), p))
}

func withAdminContext(req *http.Request) *http.Request {
	p := auth.Principal{
		UserID:      uuid.New(),
		GlobalRoles: []string{"super_admin"},
	}
	return req.WithContext(auth.WithPrincipal(req.Context(), p))
}

func TestMedicationHandlers_CRUDAndMerge(t *testing.T) {
	repo := newMockRepo()
	router, _ := setupTestRouter(repo)

	// 1. Crear medicamento por SuperAdmin
	body, _ := json.Marshal(map[string]any{
		"name":    "Amoxicilina 500mg",
		"form":    "tablet",
		"species": []string{"dog", "cat"},
		"routes":  []string{"oral"},
		"reason":  "alta inicial",
	})
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/admin/catalogs/medications", bytes.NewReader(body))
	req = withAdminContext(req)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201 Created, got %d: %s", rec.Code, rec.Body.String())
	}

	var created domain.Medication
	_ = json.Unmarshal(rec.Body.Bytes(), &created)
	if created.Name != "Amoxicilina 500mg" {
		t.Fatalf("nombre incorrecto: %s", created.Name)
	}

	// 2. Consulta clínica por veterinario
	orgID := uuid.New()
	reqGet := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/catalogs/medications/"+created.ID.String(), nil)
	reqGet = withVetContext(reqGet, orgID)
	recGet := httptest.NewRecorder()
	router.ServeHTTP(recGet, reqGet)

	if recGet.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", recGet.Code)
	}

	// 3. Crear segundo medicamento y fusionar el primero en el segundo
	body2, _ := json.Marshal(map[string]any{
		"name":   "Amoxil 500mg",
		"form":   "tablet",
		"reason": "duplicado",
	})
	req2 := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/admin/catalogs/medications", bytes.NewReader(body2))
	req2 = withAdminContext(req2)
	rec2 := httptest.NewRecorder()
	router.ServeHTTP(rec2, req2)
	var created2 domain.Medication
	_ = json.Unmarshal(rec2.Body.Bytes(), &created2)

	mergeBody, _ := json.Marshal(map[string]any{
		"into":   created2.ID,
		"reason": "fusión de duplicados",
	})
	reqMerge := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/admin/catalogs/medications/"+created.ID.String()+"/merge", bytes.NewReader(mergeBody))
	reqMerge = withAdminContext(reqMerge)
	recMerge := httptest.NewRecorder()
	router.ServeHTTP(recMerge, reqMerge)

	if recMerge.Code != http.StatusOK {
		t.Fatalf("expected 200 OK en merge, got %d", recMerge.Code)
	}

	// 4. Consultar medicamento 1 debe redirigir al 2 (301 lógica: devuelve destino con merged_from)
	reqRedirect := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/catalogs/medications/"+created.ID.String(), nil)
	reqRedirect = withVetContext(reqRedirect, orgID)
	recRedirect := httptest.NewRecorder()
	router.ServeHTTP(recRedirect, reqRedirect)

	var redirResp struct {
		Data       domain.Medication `json:"data"`
		MergedFrom string            `json:"merged_from"`
	}
	_ = json.Unmarshal(recRedirect.Body.Bytes(), &redirResp)
	if redirResp.Data.ID != created2.ID || redirResp.MergedFrom != created.ID.String() {
		t.Fatalf("se esperaba redirección a %s con merged_from %s, got %+v", created2.ID, created.ID, redirResp)
	}
}

func TestMedicationHandlers_SuggestionsAndUsage(t *testing.T) {
	repo := newMockRepo()
	router, _ := setupTestRouter(repo)

	orgID := uuid.New()

	// 1. Veterinario sugiere medicamento
	sugBody, _ := json.Marshal(map[string]any{
		"organization_id": orgID,
		"name":            "Ketamina Vet 10%",
		"notes":           "Uso para sedación",
	})
	reqSug := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/catalogs/medications/suggestions", bytes.NewReader(sugBody))
	reqSug = withVetContext(reqSug, orgID)
	recSug := httptest.NewRecorder()
	router.ServeHTTP(recSug, reqSug)

	if recSug.Code != http.StatusCreated {
		t.Fatalf("expected 201 Created suggestion, got %d", recSug.Code)
	}

	var sug domain.MedicationSuggestion
	_ = json.Unmarshal(recSug.Body.Bytes(), &sug)

	// 2. Organización lista sus sugerencias
	reqList := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/organizations/"+orgID.String()+"/catalogs/medications/suggestions", nil)
	reqList = withVetContext(reqList, orgID)
	recList := httptest.NewRecorder()
	router.ServeHTTP(recList, reqList)

	if recList.Code != http.StatusOK {
		t.Fatalf("expected 200 OK list suggestions, got %d", recList.Code)
	}

	// 3. SuperAdmin aprueba la sugerencia
	appBody, _ := json.Marshal(map[string]any{
		"name":   "Ketamina Vet 10%",
		"form":   "injectable",
		"reason": "aprobación clínica",
	})
	reqApp := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/admin/catalogs/medications/suggestions/"+sug.ID.String()+"/approve", bytes.NewReader(appBody))
	reqApp = withAdminContext(reqApp)
	recApp := httptest.NewRecorder()
	router.ServeHTTP(recApp, reqApp)

	if recApp.Code != http.StatusOK {
		t.Fatalf("expected 200 OK approve suggestion, got %d: %s", recApp.Code, recApp.Body.String())
	}

	// 4. Métrica de uso
	reqUsage := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/admin/catalogs/medications/usage", nil)
	reqUsage = withAdminContext(reqUsage)
	recUsage := httptest.NewRecorder()
	router.ServeHTTP(recUsage, reqUsage)

	if recUsage.Code != http.StatusOK {
		t.Fatalf("expected 200 OK usage stats, got %d", recUsage.Code)
	}

	var stats searchapp.FreshnessInfo // test unmarshal dummy
	_ = json.Unmarshal(recUsage.Body.Bytes(), &stats)
}
