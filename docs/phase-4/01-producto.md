# F4 · 01 — Especificación de producto

## 1. Personas (cambios)

| Persona | Novedades F4 |
|---|---|
| **Tutor** | Sube recetas externas, confirma extracción, pide cotizaciones, compara, hace pedidos, acepta sustituciones. |
| **Veterinario** | Registra su credencial profesional, emite y revoca recetas, ve sustituciones. |
| **Admin de farmacia** | Configura catálogo/mapeo, zonas y horarios, credenciales API, equipo; ve métricas. |
| **Farmacéutico** (`pharmacist`) | Cotiza, valida recetas, gestiona pedidos, registra dispensaciones. |
| **Partner técnico** | Integra contra `partner/v1` con sandbox y docs. |
| **SuperAdmin** | Alta de partners, reglas regulatorias por país, presupuesto de IA, monitor de extracción, verificación de credenciales. |

## 2. Épicas

| Épica | Historias |
|---|---|
| E20 Recetas emitidas | PRD-001 … 004, 017, 020 |
| E21 Recetas externas y extracción | PRD-005, 006, 007 |
| E22 Cotización y pedido (tutor) | PRD-008 … 011, 021, 022 |
| E23 Portal de farmacia | PRD-012 … 016 |
| E24 Plataforma B2B y administración | PRD-018, 019 |

## 3. Historias y criterios de aceptación

### E20 — Recetas emitidas

**[PP-F4-PRD-001] (MUST)** Como veterinario quiero registrar mi credencial profesional para poder emitir recetas.
- Campos: país, número de tarjeta/registro profesional, entidad emisora, fecha, documento adjunto (PDF/imagen); estado `pending → verified | rejected` por SuperAdmin con motivo; caducidad opcional.
- Sin credencial `verified` no aparece la acción "Emitir receta"; la clínica ve qué veterinarios tienen credencial.

**[PP-F4-PRD-002] (MUST)** Como veterinario quiero emitir una receta firmada desde una consulta (o directa para una mascota vinculada).
- Desde el plan firmado: selecciono ítems de medicación (prellenados), ajusto cantidad a dispensar, posología, duración, `substitution_allowed`, notas para la farmacia; validez (por defecto 30 días) y refills (0–5).
- Directa: mismos campos, requiere mascota vinculada y motivo breve.
- Al firmar: PDF con datos del animal, tutor (nombre), veterinario (nombre, credencial), clínica, ítems, validez, QR de verificación y hash; la receta queda inmutable; el tutor la recibe (notificación).
- Ítems con `catalog_id` obligatorio salvo marca "texto libre" (aviso: menos farmacias podrán cotizar).

**[PP-F4-PRD-003] (MUST)** Como farmacia (con o sin cuenta) quiero verificar una receta por QR/código para saber que es auténtica y vigente.
- Página pública `/verify/prescriptions/{id}?h=`: válida/revocada/expirada, veterinario y clínica, animal (nombre y especie), ítems, dispensaciones previas (cantidad, fecha, sin nombre de farmacia), sin datos de contacto del tutor.
- Rate limit; registro de verificaciones (el veterinario ve el contador).

**[PP-F4-PRD-004] (MUST)** Como veterinario quiero revocar una receta emitida por error mientras no se haya dispensado.
- Motivo obligatorio; el tutor es notificado; las cotizaciones abiertas se cierran; tras dispensación solo SuperAdmin.

**[PP-F4-PRD-017] (SHOULD)** Como veterinario quiero ver el estado de mis recetas (vigente, dispensada parcial/total, expirada, revocada) y recibir aviso cuando una farmacia propone una sustitución.

**[PP-F4-PRD-020] (MUST)** Como usuario quiero que las sustancias controladas se gestionen con claridad.
- Las recetas pueden incluirlas; al solicitar cotización, esos ítems se excluyen automáticamente con el mensaje "Este medicamento debe dispensarse presentando la receta en la farmacia"; el PDF/QR sirve para ese fin.
- La regla depende del país (`ref.regulatory_rules`) y la mantiene SuperAdmin.

### E21 — Recetas externas y extracción

**[PP-F4-PRD-005] (MUST)** Como tutor quiero fotografiar una receta de otra clínica para que PetPassport la entienda.
- Captura con guía (encuadre, luz, varias páginas); formatos JPEG/PNG/HEIC/PDF ≤ 10 MB por página, ≤ 5 páginas.
- Tras subir veo "Analizando…" (≤ 30 s habitual); si la imagen es ilegible, se me pide repetir con consejos.
- Resultado: lista de ítems detectados con producto propuesto (del catálogo), dosis, frecuencia, duración, cantidad y nivel de confianza; veterinario/clínica y fecha detectados si legibles.

