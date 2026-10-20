# F2 · 06 — Frontend (Vue 3)

Stack y convenciones de F1 (FE-001…004, 010, 011) vigentes. Nuevas *features*: `clinic-agenda`, `encounters`,
`card`, `records`, `shares`, `requests`, `notifications`.

## 1. Espacio clínico

**[PP-F2-FE-001] (MUST)** Agenda (`/org/:orgId/agenda`).
- Vistas día/semana por profesional (columnas) o combinada; franjas por color de estado (`requested` bandeja aparte, `proposed`, `confirmed`, `checked_in`, `completed`, `cancelled`, `no_show`); navegación por fecha; zona horaria de la organización.
- Clic en hueco libre → crear cita directa (modal: mascota vinculada con buscador, tipo, motivo, profesional, duración por tipo precargada).
- Clic en cita → panel lateral con acciones según estado (confirmar, proponer, reprogramar por arrastre o modal, cancelar, check-in, no-show, iniciar consulta).
- Bandeja de solicitudes: lista con preferencias del tutor y botón "Confirmar en…" que muestra disponibilidad coincidente (`/availability`).

**[PP-F2-FE-002] (MUST)** Gestión de citas: formulario de cita, modal de propuesta de alternativa (con caducidad 48 h visible), reprogramación con validación de solapamiento (error `SLOT_UNAVAILABLE` → resalta franja ocupada), cancelación con motivo, historial de transiciones.

**[PP-F2-FE-003] (MUST)** Editor de consulta (`/org/:orgId/encounters/:id`).
- Layout en dos columnas: izquierda, navegación SOAP (Motivo/Constantes · S · O · A · P · Adjuntos · Control) con indicador de completitud; derecha, resumen del paciente (alertas, peso anterior, últimas vacunas, próximas dosis).
- **Autoguardado** cada 5 s con `If-Match`; indicador "Guardado hh:mm:ss"; ante `409` ofrece ver diferencias y recargar.
- Constantes con unidades y *warnings* por especie (del backend) en línea; peso prellenado con el último registrado.
- Diagnósticos: lista editable con certeza y buscador de códigos (si catálogo); plan: tabla de medicación con dosis/unidad/vía/frecuencia/duración (campos estructurados, cálculo de dosis total si `mg_per_kg` y peso), procedimientos, recomendaciones, control recomendado (fecha + motivo).
- Plantilla por tipo: `vaccination` muestra panel lateral "Registrar vacuna" (FE-006) que enlaza la entrada a la consulta.
- Roles: `receptionist` ve S/O/constantes editables y A/P en solo lectura con aviso.

**[PP-F2-FE-004] (MUST)** Firma, adendas y PDF.
- Botón "Firmar" con checklist de requisitos (`ENCOUNTER_INCOMPLETE` mapea a ítems); modal de confirmación "Al firmar, la consulta no podrá modificarse; podrá añadir adendas"; tras firmar, vista de solo lectura con sello (firmante, fecha, hash abreviado + copiar).
- Adendas: formulario (texto, motivo, adjuntos ≤ 5), listado numerado bajo la consulta.
- PDF: botón que muestra "Generando…" (poll a `/documents/{job}`) y luego descarga; verificación pública enlazada.
- Borradores abandonados: lista en `/org/:orgId/encounters?filter=abandoned` con retomar/descartar.

**[PP-F2-FE-005] (MUST)** Ficha de paciente (`/org/:orgId/patients/:petId`) con pestañas: Resumen (alertas editables, última consulta, próximas dosis, peso, controles pendientes), Historial (timeline con filtros; ítems de otras clínicas marcados; indicador de frescura), Consultas (tabla propia), Carnet, Citas, Peso (gráfico). Si el acceso es solo por custodia (vínculo revocado), banner "Acceso limitado a sus propias consultas".

**[PP-F2-FE-006] (MUST)** Carnet (clínica): tabla de entradas (activas/anuladas, verificadas/declaradas con badges), formulario de registro (producto del catálogo con búsqueda por especie, fecha, lote con escaneo de código de barras opcional vía cámara (COULD), fabricante, caducidad, vía, dosis, peso, próxima dosis propuesta con explicación y editable), anular con motivo (deshabilitado fuera de ventana con tooltip), verificar/disputar declaradas, registro de preventivos.

**[PP-F2-FE-007] (MUST)** Configuración de agenda (`/org/:orgId/settings/agendas`): por profesional, editor semanal (rangos por día), duraciones por tipo, excepciones (calendario), políticas (antelación, cancelación), activar/desactivar.

**[PP-F2-FE-008] (MUST)** Solicitud de vínculo: en "Pacientes" botón "Solicitar acceso a una mascota" → búsqueda por microchip o email → resultados mínimos → mensaje opcional → enviar; lista de solicitudes con estado y caducidad.

## 2. Espacio tutor

**[PP-F2-FE-009] (MUST)** Historial (`/app/pets/:petId/history`): timeline con filtros, tarjetas por consulta firmada (clínica, profesional, diagnósticos, adendas), detalle completo con adjuntos visibles y PDF; indicador de frescura (FE-014).

**[PP-F2-FE-010] (MUST)** Carnet (`/app/pets/:petId/card`): próximas dosis destacadas (vencidas en rojo), tabla de entradas, "Declarar vacuna anterior" (formulario + foto del carnet), descargar PDF, "Compartir" → crear enlace (alcance, días, PIN opcional) con QR en pantalla y copiar; lista de enlaces activos con revocar.

**[PP-F2-FE-011] (MUST)** Citas (`/app/appointments`): próximas/pasadas; "Solicitar cita" (mascota → clínica vinculada → tipo → motivo → elegir hasta 3 franjas de la disponibilidad publicada o rango de fechas) → estado `requested`; aceptar propuesta (con cuenta atrás de caducidad); cancelar (aviso de cancelación tardía si aplica); detalle con dirección y teléfono de la clínica.

**[PP-F2-FE-012] (MUST)** Bandeja (`/app/inbox`) y preferencias: solicitudes de vínculo (aprobar/rechazar con vista de la clínica), propuestas de cita, controles recomendados (con "Solicitar cita" prellenado); `/app/settings/notifications` con interruptores por categoría (security bloqueado) y explicación.

## 3. Público

**[PP-F2-FE-013] (SHOULD)** Página de enlace compartido (`/share/:token`, sin autenticación): pantalla de PIN si aplica (5 intentos); vista de carnet o historial reducido con sello "Verificado por PetPassport · fecha"; sin datos de contacto; botón "Descargar PDF"; `noindex`. Página de verificación de consulta (`/verify/encounters/:id?hash=`).

## 4. Transversal

**[PP-F2-FE-014] (MUST)** Vistas derivadas (timeline, resumen, peso, estadísticas, bandeja) muestran estado de frescura a partir de `X-PP-Freshness`: oculto si `lag < 3 s`, "Actualizando… (hace Ns)" si mayor, y botón "Recargar". Tras un comando que afecta a una vista derivada (p. ej. firmar), la UI hace *polling* corto (3 × 2 s) para reflejarlo.

Otros: gráficos con la librería y paleta del *dataviz skill* del proyecto (peso, estadísticas); accesibilidad del editor (atajos de teclado para secciones SOAP, `aria-live` para autoguardado); cámara para foto del carnet/adjuntos en móvil (`<input capture>`).
