package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"
)

// MockClient es una implementación en memoria de Client para pruebas unitarias y golden queries.
type MockClient struct {
	mu      sync.RWMutex
	indices map[string]map[string]any // indexName -> docID -> raw doc
	versions map[string]map[string]int64
	aliases map[string][]string       // alias -> []indexName
}

var _ Client = (*MockClient)(nil)

// NewMockClient crea un nuevo MockClient en memoria.
func NewMockClient() *MockClient {
	m := &MockClient{
		indices:  make(map[string]map[string]any),
		versions: make(map[string]map[string]int64),
		aliases:  make(map[string][]string),
	}
	return m
}

func (m *MockClient) Ping(ctx context.Context) error {
	return nil
}

func (m *MockClient) IndexExists(ctx context.Context, index string) (bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, ok := m.indices[index]
	return ok, nil
}

func (m *MockClient) CreateIndex(ctx context.Context, index string, body []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.indices[index]; ok {
		return fmt.Errorf("index %s already exists", index)
	}
	m.indices[index] = make(map[string]any)
	m.versions[index] = make(map[string]int64)
	return nil
}

func (m *MockClient) DeleteIndex(ctx context.Context, index string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.indices, index)
	delete(m.versions, index)
	return nil
}

func (m *MockClient) GetAliases(ctx context.Context) (map[string][]string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make(map[string][]string)
	for a, idxs := range m.aliases {
		out[a] = append([]string{}, idxs...)
	}
	return out, nil
}

func (m *MockClient) UpdateAliases(ctx context.Context, actions []AliasAction) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, act := range actions {
		switch act.Type {
		case "add":
			m.aliases[act.Alias] = append(m.aliases[act.Alias], act.Index)
		case "remove":
			var filtered []string
			for _, idx := range m.aliases[act.Alias] {
				if idx != act.Index {
					filtered = append(filtered, idx)
				}
			}
			m.aliases[act.Alias] = filtered
		}
	}
	return nil
}

func (m *MockClient) resolveIndex(target string) []string {
	if idxs, ok := m.aliases[target]; ok && len(idxs) > 0 {
		return idxs
	}
	return []string{target}
}

func (m *MockClient) Bulk(ctx context.Context, items []BulkOperation) (*BulkResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	resp := &BulkResponse{Took: 1, Errors: false}

	for _, item := range items {
		targetIndices := m.resolveIndex(item.Index)
		action := item.Action
		if action == "" {
			action = "index"
		}

		for _, physIndex := range targetIndices {
			if _, ok := m.indices[physIndex]; !ok {
				m.indices[physIndex] = make(map[string]any)
				m.versions[physIndex] = make(map[string]int64)
			}

			currVersion := m.versions[physIndex][item.ID]
			if item.VersionType == "external_gte" && item.Version > 0 && item.Version < currVersion {
				// Conflicto de versión
				resp.Errors = true
				resp.Items = append(resp.Items, map[string]struct {
					Index   string `json:"_index"`
					ID      string `json:"_id"`
					Version int64  `json:"_version"`
					Result  string `json:"result"`
					Status  int    `json:"status"`
					Error   *struct {
						Type   string `json:"type"`
						Reason string `json:"reason"`
					} `json:"error,omitempty"`
				}{
					action: {
						Index:   physIndex,
						ID:      item.ID,
						Version: currVersion,
						Status:  409,
						Error: &struct {
							Type   string `json:"type"`
							Reason string `json:"reason"`
						}{Type: "version_conflict_engine_exception", Reason: "version conflict"},
					},
				})
				continue
			}

			if action == "delete" {
				delete(m.indices[physIndex], item.ID)
				delete(m.versions[physIndex], item.ID)
			} else {
				m.indices[physIndex][item.ID] = item.Document
				m.versions[physIndex][item.ID] = item.Version
			}

			resp.Items = append(resp.Items, map[string]struct {
				Index   string `json:"_index"`
				ID      string `json:"_id"`
				Version int64  `json:"_version"`
				Result  string `json:"result"`
				Status  int    `json:"status"`
				Error   *struct {
					Type   string `json:"type"`
					Reason string `json:"reason"`
				} `json:"error,omitempty"`
			}{
				action: {
					Index:   physIndex,
					ID:      item.ID,
					Version: item.Version,
					Result:  "created",
					Status:  200,
				},
			})
		}
	}

	return resp, nil
}

