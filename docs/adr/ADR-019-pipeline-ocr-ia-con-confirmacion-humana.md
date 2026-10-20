# ADR-019: Extracción de recetas con modelo de visión y salida estructurada, confianza por campo, confirmación humana obligatoria y proveedor intercambiable

- Estado: Aceptada
- Fecha: 2026-10-20
- Fase: F4

## Contexto

El tutor fotografía una receta (manuscrita o impresa, en español, con abreviaturas veterinarias) y el
sistema debe convertirla en ítems estructurados emparejados con el catálogo para pedir cotizaciones. La
precisión nunca será del 100 %; un error (dosis, producto) tiene consecuencias clínicas. La solución debe
ser auditable, barata por documento y sustituible si cambia el proveedor.

## Decisión

1. **Pipeline en el worker** (cola asynq `ocr`, ADR-014): `uploaded → preprocessed → extracted → matched →
   needs_review → confirmed | rejected`. Cada etapa es idempotente y deja artefactos (imagen normalizada,
   JSON de extracción, candidatos de emparejamiento) para auditoría y reprocesado.
2. **Extracción con un modelo multimodal y salida estructurada**: puerto `DocumentExtractor` con adaptador
   principal **Claude** (`claude-opus-5-5` por defecto; `claude-sonnet-5-5` configurable por entorno para
   bajar coste) vía Anthropic Go SDK: imagen en `base64` + instrucciones, respuesta constreñida por
   **`output_config.format`** con el JSON Schema `PrescriptionExtraction` (`additionalProperties: false`),
   thinking adaptativo con `effort: medium`, `fallbacks: "default"` activado, y comprobación de
   `stop_reason` (`refusal` / `max_tokens`) antes de usar el contenido. Adaptador secundario: OCR clásico
   (Tesseract + heurísticas) solo como *fallback* de disponibilidad y para extraer texto bruto de apoyo.
3. **Confianza por campo**: el esquema exige `confidence` (0–1) por ítem y por campo clave (producto, dosis,
   frecuencia, duración, cantidad), más `legibility` global y `warnings[]`. Umbrales: ítem < 0,6 → se marca
   "revisar"; documento con `legibility < 0,4` → se pide nueva foto.
4. **Emparejamiento** con el catálogo (OpenSearch `medications`, ADR-013/016): top-3 candidatos por ítem con
   puntuación; auto-selección solo si el primero supera 0,85 y el segundo < 0,6; si no, el tutor elige.
5. **Confirmación humana obligatoria**: nada se cotiza sin que el tutor revise y confirme ítem a ítem (ve la
   imagen y la extracción lado a lado); la confirmación se registra como evento con el *diff* respecto a la
   extracción (base de la métrica de precisión).
6. **Privacidad y proveedor**: las imágenes se envían al proveedor sin PII adicional (no se envía nombre del
   tutor; el nombre del animal y del veterinario pueden aparecer en la imagen y se aceptan); acuerdo de
   procesamiento de datos y configuración de retención revisados antes de producción; `PP_OCR_PROVIDER=none`
   deshabilita la extracción automática (solo captura manual) en entornos que lo requieran. Las imágenes se
   retienen 90 días tras la confirmación (configurable) y luego se borran salvo las vinculadas a pedidos.
7. **Evaluación continua**: *dataset* de recetas anonimizadas (≥ 200, con etiquetas) y harness de evaluación
   por campo (exact match / tolerancia numérica); se ejecuta en CI con muestra pequeña y en nightly completo;
   cambios de prompt/modelo exigen igual o mejor F1 por campo. Reprocesos masivos vía **Message Batches**
   (50 % de coste) cuando se cambia la versión del extractor.

## Consecuencias

- Coste estimado por receta con Opus 5.5: una imagen (~1.500 tokens) + prompt (~1.000) + salida (~600) ≈
  0,02–0,03 USD; con Sonnet 5.5 ≈ la mitad. Presupuesto mensual con alerta.
- La IA nunca decide sola: la confirmación del tutor y la validación del farmacéutico son el control.
- El puerto permite cambiar de proveedor (Google Document AI, Azure, modelos locales) sin tocar el dominio.

## Alternativas consideradas

- **OCR clásico + reglas**: insuficiente con manuscritos y abreviaturas; se mantiene como fallback.
- **Extracción sin confirmación cuando la confianza es alta**: descartada en F4 por riesgo clínico; se
  reconsidera con métricas reales en F5 solo para recetas `issued` (que ya son estructuradas y no necesitan OCR).