**[PP-F4-PRD-006] (MUST)** Como tutor quiero revisar y corregir la extracción antes de usarla.
- Vista lado a lado (imagen con zoom y resaltado de la zona de cada ítem cuando esté disponible; extracción editable); por ítem: elegir entre candidatos del catálogo, buscar otro, marcar "texto libre", editar dosis/frecuencia/duración/cantidad, eliminar; añadir ítem manual.
- Debo marcar "Confirmo que coincide con mi receta" para continuar; la receta queda `external · unverified` y las farmacias lo ven así.
- Puedo volver a editar mientras no haya cotización abierta.

**[PP-F4-PRD-007] (MUST)** Como tutor quiero ver mis recetas (emitidas y externas) por mascota con estado, validez, dispensaciones y acciones (cotizar, descargar, archivar).

### E22 — Cotización y pedido

**[PP-F4-PRD-008] (MUST)** Como tutor quiero pedir cotización de una receta a farmacias aliadas.
- Elijo ítems (controlados excluidos), modalidad (recogida / envío a mi dirección), farmacias: sugeridas (vinculadas, cercanas por directorio, "todas en mi ciudad"), hasta 10; veo tiempo de respuesta habitual y modalidad soportada por cada una.
- Plazo de respuesta visible (24 h por defecto); puedo cancelar la solicitud mientras no haya pedido.
- Límite: 3 solicitudes abiertas por receta; 10 por día.

**[PP-F4-PRD-009] (MUST)** Como tutor quiero seguir las cotizaciones en tiempo real y compararlas.
- Tablero que se actualiza (polling/push) con estado por farmacia; cada cotización muestra precio por ítem, disponibilidad (completo/parcial), sustituciones propuestas, coste y tiempo de envío o dirección de recogida, validez de la oferta.
- Comparación ordenable por total, completitud, tiempo; "Elegir" bloquea la selección.

**[PP-F4-PRD-010] (MUST)** Como tutor quiero hacer el pedido y seguirlo.
- Al elegir: confirmo modalidad, dirección (si envío) y teléfono; aviso explícito "El pago se realiza en la farmacia"; la farmacia recibe el pedido con la receta (PDF/QR o imagen).
- Estados: pedido → confirmado → preparando → listo para recoger / en camino → entregado; o rechazado/cancelado con motivo. Puedo cancelar hasta "preparando".
- Al "entregado", la farmacia registra la dispensación y mi receta refleja cantidades dispensadas y refills restantes.

**[PP-F4-PRD-011] (MUST)** Como tutor quiero aceptar o rechazar sustituciones propuestas por la farmacia.
- Solo si el ítem permitía sustitución; veo producto original vs propuesto (mismo principio activo, concentración y forma validados por el sistema) y diferencia de precio; mi decisión se registra; para recetas emitidas, el veterinario recibe aviso.

**[PP-F4-PRD-021] (SHOULD)** Como tutor quiero encontrar farmacias cercanas al solicitar cotización (reutiliza el directorio de F3 con filtro `pharmacy`, distancia y modalidad).

**[PP-F4-PRD-022] (COULD)** Como tutor quiero valorar la farmacia tras un pedido entregado (1–5 + comentario moderado); la media se muestra junto al SLA.

### E23 — Portal de farmacia

**[PP-F4-PRD-012] (MUST)** Como farmacéutico quiero recibir solicitudes de cotización y responderlas desde el portal.
- Bandeja con solicitudes nuevas (badge, email/push), plazo restante, mascota (nombre, especie, peso), ítems con producto del catálogo y candidatos de mi mapeo; receta visible (PDF/QR emitida o imagen externa marcada "no verificada").
- Formulario: por ítem precio, disponibilidad (sí/parcial/no), sustitución (buscar equivalente validado), coste de envío por zona, tiempo estimado, validez de la oferta; "Declinar" con motivo; borrador y envío.
- Puedo marcar una receta externa como "validada por farmacéutico" o "rechazada" (motivo) — se registra en la receta.

