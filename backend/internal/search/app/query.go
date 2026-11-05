package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/edcastanohunter/petpassport/backend/internal/platform/auth"
	"github.com/edcastanohunter/petpassport/backend/internal/search/domain"
)

var (
	metricSearchRequests = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "petpassport",
		Subsystem: "search",
		Name:      "requests_total",
		Help:      "Total de peticiones de búsqueda por índice y modo.",
	}, []string{"index", "mode"})

	metricPostfilterDropped = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "petpassport",
		Subsystem: "search",
		Name:      "postfilter_dropped_total",
		Help:      "Total de documentos descartados por el post-filtro de autorización.",
	})
)

// PetAccessLoader define la interfaz para resolver permisos de acceso de una mascota desde PostgreSQL/Caché.
type PetAccessLoader interface {
	GetPetAccess(ctx context.Context, petID uuid.UUID) (*domain.PetAccessRecord, error)
	GetPetIDsByOrg(ctx context.Context, orgID uuid.UUID) ([]uuid.UUID, error)
}

// FreshnessProvider provee el lag de indexación de búsqueda.
type FreshnessProvider interface {
	GetLagSeconds(ctx context.Context, index string) int64
}

// PatientSearchRequest contiene los parámetros de búsqueda de pacientes (PP-F3-API-001).
type PatientSearchRequest struct {
	OrgID   uuid.UUID
	Q       string
	Species string
	Status  string
	Cursor  string
	Size    int
}

// PatientSearchResultItem es el ítem devuelto en la búsqueda de pacientes.
type PatientSearchResultItem struct {
	Pet struct {
		ID              string  `json:"id"`
		Name            string  `json:"name"`
		Species         string  `json:"species"`
		Breed           string  `json:"breed,omitempty"`
		Sex             string  `json:"sex"`
		MicrochipSuffix string  `json:"microchip_suffix,omitempty"`
		Status          string  `json:"status"`
		PhotoThumbURL   *string `json:"photo_thumb_url,omitempty"`
	} `json:"pet"`
	GuardianDisplayName string            `json:"guardian_display_name,omitempty"`
	AccessLevel         string            `json:"access_level"` // linked | custody | guardian
	LastEncounterAt     *time.Time        `json:"last_encounter_at,omitempty"`
	Highlight           map[string]string `json:"highlight,omitempty"`
}

// FacetBucket representa una categoría y su conteo.
type FacetBucket struct {
	Key   string `json:"key"`
	Count int64  `json:"count"`
}

// PatientSearchResponse es la respuesta estructurada de búsqueda de pacientes.
type PatientSearchResponse struct {
	Data       []PatientSearchResultItem `json:"data"`
	Facets     map[string][]FacetBucket  `json:"facets"`
	NextCursor *string                   `json:"next_cursor,omitempty"`
	Freshness  FreshnessInfo             `json:"freshness"`
	Mode       string                    `json:"mode"` // full | degraded
}

// ClinicalSearchRequest contiene los parámetros de búsqueda en historiales clínicos (PP-F3-API-001).
type ClinicalSearchRequest struct {
	OrgID             uuid.UUID
	Q                 string
	Species           string
	Type              string // encounter_type
	ProfessionalID    *uuid.UUID
	From              *time.Time
	To                *time.Time
	ConfirmedOnly     bool
	OrganizationScope string // all | mine
	Cursor            string
	Size              int
}

// ClinicalHitItem representa una coincidencia clínica individual.
type ClinicalHitItem struct {
	DocKind       string                 `json:"doc_kind"`
	RefID         string                 `json:"ref_id"`
	OccurredAt    time.Time              `json:"occurred_at"`
	Organization  map[string]string      `json:"organization"` // {id, name}
	EncounterType string                 `json:"encounter_type"`
	Diagnoses     []domain.DiagnosisItem `json:"diagnoses,omitempty"`
	Highlight     map[string][]string    `json:"highlight,omitempty"`
}

