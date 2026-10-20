# ADR-018: `Prescription` como agregado con dos orígenes (emitida por veterinario / externa subida por el tutor); validez, sustancias controladas y verificación

- Estado: Aceptada
- Fecha: 2026-10-20
- Fase: F4

## Contexto

Una receta es un documento con valor legal distinto a un plan de tratamiento (F2). La farmacia necesita saber
quién la emitió, para qué animal, qué productos, en qué cantidad y hasta cuándo es válida. Además hay dos
caminos: la receta nace **dentro** de PetPassport (el veterinario la emite desde la consulta) o **fuera**
(papel de otra clínica, foto subida por el tutor y procesada por OCR/IA).

## Decisión

1. **`Prescription`** es raíz de agregado (stream `rx-<id>`), independiente de `Encounter`, con `Source`:
   - `issued`: creada por un `veterinarian` con credencial verificada (`ProfessionalCredential`, nuevo en
     Partner Network), opcionalmente a partir de los `MedicationItem` de un encuentro firmado; se **firma**
     (`PrescriptionIssued`) y genera PDF con hash y QR de verificación (infra de F2).
   - `external`: creada por el tutor subiendo una imagen; pasa por extracción (ADR-019) y **confirmación del
     tutor**; queda marcada `unverified` hasta que una farmacia (`pharmacist`) la valide al cotizar o un
     veterinario vinculado la valide explícitamente.
2. **Ítems** referencian el catálogo (`catalog_id`, ADR-016) o texto libre con marca; cada ítem lleva cantidad
   a dispensar, posología y `substitution_allowed`.
3. **Validez**: `valid_until` (por defecto 30 días desde emisión, configurable por país; máximo 180),
   `refills_allowed` (0..5), `dispensations[]` (registro de dispensaciones por farmacia, con fecha y cantidad).
   Una receta `expired` o totalmente dispensada no admite nuevas cotizaciones.
4. **Sustancias controladas** (`medication.controlled = true`): se emiten y verifican en PetPassport, pero
   **no se cotizan ni se piden** a través de la plataforma en F4 (el tutor acude con la receta física/PDF).
   Regla explícita por país en `ref.regulatory_rules`.
5. **Revocación**: el veterinario emisor puede revocar (`PrescriptionRevoked`, motivo) mientras no haya
   dispensación; después solo SuperAdmin.
6. **Inmutabilidad**: una receta emitida no se edita; errores → revocar y emitir nueva (misma regla que
   encuentros, ADR-010).

## Consecuencias

- El plan de tratamiento (F2) sigue siendo documentación clínica; la receta es el documento dispensable.
  Un plan puede generar 0..n recetas.
- Las farmacias verifican recetas `issued` por QR/hash sin cuenta (endpoint público de verificación) y
  recetas `external` solo con juicio profesional (la plataforma lo deja claro en la UI y la API).
- Se añade `ProfessionalCredential` (número de tarjeta profesional, país, verificación por SuperAdmin) como
  requisito para emitir recetas.

## Alternativas consideradas

- Receta como parte de `Encounter`: descartado; ciclo de vida distinto (validez, dispensaciones) y existe
  sin consulta en PetPassport (externa).
- Prohibir recetas externas: descartado; es el caso de uso principal del tutor que llega desde otra clínica.