func (m *MockClient) Search(ctx context.Context, index string, query map[string]any) (*SearchRawResult, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	targetIndices := m.resolveIndex(index)
	rawResult := &SearchRawResult{Took: 1}

	// Recoger todos los documentos de los índices físicos
	var allDocs []struct {
		id  string
		doc map[string]any
	}

	for _, phys := range targetIndices {
		if store, ok := m.indices[phys]; ok {
			for id, doc := range store {
				docBytes, _ := json.Marshal(doc)
				var docMap map[string]any
				_ = json.Unmarshal(docBytes, &docMap)
				allDocs = append(allDocs, struct {
					id  string
					doc map[string]any
				}{id: id, doc: docMap})
			}
		}
	}

	// Extraer filtros y términos
	filterGuardianID, filterOrgID, qStr, filterSpecies, filterStatus := parseSearchQuery(query)

	var matchedHits []struct {
		Index     string          `json:"_index"`
		ID        string          `json:"_id"`
		Score     *float64        `json:"_score"`
		Source    json.RawMessage `json:"_source"`
		Highlight map[string][]string `json:"highlight,omitempty"`
		Sort      []any           `json:"sort,omitempty"`
	}

	speciesCounts := make(map[string]int64)

	for _, item := range allDocs {
		d := item.doc

		// 1. Filtrar por status
		if filterStatus != "" {
			if st, ok := d["status"].(string); ok && st != filterStatus {
				continue
			}
		}

		// 2. Filtrar por especie
		if filterSpecies != "" {
			if sp, ok := d["species"].(string); ok && sp != filterSpecies {
				continue
			}
		}

		// 3. Comprobar accessFilter
		if filterGuardianID != "" {
			acc, _ := d["access"].(map[string]any)
			if acc == nil || acc["guardian_id"] != filterGuardianID {
				continue
			}
		}
		if filterOrgID != "" {
			acc, _ := d["access"].(map[string]any)
			allowed := false
			if acc != nil {
				if linked, ok := acc["org_ids_linked"].([]any); ok {
					for _, l := range linked {
						if fmt.Sprint(l) == filterOrgID {
							allowed = true
							break
						}
					}
				}
				if !allowed {
					if custody, ok := acc["org_ids_custody"].([]any); ok {
						for _, c := range custody {
							if fmt.Sprint(c) == filterOrgID {
								allowed = true
								break
							}
						}
					}
				}
			}
			if !allowed {
				continue
			}
		}

		// 4. Búsqueda de texto (Q)
		score := 1.0
		hl := make(map[string][]string)

		if qStr != "" {
			matched := false
			qLower := normalizeForSearch(qStr)

			// Buscar en name
			if name, ok := d["name"].(string); ok {
				nameNorm := normalizeForSearch(name)
				if strings.Contains(nameNorm, qLower) || matchFuzzy(nameNorm, qLower) {
					matched = true
					score += 3.0
					hl["name"] = []string{highlightWord(name, qStr)}
				}
			}
			// Buscar en microchip
			if mc, ok := d["microchip"].(string); ok && strings.Contains(mc, qStr) {
				matched = true
				score += 2.0
			}
			// Buscar en breed
			if breed, ok := d["breed"].(string); ok {
				if strings.Contains(normalizeForSearch(breed), qLower) {
					matched = true
					score += 1.5
				}
			}
			// Buscar en text
			if text, ok := d["text"].(string); ok {
				textNorm := normalizeForSearch(text)
				if strings.Contains(textNorm, qLower) || matchSynonyms(textNorm, qLower) {
					matched = true
					score += 1.0
					hl["text"] = []string{highlightWord(text, qStr)}
				}
			}
			// Buscar en diagnósticos
			if diagList, ok := d["diagnoses"].([]any); ok {
				for _, diagItem := range diagList {
					if dm, ok := diagItem.(map[string]any); ok {
						if dt, ok := dm["text"].(string); ok {
							dtNorm := normalizeForSearch(dt)
							if strings.Contains(dtNorm, qLower) || matchSynonyms(dtNorm, qLower) || matchFuzzy(dtNorm, qLower) {
								matched = true
								score += 4.0
								hl["diagnoses.text"] = []string{highlightWord(dt, qStr)}
							}
						}
					}
				}
			}

			if !matched {
				continue
			}
		}

		if sp, ok := d["species"].(string); ok {
			speciesCounts[sp]++
		}

		sourceBytes, _ := json.Marshal(d)
		hitScore := score
		matchedHits = append(matchedHits, struct {
			Index     string          `json:"_index"`
			ID        string          `json:"_id"`
			Score     *float64        `json:"_score"`
			Source    json.RawMessage `json:"_source"`
			Highlight map[string][]string `json:"highlight,omitempty"`
			Sort      []any           `json:"sort,omitempty"`
		}{
			Index:     targetIndices[0],
			ID:        item.id,
			Score:     &hitScore,
			Source:    sourceBytes,
			Highlight: hl,
			Sort:      []any{score, time.Now().Unix()},
		})
	}

	size := 20
	if s, ok := query["size"].(int); ok && s > 0 {
		size = s
	}

	rawResult.Hits.Total.Value = int64(len(matchedHits))
	if len(matchedHits) > size {
		matchedHits = matchedHits[:size]
	}
	rawResult.Hits.Hits = matchedHits

	// Facetas
	var buckets []map[string]any
	for k, count := range speciesCounts {
		buckets = append(buckets, map[string]any{"key": k, "doc_count": count})
	}
	aggBytes, _ := json.Marshal(map[string]any{
		"species": map[string]any{"buckets": buckets},
	})
	rawResult.Aggregations = aggBytes

	return rawResult, nil
}