// ClinicalSearchResultItem agrupa coincidencias clínicas por paciente.
type ClinicalSearchResultItem struct {
	Pet struct {
		ID      string `json:"id"`
		Name    string `json:"name"`
		Species string `json:"species"`
	} `json:"pet"`
	Hits []ClinicalHitItem `json:"hits"`
}

// ClinicalSearchResponse es la respuesta estructurada de búsqueda clínica.
type ClinicalSearchResponse struct {
	Data       []ClinicalSearchResultItem `json:"data"`
	Facets     map[string][]FacetBucket   `json:"facets"`
	NextCursor *string                    `json:"next_cursor,omitempty"`
	Freshness  FreshnessInfo              `json:"freshness"`
	Mode       string                     `json:"mode"`
}

// GuardianSearchRequest contiene los parámetros de búsqueda de un tutor en sus mascotas.
type GuardianSearchRequest struct {
	Q      string
	PetID  *uuid.UUID
	Kind   string
	From   *time.Time
	To     *time.Time
	Cursor string
	Size   int
}

// SuggestPatientItem representa un resultado de autocompletado rápido.
type SuggestPatientItem struct {
	PetID               string `json:"pet_id"`
	Name                string `json:"name"`
	Species             string `json:"species"`
	GuardianDisplayName string `json:"guardian_display_name,omitempty"`
	MicrochipSuffix     string `json:"microchip_suffix,omitempty"`
	AccessLevel         string `json:"access_level"`
}

// SuggestMedicationItem representa un medicamento sugerido con autocompletado (PP-F3-PRD-004, API-003).
type SuggestMedicationItem struct {
	CatalogID            string   `json:"catalog_id"`
	Name                 string   `json:"name"`
	ActiveIngredients    []string `json:"active_ingredients"`
	Form                 string   `json:"form"`
	Concentration        *string  `json:"concentration,omitempty"`
	Presentation         *string  `json:"presentation,omitempty"`
	Manufacturer         *string  `json:"manufacturer,omitempty"`
	Species              []string `json:"species,omitempty"`
	Routes               []string `json:"routes,omitempty"`
	PrescriptionRequired bool     `json:"prescription_required"`
	Controlled           bool     `json:"controlled"`
	DefaultRoute         *string  `json:"default_route,omitempty"`
	DefaultDoseUnit      *string  `json:"default_dose_unit,omitempty"`
}

// FreshnessInfo indica el retraso de indexación.
type FreshnessInfo struct {
	LagSeconds int64 `json:"lag_seconds"`
}

// QueryService implementa la búsqueda con OpenSearch, filtro obligatorio de acceso y post-filtro.
type QueryService struct {
	client      Client
	access      PetAccessLoader
	freshness   FreshnessProvider
	photoSigner func(key string) string
}

// NewQueryService crea una instancia de QueryService.
func NewQueryService(client Client, access PetAccessLoader, freshness FreshnessProvider, photoSigner func(key string) string) *QueryService {
	return &QueryService{
		client:      client,
		access:      access,
		freshness:   freshness,
		photoSigner: photoSigner,
	}
}

