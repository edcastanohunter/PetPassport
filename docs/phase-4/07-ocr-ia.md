# F4 · 07 — OCR / IA: pipeline de extracción de recetas

Marco: ADR-019. Área `OCR`. Módulo `internal/ocr` (worker). Principio: **la IA propone, el humano confirma.**

## 1. Pipeline y estados

**[PP-F4-OCR-001] (MUST)**

```
ExternalPrescriptionUploaded
  └─ ocr:preprocess  (por página)  → imagen normalizada + miniatura + hash
  └─ ocr:extract     (documento)   → Extraction JSON (proveedor) [+ texto OCR clásico opcional]
  └─ ocr:match       (documento)   → candidatos de catálogo por ítem
  └─ comando RecordExtraction | RequestReupload (legibilidad baja) | marca failed/refused
```

- Cola asynq `ocr`, concurrencia 4 por réplica de worker, timeout 120 s por job, 3 reintentos (errores transitorios) y luego `failed` con notificación al tutor ("no pudimos analizarla; puedes capturarla manualmente").
- Cada etapa escribe en `read_prescriptions.extraction_jobs` (estado, métricas) y guarda artefactos en `rx/<prescription>/<attempt>/{page-N.webp, extraction.json, matches.json}`.
- `attempt` incrementa con cada `retry` (máx 3); el tutor puede capturar manualmente en cualquier momento (`confirm` sin extracción).

## 2. Preprocesado

**[PP-F4-OCR-002] (MUST)** Por página: decodificación segura (límite de píxeles), corrección de orientación (EXIF + detección de texto girado 90/180/270 mediante heurística de proyección), *deskew* (≤ 15°), recorte de bordes oscuros, normalización de contraste (CLAHE) y re-encode WebP 1600 px lado mayor (y 2400 px si la densidad de texto es alta), eliminación de metadatos; análisis de calidad (`blur` por varianza de Laplaciano, brillo, cobertura de texto estimada) → `quality_score`; si < umbral, `RequestReupload` **antes** de llamar al proveedor (ahorro y mejor UX).

## 3. Extractor principal (Claude)

**[PP-F4-OCR-003] (MUST)** Adaptador `ocr/infra/extractor/claude` con el SDK oficial de Go (`github.com/anthropics/anthropic-sdk-go`), a través de `platform/egress` (ARQ-006).

Parámetros de la llamada:

| Parámetro | Valor |
|---|---|
| `model` | `PP_OCR_MODEL`, por defecto `claude-opus-5-5`; `claude-sonnet-5-5` permitido para reducir coste (decisión por métricas de evaluación, OCR-007). Siempre el ID exacto, sin sufijos de fecha. |
| Contenido | Bloques `image` (base64, `image/webp` o `image/jpeg`) por página, en orden, seguidos de un bloque `text` con la instrucción; sin PII adicional del tutor. |
| `system` | Prompt versionado (`prompts/extract_prescription_v<N>.md`): rol (transcriptor de recetas veterinarias en español/inglés), reglas (no inventar; marcar ilegible; conservar abreviaturas en `raw_text`; normalizar unidades; una entrada por medicamento; no deducir dosis ausentes), glosario de abreviaturas veterinarias (SID/BID/TID, PO/SC/IM, c/8h…), ejemplos. Es contenido estable → candidato a `cache_control` (prompt caching) para reducir coste. |
| `output_config.format` | JSON Schema `PrescriptionExtraction` (sección 3.1) con `additionalProperties: false` en todos los objetos; sin restricciones numéricas en el esquema (se validan en Go). |
| Thinking | Adaptativo (por defecto en el modelo); `output_config.effort: "medium"` (configurable; evaluar `low` para coste). |
| `max_tokens` | 8.000 (salida típica < 1.500). |
| `fallbacks` | `"default"` con beta `server-side-fallback-2026-07-01` (ruta a un modelo alternativo ante rechazo por categoría de seguridad). |
| Streaming | No necesario (salida corta); request no-streaming con timeout 60 s. |

Manejo de la respuesta:

- Comprobar `stop_reason` **antes** de leer contenido: `refusal` → `extraction_jobs.status = refused`, contador `ocr_refusals_total`, el tutor recibe "captura manual"; `max_tokens` → reintento con `max_tokens` ×2 una vez; `end_turn` → parsear JSON del bloque de texto, validar contra el esquema (Go, `jsonschema`) y contra reglas de negocio (unidades conocidas, cantidades > 0, `confidence ∈ [0,1]`).
- Registrar `usage` (input/output tokens, cache reads) → `ocr.ai_usage_daily` y coste calculado con la tarifa configurada (`PP_OCR_PRICE_IN/OUT` por MTok; valores de referencia 2026-10: Opus 5.5 4 USD / 20 USD; Sonnet 5.5 2 USD / 10 USD).
- Errores: `429`/`5xx`/red → reintento asynq con backoff; `400` → `failed` (bug de esquema/prompt: alerta).
- Trazas OTel con `gen_ai.*` (modelo, tokens, latencia), sin contenido.