func (m *MockClient) UpdateByQuery(ctx context.Context, index string, query map[string]any, script map[string]any) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	targetIndices := m.resolveIndex(index)
	var updated int64

	// Extraer pet_id del query
	var targetPetID string
	if term, ok := query["term"].(map[string]any); ok {
		if pid, ok := term["pet_id"].(string); ok {
			targetPetID = pid
		}
	}

	newAccess, _ := script["params"].(map[string]any)["access"]

	for _, phys := range targetIndices {
		if store, ok := m.indices[phys]; ok {
			for _, doc := range store {
				docBytes, _ := json.Marshal(doc)
				var docMap map[string]any
				_ = json.Unmarshal(docBytes, &docMap)
				if pid, ok := docMap["pet_id"].(string); ok && pid == targetPetID {
					docMap["access"] = newAccess
					m.indices[phys][docMap["pet_id"].(string)] = docMap
					updated++
				}
			}
		}
	}

	return updated, nil
}

func (m *MockClient) Delete(ctx context.Context, index, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, phys := range m.resolveIndex(index) {
		delete(m.indices[phys], id)
		delete(m.versions[phys], id)
	}
	return nil
}

func (m *MockClient) CatIndices(ctx context.Context) ([]IndexStat, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var stats []IndexStat
	for idx, docs := range m.indices {
		stats = append(stats, IndexStat{
			Index:     idx,
			Health:    "green",
			Status:    "open",
			DocsCount: fmt.Sprint(len(docs)),
			StoreSize: "10kb",
		})
	}
	return stats, nil
}

