# ADR-020: `QuoteRequest` como saga con estado propio; `Order` como agregado; pago en la farmacia (sin pasarela en F4)

- Estado: Aceptada
- Fecha: 2026-10-20
- Fase: F4

## Contexto

Una solicitud de cotización abre N conversaciones paralelas con farmacias, con respuestas asíncronas,
tiempos de espera, fallos parciales y sustituciones. Es un proceso de larga duración con estado propio, no
una transacción. Además, procesar pagos obliga a cumplir PCI y a gestionar devoluciones; no es necesario para
demostrar valor en F4.

## Decisión

1. **`QuoteRequest`** es raíz de agregado (stream `qr-<id>`) que modela la saga: receta, ítems solicitados,
   farmacias invitadas, estado por farmacia (`pending → quoted | declined | failed | expired`), plazo
   (`expires_at`, por defecto 24 h), y resultado (`selected_quote_id`, `closed_reason`). Los `Quote` son
   entidades dentro de la saga (precio por ítem, disponibilidad, sustituciones propuestas, costes de envío,
   validez de la oferta).
2. **Orquestación** con process managers (F2/F3): `QuoteRequested` → encola `partner:quote-request` por
   farmacia (adaptador según nivel de integración, ADR-021); respuestas (portal, webhook o polling) →
   comandos `RecordQuote` / `DeclineQuote`; `expires_at` → `ExpireQuoteRequest` (scheduler de comandos);
   `QuoteSelected` → crea `Order`.
3. **`Order`** es raíz de agregado (stream `ord-<id>`): referencia a receta, cotización aceptada, farmacia,
   modalidad (`pickup | delivery`), dirección (solo si delivery, y solo en este momento se comparte contacto
   con la farmacia), estados `placed → confirmed → preparing → ready | out_for_delivery → delivered |
   cancelled | rejected`, y **registro de dispensación** que se refleja en `Prescription`.
4. **Pago en la farmacia** (F4): el pedido es una reserva/aceptación de oferta; el cobro ocurre al recoger
   o en la entrega, fuera de la plataforma. La API y la UI lo dicen explícitamente. Pasarela de pago
   (y comisión) se evalúa en F5/F6 con datos reales de conversión.
5. **Sustituciones**: una farmacia puede proponer un equivalente (mismo principio activo, concentración y
   forma; validado contra el catálogo) solo si `substitution_allowed`; el tutor acepta explícitamente; para
   recetas `issued`, el veterinario emisor recibe notificación de la sustitución (no bloquea).
6. **Datos compartidos con la farmacia** (minimización): en cotización — ítems, especie y peso del animal;
   en pedido — nombre del tutor, teléfono, dirección si delivery, PDF/QR de la receta `issued` o imagen de la
   receta `external`. Nunca historial clínico.

## Consecuencias

- Fallos parciales son normales: la saga cierra con las cotizaciones que lleguen; la UI muestra progreso.
- Un pedido cancelado por la farmacia reabre la posibilidad de elegir otra cotización vigente.
- La dispensación registrada por la farmacia es la única fuente para "receta consumida".

## Alternativas consideradas

- Pasarela de pago en F4: descartada por alcance y PCI.
- Saga sin agregado (solo tablas de proceso): descartada; el estado tiene reglas (plazos, selección única,
  cierre) que merecen invariantes y auditoría por eventos.