// accessFilter es la ÚNICA función que construye el filtro de autorización de OpenSearch (ARQ-002, SEC-001).
// Se aplica como `filter` de la consulta, por lo que no altera la puntuación (scoring).
func accessFilter(principal auth.Principal, orgID uuid.UUID, orgScope string, isSuperAdmin bool, reason string) (map[string]any, error) {
	if isSuperAdmin {
		if strings.TrimSpace(reason) == "" {
			return nil, domain.ErrReasonRequired
		}
		// SuperAdmin con motivo válido: sin filtro restrictivo
		return map[string]any{"match_all": map[string]any{}}, nil
	}

	// Si es consulta de tutor (individual)
	if orgID == uuid.Nil {
		if principal.UserID == uuid.Nil {
			return nil, fmt.Errorf("unauthenticated search")
		}
		return map[string]any{
			"term": map[string]any{
				"access.guardian_id": principal.UserID.String(),
			},
		}, nil
	}

	// Si es miembro clínico
	// Comprobar si la organización está suspendida
	for _, m := range principal.Memberships {
		if m.OrganizationID == orgID {
			if m.OrgStatus == "suspended" {
				return nil, domain.ErrOrgSuspended
			}
			break
		}
	}

	// Scope: solo mi clínica
	if orgScope == "mine" {
		return map[string]any{
			"bool": map[string]any{
				"should": []any{
					map[string]any{"term": map[string]any{"organization_id": orgID.String()}},
					map[string]any{"term": map[string]any{"access.org_ids_custody": orgID.String()}},
				},
				"minimum_should_match": 1,
			},
		}, nil
	}

	// Acceso normal de clínica: mascotas vinculadas O custodiadas
	return map[string]any{
		"bool": map[string]any{
			"should": []any{
				map[string]any{"term": map[string]any{"access.org_ids_linked": orgID.String()}},
				map[string]any{"term": map[string]any{"access.org_ids_custody": orgID.String()}},
			},
			"minimum_should_match": 1,
		},
	}, nil
}

