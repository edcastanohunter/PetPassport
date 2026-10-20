# F4 · 06 — Frontend: tutor, veterinario y portal de farmacia

Stack y convenciones de F1–F3. Nuevas *features*: `prescriptions`, `quotes`, `orders`, `pharmacy` (portal), `partners-admin`, `developers` (sitio estático aparte).

## 1. Veterinario

**[PP-F4-FE-001] (MUST)**
- *Credencial* (`/app/profile/credentials`): formulario (país, número, entidad, fechas, documento), estado con motivo si rechazada; banner en el espacio clínico si falta.
- *Emitir receta* desde la consulta firmada (`/org/:orgId/encounters/:id` → "Emitir receta"): ítems del plan preseleccionados con casillas; por ítem cantidad a dispensar (sugerida desde dosis × frecuencia × duración, editable), sustitución permitida, instrucciones; validez y refills con límites del país visibles; aviso por ítem de texto libre y por controlados; vista previa; "Firmar y emitir" con confirmación. Receta directa desde la ficha del paciente.
- *Mis recetas* (`/org/:orgId/prescriptions`): tabla con estado, mascota, fecha, validez, dispensado; detalle con PDF, verificaciones (contador), dispensaciones, sustituciones aceptadas; revocar con motivo.

## 2. Tutor

**[PP-F4-FE-002] (MUST)** *Subir receta externa* (`/app/pets/:petId/prescriptions/new`): captura con cámara (overlay de encuadre, aviso de luz/enfoque por análisis simple en cliente, varias páginas), o archivo; progreso "Analizando…" con pasos; pantalla de **revisión lado a lado**: imagen con zoom/pan y, si hay `bbox`, resaltado del ítem activo; lista de ítems editables (producto: chips de candidatos con puntuación, buscador del catálogo, "texto libre"; dosis/unidad, frecuencia, duración, cantidad), confianza visual (verde/ámbar/rojo), añadir/eliminar ítem; cabecera (veterinario, clínica, fecha) editable; checkbox de atestación; "Confirmar". Estados `reupload_requested` con consejos y `AI_BUDGET_EXCEEDED` con captura manual.

**[PP-F4-FE-003] (MUST)** *Mis recetas* (`/app/prescriptions`): por mascota, tarjetas con origen (emitida/externa · verificada/no), estado, validez, dispensado/restante, acciones (cotizar, PDF/imagen, archivar); detalle con dispensaciones y pedidos.

**[PP-F4-FE-004] (MUST)** *Solicitar cotización* (wizard): 1) ítems (controlados y no mapeados marcados como excluidos con explicación), 2) modalidad y zona/dirección aproximada (o ubicación), 3) farmacias (lista con SLA, distancia, modalidades, valoración; vinculadas primero; selección múltiple hasta 10; "todas en mi ciudad"), 4) resumen y plazo; envío.

**[PP-F4-FE-005] (MUST)** *Tablero de cotizaciones* (`/app/quote-requests/:id`): estado por farmacia con tiempo restante; cotizaciones en tarjetas comparables (total, completitud, envío/recogida, ETA, validez) con ordenación; detalle por ítem; sustituciones con comparación original/propuesta y decisión; "Elegir" → modal de contacto/dirección + aviso de pago en farmacia; actualizaciones por SSE con reconexión y *fallback* a polling; cancelar solicitud.

**[PP-F4-FE-006] (MUST)** *Pedidos* (`/app/orders`, `/app/orders/:id`): línea de tiempo de estados, datos de la farmacia (dirección, teléfono, horario), instrucciones de recogida/entrega, receta adjunta, cancelar (si procede), valoración al entregar (COULD).

## 3. Portal de farmacia (`/pharmacy/:orgId`)

**[PP-F4-FE-007] (MUST)** *Bandeja de cotizaciones*: pestañas (pendientes con cuenta atrás, cotizadas, declinadas, expiradas); detalle con mascota (especie, peso, edad), ítems con producto PetPassport y mi mapeo (SKU/precio base prellenados), receta (PDF/QR con botón "Verificar" o imágenes con marca "no verificada" + acción "Validar/Rechazar" para farmacéuticos); **formulario de cotización** por ítem (precio, disponibilidad, sustitución con buscador restringido a equivalentes), envío por zona (prellenado), ETA, validez, notas; borrador automático; "Enviar cotización" / "Declinar".

**[PP-F4-FE-008] (MUST)** *Pedidos*: lista por estado con alertas (nuevos, por entregar hoy); detalle con contacto del tutor (visible tras pedido), dirección/mapa si envío, receta; botones de transición; "Entregado" abre registro de dispensación (cantidades por ítem, parcial permitido); rechazar/cancelar con motivo; impresión de etiqueta/comprobante (COULD).

**[PP-F4-FE-009] (MUST)** *Configuración*: capacidades y zonas (ciudades/zonas con coste y tiempo), horarios, auto-declinar; *catálogo/mapeo* (tabla del catálogo PetPassport con mi SKU/precio/activo, búsqueda, importación CSV con previsualización, exportación); equipo (reutiliza F1).

**[PP-F4-FE-010] (MUST)** *Integración*: nivel actual, credenciales (crear/rotar con secreto mostrado una vez y copiado), sandbox (activar, credenciales), webhook (URL, eventos, secreto, probar, registro de entregas con estado/código/reintentar), enlaces a `developers.<dominio>`, estado de SLA. Métricas (PRD-016) con gráficos.

## 4. Administración

**[PP-F4-FE-011] (MUST)** `/admin/partners` (alta/nivel/adaptador/suspender, estado de credenciales y webhooks, SLA), `/admin/regulatory` (reglas por país), `/admin/ai` (presupuesto, consumo diario por modelo, corte), `/admin/ocr` (monitor: volumen, tasas, latencia, refusals, tasas de edición por campo; muestras con imagen y extracción para revisión, acceso auditado), `/admin/credentials` (cola de verificación con documento).

## 5. Público y desarrolladores

**[PP-F4-FE-012] (MUST)** `/verify/prescriptions/:id` (público): estado grande (válida/revocada/expirada/agotada), emisor y credencial, clínica, animal, ítems (controlados marcados), dispensaciones; sin contacto del tutor; QR escaneable desde móvil abre esta página.

**[PP-F4-FE-013] (SHOULD)** `developers.<dominio>`: sitio estático (Astro/Starlight o similar) generado en CI: inicio, autenticación, flujo de cotización/pedido, webhooks (firma, vectores, ejemplos en 4 lenguajes), referencia OpenAPI interactiva (Scalar), sandbox, checklist go-live, changelog, estado.

## 6. Transversal

- SSE con `EventSource` y reconexión exponencial; indicador "en vivo".
- Moneda y formato según país de la organización (`Intl.NumberFormat`).
- Accesibilidad del revisor de extracción: navegación por teclado entre ítems, descripciones `aria` de confianza, contraste de resaltados.
- Textos legales: aviso "receta externa no verificada", "pago en farmacia", consentimiento para compartir contacto con la farmacia al hacer el pedido.