// Helpers para normalización, sinónimos y búsqueda tolerante a errores
func normalizeForSearch(s string) string {
	s = strings.ToLower(s)
	replacer := strings.NewReplacer(
		"á", "a", "é", "e", "í", "i", "ó", "o", "ú", "u", "ü", "u",
		"ñ", "n",
	)
	return replacer.Replace(s)
}

func matchFuzzy(text, query string) bool {
	// Simple Levenshtein distance check or prefix match
	if strings.HasPrefix(text, query) || strings.HasPrefix(query, text) {
		return true
	}
	return levenshteinDistance(text, query) <= 2
}

func levenshteinDistance(a, b string) int {
	if len(a) == 0 {
		return len(b)
	}
	if len(b) == 0 {
		return len(a)
	}
	d := make([][]int, len(a)+1)
	for i := range d {
		d[i] = make([]int, len(b)+1)
		d[i][0] = i
	}
	for j := range d[0] {
		d[0][j] = j
	}
	for i := 1; i <= len(a); i++ {
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			d[i][j] = min(
				d[i-1][j]+1,
				d[i][j-1]+1,
				d[i-1][j-1]+cost,
			)
		}
	}
	return d[len(a)][len(b)]
}

func min(a, b, c int) int {
	if a < b {
		if a < c {
			return a
		}
		return c
	}
	if b < c {
		return b
	}
	return c
}

var knownSynonyms = map[string][]string{
	"vomito":      {"emesis", "vomitos"},
	"emesis":      {"vomito", "vomitos"},
	"picazon":     {"prurito", "rascado"},
	"prurito":     {"picazon", "rascado"},
	"rabia":       {"antirrabica"},
	"antirrabica": {"rabia"},
	"diarrea":     {"heces blandas", "enteritis"},
}

func matchSynonyms(text, query string) bool {
	syns, ok := knownSynonyms[query]
	if !ok {
		return false
	}
	for _, syn := range syns {
		if strings.Contains(text, syn) {
			return true
		}
	}
	return false
}

func highlightWord(text, word string) string {
	idx := strings.Index(strings.ToLower(text), strings.ToLower(word))
	if idx >= 0 {
		return text[:idx] + "[[mark]]" + text[idx:idx+len(word)] + "[[/mark]]" + text[idx+len(word):]
	}
	return text
}

func parseSearchQuery(query map[string]any) (guardianID, orgID, qStr, species, status string) {
	b, ok := query["query"].(map[string]any)["bool"].(map[string]any)
	if !ok {
		return
	}

	if filters, ok := b["filter"].([]any); ok {
		for _, f := range filters {
			fm, ok := f.(map[string]any)
			if !ok {
				continue
			}
			if term, ok := fm["term"].(map[string]any); ok {
				if g, ok := term["access.guardian_id"].(string); ok {
					guardianID = g
				}
				if sp, ok := term["species"].(string); ok {
					species = sp
				}
				if st, ok := term["status"].(string); ok {
					status = st
				}
			}
			if boolFilter, ok := fm["bool"].(map[string]any); ok {
				if should, ok := boolFilter["should"].([]any); ok {
					for _, sh := range should {
						if shTerm, ok := sh.(map[string]any)["term"].(map[string]any); ok {
							if o, ok := shTerm["access.org_ids_linked"].(string); ok {
								orgID = o
							}
						}
					}
				}
			}
		}
	}

	if mustList, ok := b["must"].([]any); ok {
		for _, m := range mustList {
			if mm, ok := m.(map[string]any)["multi_match"].(map[string]any); ok {
				if q, ok := mm["query"].(string); ok {
					qStr = q
				}
			}
			if boolM, ok := m.(map[string]any)["bool"].(map[string]any); ok {
				if shouldList, ok := boolM["should"].([]any); ok {
					for _, sh := range shouldList {
						if mm, ok := sh.(map[string]any)["multi_match"].(map[string]any); ok {
							if q, ok := mm["query"].(string); ok {
								qStr = q
							}
						}
					}
				}
			}
		}
	}

	return
}