// SearchPatients busca pacientes en OpenSearch aplicando accessFilter y post-filtro (ARQ-002).
func (s *QueryService) SearchPatients(ctx context.Context, req PatientSearchRequest, principal auth.Principal) (*PatientSearchResponse, error) {
	size := req.Size
	if size <= 0 || size > 50 {
		size = 20
	}

	filter, err := accessFilter(principal, req.OrgID, "", false, "")
	if err != nil {
		return nil, err
	}

	boolQuery := map[string]any{
		"filter": []any{filter},
	}

	filtersList := boolQuery["filter"].([]any)
	if req.Species != "" {
		filtersList = append(filtersList, map[string]any{"term": map[string]any{"species": req.Species}})
	}
	if req.Status != "" {
		filtersList = append(filtersList, map[string]any{"term": map[string]any{"status": req.Status}})
	} else {
		// Por defecto solo mascotas activas
		filtersList = append(filtersList, map[string]any{"term": map[string]any{"status": "active"}})
	}
	boolQuery["filter"] = filtersList

	q := strings.TrimSpace(req.Q)
	if q != "" {
		boolQuery["must"] = []any{
			map[string]any{
				"multi_match": map[string]any{
					"query":     q,
					"fields":    []string{"name^3", "name.suggest^2", "guardian_display_name^2", "microchip", "breed"},
					"fuzziness": "AUTO",
					"operator":  "and",
				},
			},
		}
	} else {
		boolQuery["must"] = []any{map[string]any{"match_all": map[string]any{}}}
	}

	searchBody := map[string]any{
		"query": map[string]any{"bool": boolQuery},
		"size":  size,
		"sort": []any{
			map[string]any{"_score": "desc"},
			map[string]any{"updated_at": "desc"},
		},
		"highlight": map[string]any{
			"pre_tags":  []string{"[[mark]]"},
			"post_tags": []string{"[[/mark]]"},
			"fields": map[string]any{
				"name":                  map[string]any{},
				"breed":                 map[string]any{},
				"guardian_display_name": map[string]any{},
			},
		},
		"aggs": map[string]any{
			"species": map[string]any{
				"terms": map[string]any{"field": "species", "size": 10},
			},
		},
	}

	if req.Cursor != "" {
		var sortValues []any
		if err := json.Unmarshal([]byte(req.Cursor), &sortValues); err == nil {
			searchBody["search_after"] = sortValues
		}
	}

	raw, err := s.client.Search(ctx, domain.IndexPets, searchBody)
	if err != nil {
		return nil, err
	}
	metricSearchRequests.WithLabelValues("pets", "full").Inc()

	var items []PatientSearchResultItem
	var lastSort []any

	for _, hit := range raw.Hits.Hits {
		var doc domain.PetDoc
		if err := json.Unmarshal(hit.Source, &doc); err != nil {
			continue
		}

		// Post-filtro: comprobación obligatoria contra PostgreSQL / Caché
		if s.access != nil {
			petUUID, err := uuid.Parse(doc.PetID)
			if err != nil {
				continue
			}
			accessRecord, err := s.access.GetPetAccess(ctx, petUUID)
			if err != nil || accessRecord == nil {
				metricPostfilterDropped.Inc()
				continue
			}
			allowed, _ := accessRecord.HasAccess(principal.UserID, req.OrgID)
			if !allowed {
				metricPostfilterDropped.Inc()
				continue
			}
		}

		accessLevel := "linked"
		if req.OrgID != uuid.Nil {
			isLinked := false
			for _, idStr := range doc.Access.OrgIDsLinked {
				if idStr == req.OrgID.String() {
					isLinked = true
					break
				}
			}
			if !isLinked {
				accessLevel = "custody"
			}
		} else {
			accessLevel = "guardian"
		}

		hlMap := make(map[string]string)
		for k, vList := range hit.Highlight {
			if len(vList) > 0 {
				hlMap[k] = vList[0]
			}
		}

		item := PatientSearchResultItem{
			GuardianDisplayName: doc.GuardianDisplayName,
			AccessLevel:         accessLevel,
			LastEncounterAt:     doc.LastEncounterAt,
			Highlight:           hlMap,
		}
		item.Pet.ID = doc.PetID
		item.Pet.Name = doc.Name
		item.Pet.Species = doc.Species
		item.Pet.Breed = doc.Breed
		item.Pet.Sex = doc.Sex
		item.Pet.MicrochipSuffix = doc.MicrochipSuffix
		item.Pet.Status = doc.Status

		items = append(items, item)
		lastSort = hit.Sort
	}

	facets := make(map[string][]FacetBucket)
	if len(raw.Aggregations) > 0 {
		var aggRaw struct {
			Species struct {
				Buckets []struct {
					Key      string `json:"key"`
					DocCount int64  `json:"doc_count"`
				} `json:"buckets"`
			} `json:"species"`
		}
		if err := json.Unmarshal(raw.Aggregations, &aggRaw); err == nil {
			var bList []FacetBucket
			for _, b := range aggRaw.Species.Buckets {
				bList = append(bList, FacetBucket{Key: b.Key, Count: b.DocCount})
			}
			facets["species"] = bList
		}
	}

	var nextCursor *string
	if len(lastSort) > 0 && len(items) == size {
		cursorBytes, _ := json.Marshal(lastSort)
		s := string(cursorBytes)
		nextCursor = &s
	}

	var lag int64
	if s.freshness != nil {
		lag = s.freshness.GetLagSeconds(ctx, domain.IndexPets)
	}

	return &PatientSearchResponse{
		Data:       items,
		Facets:     facets,
		NextCursor: nextCursor,
		Freshness:  FreshnessInfo{LagSeconds: lag},
		Mode:       "full",
	}, nil
}

