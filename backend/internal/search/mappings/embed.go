package mappings

import (
	"embed"
	"encoding/json"
	"fmt"
	"strings"
)

//go:embed *.json analyzers/*
var FS embed.FS

// GetMapping devuelve el mapping JSON para el alias dado.
// Si el entorno no soporta synonyms_path (por ejemplo en OpenSearch local sin volumen montado
// o simulado), puede reemplazar synonyms_path con la lista inline de sinónimos.
func GetMapping(alias string, inlineSynonyms bool) ([]byte, error) {
	filename := alias + ".json"
	data, err := FS.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("reading mapping file %s: %w", filename, err)
	}

	if !inlineSynonyms {
		return data, nil
	}

	// Cargar sinónimos
	synonymsData, err := FS.ReadFile("analyzers/vet_synonyms_es.txt")
	if err != nil {
		return data, nil
	}

	lines := strings.Split(string(synonymsData), "\n")
	var synonyms []string
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l != "" && !strings.HasPrefix(l, "#") {
			synonyms = append(synonyms, l)
		}
	}

	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}

	// Si contiene filter.vet_synonyms, reemplazar synonyms_path por synonyms array
	if settings, ok := m["settings"].(map[string]any); ok {
		if analysis, ok := settings["analysis"].(map[string]any); ok {
			if filter, ok := analysis["filter"].(map[string]any); ok {
				if vetSyn, ok := filter["vet_synonyms"].(map[string]any); ok {
					delete(vetSyn, "synonyms_path")
					vetSyn["synonyms"] = synonyms
				}
			}
		}
	}

	return json.Marshal(m)
}

// GetSynonyms devuelve el contenido del archivo de sinónimos veterinarios.
func GetSynonyms() (string, error) {
	data, err := FS.ReadFile("analyzers/vet_synonyms_es.txt")
	if err != nil {
		return "", err
	}
	return string(data), nil
}