### 3.1 Esquema `PrescriptionExtraction` (resumen)

```jsonc
{ "type": "object", "additionalProperties": false,
  "required": ["legibility", "language", "header", "items", "warnings"],
  "properties": {
    "legibility": { "type": "number" },                       // 0–1
    "language": { "type": "string", "enum": ["es", "en", "other"] },
    "header": { "type": "object", "additionalProperties": false,
      "required": ["vet_name", "vet_registry", "clinic_name", "issued_on", "pet_name", "species"],
      "properties": {
        "vet_name":     { "$ref": "#/$defs/field_text" }, "vet_registry": { "$ref": "#/$defs/field_text" },
        "clinic_name":  { "$ref": "#/$defs/field_text" }, "issued_on":    { "$ref": "#/$defs/field_date" },
        "pet_name":     { "$ref": "#/$defs/field_text" }, "species":      { "$ref": "#/$defs/field_text" } } },
    "items": { "type": "array", "items": { "type": "object", "additionalProperties": false,
      "required": ["raw_text", "product", "dose", "route", "frequency", "duration", "quantity", "notes", "page", "bbox"],
      "properties": {
        "raw_text": { "type": "string" },
        "product":  { "$ref": "#/$defs/field_text" },          // nombre tal como aparece + normalizado
        "dose":     { "type": "object", "additionalProperties": false, "required": ["value", "unit", "confidence"],
                      "properties": { "value": { "type": ["number","null"] }, "unit": { "type": ["string","null"] }, "confidence": { "type": "number" } } },
        "route":    { "$ref": "#/$defs/field_enum_route" },
        "frequency":{ "type": "object", "additionalProperties": false, "required": ["every", "unit", "raw", "confidence"],
                      "properties": { "every": { "type": ["integer","null"] }, "unit": { "type": ["string","null"], "enum": ["hours","days",null] }, "raw": { "type": ["string","null"] }, "confidence": { "type": "number" } } },
        "duration": { "type": "object", "additionalProperties": false, "required": ["days", "confidence"],
                      "properties": { "days": { "type": ["integer","null"] }, "confidence": { "type": "number" } } },
        "quantity": { "type": "object", "additionalProperties": false, "required": ["value", "unit", "confidence"],
                      "properties": { "value": { "type": ["number","null"] }, "unit": { "type": ["string","null"] }, "confidence": { "type": "number" } } },
        "notes":    { "type": ["string","null"] },
        "page":     { "type": "integer" },
        "bbox":     { "type": ["array","null"], "items": { "type": "number" } }   // [x0,y0,x1,y1] relativo 0–1
      } } },
    "warnings": { "type": "array", "items": { "type": "string" } }
  },
  "$defs": {
    "field_text": { "type": "object", "additionalProperties": false, "required": ["value", "confidence"],
                    "properties": { "value": { "type": ["string","null"] }, "confidence": { "type": "number" } } },
    "field_date": { "type": "object", "additionalProperties": false, "required": ["value", "confidence"],
                    "properties": { "value": { "type": ["string","null"], "format": "date" }, "confidence": { "type": "number" } } },
    "field_enum_route": { "type": "object", "additionalProperties": false, "required": ["value", "confidence"],
                    "properties": { "value": { "type": ["string","null"], "enum": ["oral","sc","im","iv","topical","ophthalmic","otic","inhaled","rectal",null] }, "confidence": { "type": "number" } } }
  } }
```

El esquema vive en `backend/internal/ocr/schema/prescription_extraction.v1.json` y se versiona junto con el prompt (`extractor_id = "claude:<model>:prompt-v<N>:schema-v1"`).

## 4. Extractor de respaldo

**[PP-F4-OCR-004] (SHOULD)** `ocr/infra/extractor/tesseract` (idiomas `spa+eng`): produce `raw_text` por página y, con heurísticas (líneas con unidades/frecuencias), ítems con `confidence ≤ 0,4`. Se usa cuando `PP_OCR_PROVIDER=tesseract`, cuando el principal está con circuito abierto, o como texto de apoyo adjunto al principal (`RawText`). Nunca auto-selecciona candidatos.

## 5. Emparejamiento con el catálogo

**[PP-F4-OCR-005] (MUST)** Por ítem: consulta a OpenSearch `medications` (F3) con `product.value` (y `raw_text` como respaldo) + filtro por especie si se conoce; puntuación combinada = `0,6 × score_normalizado + 0,25 × coincidencia de concentración/forma detectadas + 0,15 × usage_count_normalizado`; top-3 candidatos. Auto-selección solo si `top1 ≥ 0,85` y `top2 < 0,6`; si el ítem es `controlled`, se marca para que la UI explique la exclusión. Sinónimos y abreviaturas de productos (`ref.medications.synonyms`) alimentan la búsqueda.

## 6. Umbrales y revisión

**[PP-F4-OCR-006] (MUST)**