// SearchClinical busca en historiales clínicos y carnet en OpenSearch (ARQ-002).
func (s *QueryService) SearchClinical(ctx context.Context, req ClinicalSearchRequest, principal auth.Principal) (*ClinicalSearchResponse, error) {
	size := req.Size
	if size <= 0 || size > 50 {
		size = 20
	}

	filter, err := accessFilter(principal, req.OrgID, req.OrganizationScope, false, "")
	if err != nil {
		return nil, err
	}

	boolQuery := map[string]any{
		"filter": []any{filter},
	}

	filtersList := boolQuery["filter"].([]any)
	if req.Species != "" {
		filtersList = append(filtersList, map[string]any{"term": map[string]any{"species": req.Species}})
	}
	if req.Type != "" {
		filtersList = append(filtersList, map[string]any{"term": map[string]any{"encounter_type": req.Type}})
	}
	if req.ProfessionalID != nil {
		filtersList = append(filtersList, map[string]any{"term": map[string]any{"professional_id": req.ProfessionalID.String()}})
	}
	if req.From != nil || req.To != nil {
		rangeMap := map[string]any{}
		if req.From != nil {
			rangeMap["gte"] = req.From.Format(time.RFC3339)
		}
		if req.To != nil {
			rangeMap["lte"] = req.To.Format(time.RFC3339)
		}
		filtersList = append(filtersList, map[string]any{"range": map[string]any{"occurred_at": rangeMap}})
	}

	q := strings.TrimSpace(req.Q)
	var mustList []any

	if q != "" {
		words := strings.Fields(q)
		operator := "or"
		if len(words) >= 3 {
			operator = "and"
		}

		mustList = append(mustList, map[string]any{
			"bool": map[string]any{
				"should": []any{
					map[string]any{
						"multi_match": map[string]any{
							"query":     q,
							"fields":    []string{"pet_name^2", "text", "medications.name", "procedures", "vaccine_product"},
							"fuzziness": "AUTO",
							"operator":  operator,
						},
					},
					map[string]any{
						"nested": map[string]any{
							"path": "diagnoses",
							"query": map[string]any{
								"bool": map[string]any{
									"must": func() []any {
										m := []any{
											map[string]any{
												"match": map[string]any{
													"diagnoses.text": map[string]any{
														"query":     q,
														"fuzziness": "AUTO",
													},
												},
											},
										}
										if req.ConfirmedOnly {
											m = append(m, map[string]any{"term": map[string]any{"diagnoses.certainty": "confirmed"}})
										}
										return m
									}(),
								},
							},
						},
					},
				},
				"minimum_should_match": 1,
			},
		})
	} else {
		mustList = append(mustList, map[string]any{"match_all": map[string]any{}})
	}

	boolQuery["must"] = mustList
	boolQuery["filter"] = filtersList

	searchBody := map[string]any{
		"query": map[string]any{"bool": boolQuery},
		"size":  size,
		"sort": []any{
			map[string]any{"occurred_at": "desc"},
			map[string]any{"_score": "desc"},
		},
		"highlight": map[string]any{
			"pre_tags":            []string{"[[mark]]"},
			"post_tags":           []string{"[[/mark]]"},
			"fragment_size":       150,
			"number_of_fragments": 3,
			"fields": map[string]any{
				"text":           map[string]any{},
				"diagnoses.text": map[string]any{},
			},
		},
		"aggs": map[string]any{
			"species":        map[string]any{"terms": map[string]any{"field": "species", "size": 10}},
			"encounter_type": map[string]any{"terms": map[string]any{"field": "encounter_type", "size": 10}},
			"organization":   map[string]any{"terms": map[string]any{"field": "organization_name", "size": 10}},
			"year":           map[string]any{"terms": map[string]any{"field": "year", "size": 5}},
		},
	}

	raw, err := s.client.Search(ctx, domain.IndexClinical, searchBody)
	if err != nil {
		return nil, err
	}
	metricSearchRequests.WithLabelValues("clinical", "full").Inc()

	// Agrupar resultados por paciente (PP-F3-API-001)
	grouped := make(map[string]*ClinicalSearchResultItem)
	var orderedPetIDs []string
	var lastSort []any

	for _, hit := range raw.Hits.Hits {
		var doc domain.ClinicalDoc
		if err := json.Unmarshal(hit.Source, &doc); err != nil {
			continue
		}

		// Post-filtro contra PetAccess
		if s.access != nil {
			petUUID, err := uuid.Parse(doc.PetID)
			if err != nil {
				continue
			}
			accessRecord, err := s.access.GetPetAccess(ctx, petUUID)
			if err != nil || accessRecord == nil {
				metricPostfilterDropped.Inc()
				continue
			}
			allowed, _ := accessRecord.HasAccess(principal.UserID, req.OrgID)
			if !allowed {
				metricPostfilterDropped.Inc()
				continue
			}
		}

		group, exists := grouped[doc.PetID]
		if !exists {
			group = &ClinicalSearchResultItem{}
			group.Pet.ID = doc.PetID
			group.Pet.Name = doc.PetName
			group.Pet.Species = doc.Species
			grouped[doc.PetID] = group
			orderedPetIDs = append(orderedPetIDs, doc.PetID)
		}

		hlMap := make(map[string][]string)
		for k, vList := range hit.Highlight {
			hlMap[k] = vList
		}

		hitItem := ClinicalHitItem{
			DocKind:       doc.DocKind,
			RefID:         hit.ID,
			OccurredAt:    doc.OccurredAt,
			Organization:  map[string]string{"id": doc.OrganizationID, "name": doc.OrganizationName},
			EncounterType: doc.EncounterType,
			Diagnoses:     doc.Diagnoses,
			Highlight:     hlMap,
		}

		group.Hits = append(group.Hits, hitItem)
		lastSort = hit.Sort
	}

	var data []ClinicalSearchResultItem
	for _, petID := range orderedPetIDs {
		data = append(data, *grouped[petID])
	}

	facets := make(map[string][]FacetBucket)
	if len(raw.Aggregations) > 0 {
		var aggRaw map[string]struct {
			Buckets []struct {
				Key   any   `json:"key"`
				Count int64 `json:"doc_count"`
			} `json:"buckets"`
		}
		if err := json.Unmarshal(raw.Aggregations, &aggRaw); err == nil {
			for name, agg := range aggRaw {
				var bList []FacetBucket
				for _, b := range agg.Buckets {
					bList = append(bList, FacetBucket{Key: fmt.Sprint(b.Key), Count: b.Count})
				}
				facets[name] = bList
			}
		}
	}

	var nextCursor *string
	if len(lastSort) > 0 && len(data) == size {
		cursorBytes, _ := json.Marshal(lastSort)
		s := string(cursorBytes)
		nextCursor = &s
	}

	var lag int64
	if s.freshness != nil {
		lag = s.freshness.GetLagSeconds(ctx, domain.IndexClinical)
	}

	return &ClinicalSearchResponse{
		Data:       data,
		Facets:     facets,
		NextCursor: nextCursor,
		Freshness:  FreshnessInfo{LagSeconds: lag},
		Mode:       "full",
	}, nil
}

