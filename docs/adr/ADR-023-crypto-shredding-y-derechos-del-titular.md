# ADR-023: Crypto-shredding por sujeto para PII en eventos; una única migración controlada de eventos históricos; automatización de derechos del titular

- Estado: Aceptada (ejecuta lo previsto en ADR-002)
- Fecha: 2026-10-20
- Fase: F5

## Contexto

Los eventos son inmutables y contienen PII aislada en `pii{}` desde F1. El derecho de supresión exige
poder hacer irrecuperable esa PII sin romper el *event log* ni la historia clínica del animal. Además,
exportación y eliminación son hoy procesos manuales/semiautomáticos.

## Decisión

1. **Claves por sujeto**: `privacy.subject_keys(subject_id, dek_encrypted, kek_id, status active|shredded)`.
   La DEK (AES-256-GCM) se genera al crear el sujeto (usuario) y se cifra con una KEK del KMS del proveedor
   cloud (`kek_id` rotable, *envelope encryption*). Las claves **nunca** se almacenan en la misma base de
   datos en claro ni se incluyen en los backups de la base de eventos (viven en el KMS + tabla cifrada).
2. **Cifrado en el Registry**: al serializar un evento, los campos marcados `pii` se cifran con la DEK del
   sujeto que declara `PIISubjects(event)` (F1); al deserializar, se descifran si la DEK está `active`; si está
   `shredded`, el campo se devuelve como `null` y el evento sigue siendo válido (los upcasters y proyectores
   toleran `pii` nulo: requisito de F1 cumplido por diseño).
3. **Eventos históricos**: una **única migración controlada** (`cmd/migrate privacy-encrypt-history`) reescribe
   `payload.pii` de los eventos anteriores a F5 con el valor cifrado. Es la única `UPDATE` permitida en
   `es.events` en la vida del sistema: se ejecuta con el rol `pp_migrator`, con el trigger append-only
   deshabilitado solo durante la ventana, con backup verificado previo, por lotes, con comprobación de que
   el payload descifrado coincide con el original (hash) y con registro en `audit_log`. Después, el trigger se
   restaura y se añade una prueba que falla si vuelve a existir cualquier migración que toque `es.events`.
4. **Supresión** = `ShredSubject(subject_id)`: marca la DEK `shredded` y la borra del KMS tras 7 días de
   gracia (reversible por si fue un error) → PII en eventos irrecuperable; además, job que anonimiza read
   models, OpenSearch, caché, backups lógicos recientes (los backups físicos expiran por retención; sin la
   DEK su contenido PII es inútil, lo que se documenta como mecanismo de cumplimiento).
5. **Derechos del titular automatizados** (`DataSubjectRequest` como proceso): exportación (JSON + PDFs en
   ZIP cifrado con enlace de un solo uso), supresión (verificación de identidad, plazo de gracia, excepciones
   legales: recetas con dispensación y datos de facturación se conservan el periodo legal, anonimizando lo
   demás), rectificación (ya cubierta por edición de perfil), con plazos y registro.
6. Lo que **no** se cifra: identificadores (`UserID`), nombre del animal, contenido clínico (dato del animal),
   nombres de organizaciones y profesionales en su rol público.

## Consecuencias

- Coste de CPU por evento con PII (pocos: registro, transferencias, pedidos); medido y aceptable.
- La supresión no destruye la historia clínica del animal: queda vinculada a un tutor anónimo.
- Rotación de KEK sin re-cifrar eventos (solo re-cifra las DEK).

## Alternativas consideradas

- **Borrar/editar eventos**: rompe inmutabilidad, proyecciones y auditoría.
- **Tombstone events** (`UserErased`) con proyecciones que ocultan PII: no elimina la PII del log; se usa
  además, como señal de dominio, pero no sustituye al cifrado.