**[PP-F4-PRD-013] (MUST)** Como farmacéutico quiero gestionar pedidos y registrar dispensaciones.
- Lista por estado; detalle con datos de contacto del tutor (solo tras pedido), dirección si envío, receta; acciones de estado; "Entregado" exige registrar dispensación (ítems y cantidades; parcial permitido) que se propaga a la receta.
- Rechazar con motivo (p. ej. receta inválida) devuelve al tutor la posibilidad de elegir otra cotización.

**[PP-F4-PRD-014] (MUST)** Como admin de farmacia quiero configurar mi catálogo, zonas y modalidades.
- Mapeo producto PetPassport ↔ mi SKU/precio base (manual o CSV), activar/desactivar productos; modalidades (recogida/envío), zonas de envío (ciudades/polígonos simples) con coste y tiempo; horarios de atención; auto-declinar fuera de zona.

**[PP-F4-PRD-015] (MUST)** Como admin de farmacia quiero credenciales de API, webhooks y sandbox para integrar mis sistemas.
- Crear/rotar `client_secret`, configurar URL y secreto de webhook, elegir eventos, probar entrega ("Enviar evento de prueba"), ver registro de entregas con reintento; activar entorno sandbox con datos ficticios; documentación enlazada.

**[PP-F4-PRD-016] (SHOULD)** Como admin de farmacia quiero ver métricas: solicitudes recibidas/respondidas, tiempo medio de respuesta, tasa de adjudicación, pedidos por estado, ingresos estimados (suma de pedidos entregados), valoración.

### E24 — Plataforma B2B y administración

**[PP-F4-PRD-018] (MUST)** Como SuperAdmin quiero administrar partners y reglas.
- Activar integración por farmacia (nivel 1/2/3), ver estado de credenciales y webhooks, suspender; reglas regulatorias por país (validez por defecto/máxima, controladas, refills máx.); verificación de credenciales profesionales; presupuesto mensual de IA con consumo actual y corte; monitor de extracción (volumen, precisión estimada por diffs, latencia, fallos, refusals).

**[PP-F4-PRD-019] (MUST)** Como usuario quiero notificaciones del ciclo.
- Tutor: receta emitida, extracción lista, cotización recibida (agrupadas), plazo por vencer (T-2h) con cotizaciones disponibles, pedido confirmado/listo/en camino/entregado/rechazado, sustitución propuesta.
- Farmacia: nueva solicitud, recordatorio a mitad de plazo si no respondió, pedido nuevo, cancelación.
- Veterinario: sustitución aceptada, receta dispensada (SHOULD), credencial verificada/rechazada.

## 4. Reglas de negocio F4

| Regla | Detalle |
|---|---|
| RN-22 | Solo veterinarios con credencial `verified` emiten recetas; la credencial se verifica por humano. |
| RN-23 | Una receta emitida es inmutable; corrección = revocar + emitir. |
| RN-24 | Las recetas externas son `unverified` hasta validación profesional y así se muestran siempre. |
| RN-25 | Ningún pedido se crea sin confirmación humana de los ítems (tutor) y aceptación de una cotización. |
| RN-26 | Sustancias controladas: emitibles, verificables, **no** cotizables ni pedibles en plataforma. |
| RN-27 | Sustitución solo con `substitution_allowed`, mismo principio activo/concentración/forma, y aceptación del tutor. |
| RN-28 | Datos del tutor se comparten con la farmacia solo tras el pedido; el historial clínico nunca. |
| RN-29 | Una receta con dispensación total o expirada no admite nuevas cotizaciones; refills descuentan por dispensación total. |
| RN-30 | El pago ocurre en la farmacia; PetPassport no cobra al tutor en F4. |
| RN-31 | Toda llamada a un partner (entrante o saliente) queda registrada con payload minimizado y resultado. |

## 5. Métricas F4

- Recetas emitidas por veterinario activo/semana; % de ítems con `catalog_id`.
- Extracción: F1 por campo (eval), % de ítems corregidos por el tutor (producción), tiempo mediano hasta confirmación, coste por receta.
- Cotización: solicitudes/semana, farmacias invitadas por solicitud, % respondidas antes del plazo, tiempo mediano de primera cotización, % con ≥ 2 cotizaciones.
- Pedido: conversión cotización→pedido (objetivo ≥ 40 % en piloto), % entregados, cancelaciones por causa, dispensaciones registradas.
- Partners: SLA por farmacia, errores de webhook, uso de API.

## 6. Fuera de alcance explícito

Pagos en línea, facturación de comisiones, inventario en tiempo real genérico, receta electrónica oficial con firma cualificada, logística, recomendación clínica de medicamentos.