// SuggestPatients proporciona autocompletado rápido tolerante a errores (PP-F3-API-002).
func (s *QueryService) SuggestPatients(ctx context.Context, orgID uuid.UUID, q string, principal auth.Principal) ([]SuggestPatientItem, error) {
	q = strings.TrimSpace(q)
	if len(q) < 2 {
		return []SuggestPatientItem{}, nil
	}

	filter, err := accessFilter(principal, orgID, "", false, "")
	if err != nil {
		return nil, err
	}

	// Match phrase prefix / edge ngram sobre name y guardian_display_name
	searchBody := map[string]any{
		"query": map[string]any{
			"bool": map[string]any{
				"must": []any{
					map[string]any{
						"multi_match": map[string]any{
							"query":  q,
							"fields": []string{"name.suggest^3", "name", "guardian_display_name.suggest^2", "microchip"},
							"type":   "phrase_prefix",
						},
					},
				},
				"filter": []any{
					filter,
					map[string]any{"term": map[string]any{"status": "active"}},
				},
			},
		},
		"size": 8,
	}

	raw, err := s.client.Search(ctx, domain.IndexPets, searchBody)
	if err != nil {
		return nil, err
	}

	var items []SuggestPatientItem
	for _, hit := range raw.Hits.Hits {
		var doc domain.PetDoc
		if err := json.Unmarshal(hit.Source, &doc); err != nil {
			continue
		}

		// Post-filtro rápido
		if s.access != nil {
			petUUID, err := uuid.Parse(doc.PetID)
			if err != nil {
				continue
			}
			accessRecord, err := s.access.GetPetAccess(ctx, petUUID)
			if err != nil || accessRecord == nil {
				continue
			}
			allowed, _ := accessRecord.HasAccess(principal.UserID, orgID)
			if !allowed {
				continue
			}
		}

		accessLevel := "linked"
		if orgID != uuid.Nil {
			isLinked := false
			for _, idStr := range doc.Access.OrgIDsLinked {
				if idStr == orgID.String() {
					isLinked = true
					break
				}
			}
			if !isLinked {
				accessLevel = "custody"
			}
		} else {
			accessLevel = "guardian"
		}

		items = append(items, SuggestPatientItem{
			PetID:               doc.PetID,
			Name:                doc.Name,
			Species:             doc.Species,
			GuardianDisplayName: doc.GuardianDisplayName,
			MicrochipSuffix:     doc.MicrochipSuffix,
			AccessLevel:         accessLevel,
		})
	}

	return items, nil
}

