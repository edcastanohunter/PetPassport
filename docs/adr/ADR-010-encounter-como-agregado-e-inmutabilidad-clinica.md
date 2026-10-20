# ADR-010: `Encounter` y `VaccinationCard` como agregados; el historial clínico es un read model; inmutabilidad por firma y adendas

- Estado: Aceptada
- Fecha: 2026-10-20
- Fase: F2

## Contexto

"Historial clínico inmutable" es la promesa central. Hay que decidir la unidad de consistencia:

- Un único agregado `MedicalRecord` por mascota que contiene todas las consultas: invariantes globales fáciles,
  pero un stream que crece durante toda la vida del animal y que serializa a todos los profesionales que
  atienden a la misma mascota.
- Un agregado por consulta: streams pequeños, concurrencia natural (dos clínicas, dos consultas), pero las
  reglas que cruzan consultas (p. ej. próximas dosis de vacuna) necesitan otro agregado o un proceso.

## Decisión

1. **`Encounter`** (consulta/atención) es raíz de agregado, uno por atención, stream `enc-<id>`. Contiene
   motivo, constantes vitales, anamnesis, hallazgos, diagnósticos, plan de tratamiento (incluidos ítems de
   medicación estructurados que F4 convertirá en `Prescription`), procedimientos, adjuntos, recomendación de
   control y la **firma**.
2. **`VaccinationCard`** (carnet) es raíz de agregado, una por mascota, stream `vax-<petId>`. Contiene las
   dosis de vacunas y tratamientos preventivos (desparasitación, antiparasitarios externos) con su trazabilidad
   (lote, fabricante, profesional, organización) y el cálculo de próximas dosis según protocolo.
3. **El historial clínico (`MedicalRecord`) es un read model**, no un agregado: la proyección
   `read_clinical.medical_timeline` compone en orden cronológico encuentros, vacunas, citas y eventos de
   tutela de una mascota.
4. **Inmutabilidad**:
   - Un `Encounter` en `draft` puede editarse por el profesional que lo atiende (y por otros miembros clínicos
     de la misma organización); al **firmar** (`EncounterSigned`, solo `veterinarian`) queda congelado.
   - Tras la firma solo se admiten **adendas** (`EncounterAddendumAdded`: texto + motivo + firmante). Nunca se
     emiten eventos que "corrijan" campos firmados.
   - En el carnet, una dosis registrada se **anula** (`VaccinationEntryVoided` con motivo, por la misma
     organización dentro de 30 días o por SuperAdmin) y se registra una nueva; nunca se edita.
   - La organización que registró un encuentro conserva acceso de lectura a **ese** encuentro aunque el tutor
     revoque el vínculo (obligación de custodia del profesional). No ve nada posterior.
5. **Datos declarados por el tutor** (carnets de papel anteriores) se admiten como entradas `declared`
   (no verificadas), claramente marcadas; una clínica puede verificarlas (`VaccinationEntryVerified`).

## Consecuencias

- La concurrencia por mascota solo afecta al carnet; dos clínicas pueden abrir encuentros en paralelo.
- La UI de "ficha" se alimenta de `medical_timeline` (asíncrona) + `encounters` (síncrona): la consulta recién
  firmada aparece de inmediato en la lista de encuentros y, segundos después, en la línea de tiempo.
- Para auditoría legal, el PDF de un encuentro firmado se genera desde los eventos y lleva *hash* del stream
  hasta la versión firmada.

## Alternativas consideradas

- `MedicalRecord` como único agregado por mascota: descartado por contención y crecimiento del stream.
- Permitir edición post-firma con historial de versiones: descartado; contradice la promesa de inmutabilidad
  y la práctica clínico-legal (adendas).