| Condición | Acción |
|---|---|
| `quality_score < 0,35` (pre) o `legibility < 0,4` | `RequestReupload` con consejo específico (borrosa / oscura / recortada). |
| ítem con `product.confidence < 0,6` o sin candidato ≥ 0,5 | marcado "revisar" (ámbar) sin preselección. |
| campo numérico `confidence < 0,5` | campo vacío en la UI con el `raw` visible (nunca prellenar números dudosos). |
| `warnings` con "posible sustancia controlada" | aviso en UI. |
| 0 ítems detectados con `legibility ≥ 0,6` | se muestra "no encontramos medicamentos; añade manualmente". |

Los umbrales son configuración (`PP_OCR_THRESHOLDS`) y se ajustan con la evaluación.

## 7. Dataset y evaluación continua

**[PP-F4-OCR-007] (MUST)**

- Dataset `ocr.eval_samples`: ≥ 200 recetas al cierre de M4.1 (objetivo 500 en F5): sintéticas (plantillas impresas y manuscritas generadas con variaciones), donadas por clínicas piloto (anonimizadas: tachado de nombres de tutores) y de producción **solo con consentimiento explícito**. Etiquetas: cabecera e ítems canónicos.
- Harness `cmd/worker ocr-eval --dataset v<N> --extractor <id>`: métricas por campo (producto: emparejamiento correcto en top-1/top-3; dosis/frecuencia/duración/cantidad: exactitud con tolerancia; cabecera: exact match normalizado), F1 por campo, tasa de `reupload`, latencia, coste. Resultados en `ocr.eval_runs` y en el PR.
- CI: muestra de 30 (coste ≈ 1 USD) en PRs que toquen `internal/ocr`, prompts o esquema; nightly completo. **Gate**: ningún campo empeora > 2 puntos vs. la línea base; umbrales mínimos F1 producto ≥ 0,85, dosis/frecuencia ≥ 0,80.
- Reprocesos de dataset completos con **Message Batches** (50 % de coste) cuando cambia el extractor.
- Cada cambio de prompt es un PR con `extractor_id` nuevo y comparación; no se edita un prompt publicado.

## 8. Presupuesto y coste

**[PP-F4-OCR-008] (MUST)** `ocr.budget` mensual (por defecto 300 USD; alerta al 80 %; corte duro al 100 % → `AI_BUDGET_EXCEEDED` y captura manual); contador diario por modelo; coste estimado por receta mostrado en el monitor admin; `prompt caching` del `system` (reduce input repetido); elección de modelo por configuración con evaluación previa; límite de 3 intentos por receta y 20 recetas/día por tutor.

## 9. Privacidad y proveedor

**[PP-F4-OCR-009] (MUST)**

- Acuerdo de procesamiento de datos con el proveedor y revisión de su política de retención/entrenamiento antes de producción; opción `PP_OCR_PROVIDER=none` por entorno.
- No se envía al proveedor: nombre/contacto del tutor, historial, identificadores internos (salvo un `correlation_id` opaco en metadatos de la petición si el proveedor lo admite).
- Imágenes y artefactos con retención DAT-006; acceso a muestras en el monitor admin auditado y con motivo.
- Información al tutor en la UI: "La imagen se analiza con un servicio de IA de terceros; no se usa para entrenar modelos" (ajustar según el acuerdo vigente).

## 10. Métricas de precisión en producción

**[PP-F4-OCR-010] (MUST)** A partir de `ExternalPrescriptionConfirmed.diff`: por campo, % de ítems aceptados sin cambios, editados, eliminados y añadidos; por `extractor_id`; tendencia diaria (`ocr.metrics_daily`); alerta si la tasa de edición de `producto` supera el 30 % durante 3 días (posible regresión del catálogo o del prompt). Los diffs son también fuente de candidatos para el dataset (con consentimiento).

## 11. Ejemplo de llamada (Go, orientativo)

```go
// internal/ocr/infra/extractor/claude/extractor.go (esqueleto; detalles de tipos según el SDK vigente)
msg, err := e.client.Messages.New(ctx, anthropic.MessageNewParams{
    Model:     anthropic.Model(e.cfg.Model),            // "claude-opus-5-5"
    MaxTokens: 8000,
    System:    e.systemPrompt,                          // con cache_control
    Messages:  []anthropic.MessageParam{ anthropic.NewUserMessage(blocks...) }, // imágenes + instrucción
    OutputConfig: &anthropic.OutputConfigParam{ Effort: "medium", Format: e.jsonSchemaFormat },
    // fallbacks "default" + beta server-side-fallback-2026-07-01 según el SDK
})
if err != nil { return classify(err) }
switch msg.StopReason {
case anthropic.StopReasonRefusal:  return ErrRefused
case anthropic.StopReasonMaxTokens: return ErrTruncated
}
var out Extraction
if err := json.Unmarshal([]byte(firstText(msg.Content)), &out); err != nil { return ErrInvalidJSON }
return validate(out)
```

Los nombres exactos de tipos/campos se confirman contra el SDK en el momento de implementar (regla del proyecto: no adivinar firmas; compilar e iterar).