// SuggestMedications autocompleta medicamentos desde OpenSearch con boost de usage_count (PP-F3-PRD-004, API-003).
func (s *QueryService) SuggestMedications(ctx context.Context, q string, species string, limit int) ([]SuggestMedicationItem, error) {
	if limit <= 0 || limit > 50 {
		limit = 10
	}
	cleanQ := strings.TrimSpace(q)

	filters := []any{
		map[string]any{"term": map[string]any{"status": "active"}},
	}
	if species != "" {
		filters = append(filters, map[string]any{
			"bool": map[string]any{
				"should": []any{
					map[string]any{"term": map[string]any{"species": species}},
					map[string]any{"bool": map[string]any{"must_not": map[string]any{"exists": map[string]any{"field": "species"}}}},
				},
				"minimum_should_match": 1,
			},
		})
	}

	var queryClause any
	if cleanQ != "" {
		queryClause = map[string]any{
			"multi_match": map[string]any{
				"query":  cleanQ,
				"fields": []string{"name^3", "name.suggest^3", "active_ingredients^2", "active_ingredients.suggest^2", "synonyms"},
			},
		}
	} else {
		queryClause = map[string]any{"match_all": map[string]any{}}
	}

	searchBody := map[string]any{
		"size": limit,
		"query": map[string]any{
			"function_score": map[string]any{
				"query": map[string]any{
					"bool": map[string]any{
						"must":   queryClause,
						"filter": filters,
					},
				},
				"field_value_factor": map[string]any{
					"field":    "usage_count",
					"factor":   1.2,
					"modifier": "log1p",
					"missing":  0,
				},
				"boost_mode": "sum",
			},
		},
	}

	resp, err := s.client.Search(ctx, domain.IndexMedications, searchBody)
	if err != nil {
		return nil, fmt.Errorf("search medications suggest: %w", err)
	}

	var items []SuggestMedicationItem
	for _, hit := range resp.Hits.Hits {
		var doc domain.MedicationDoc
		if err := json.Unmarshal(hit.Source, &doc); err != nil {
			continue
		}
		items = append(items, SuggestMedicationItem{
			CatalogID:            doc.CatalogID,
			Name:                 doc.Name,
			ActiveIngredients:    doc.ActiveIngredients,
			Form:                 doc.Form,
			Concentration:        doc.Concentration,
			Presentation:         doc.Presentation,
			Manufacturer:         doc.Manufacturer,
			Species:              doc.Species,
			Routes:               doc.Routes,
			PrescriptionRequired: doc.PrescriptionRequired,
			Controlled:           doc.Controlled,
		})
	}

	return items, nil
}
