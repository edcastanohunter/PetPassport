# F3 · 01 — Especificación de producto

## 1. Épicas

| Épica | Historias |
|---|---|
| E15 Búsqueda clínica | PRD-001, 002, 003, 010 |
| E16 Catálogo de medicamentos | PRD-004, 005 |
| E17 Descubrimiento y cohortes | PRD-006, 007, 008 |
| E18 Rendimiento percibido y límites | PRD-009, 014 |
| E19 Pendientes de F2 | PRD-011, 012, 013 |

## 2. Historias y criterios de aceptación

### E15 — Búsqueda clínica

**[PP-F3-PRD-001] (MUST)** Como miembro clínico quiero encontrar un paciente escribiendo cualquier dato que recuerde (nombre de la mascota, nombre del tutor, microchip parcial, raza).
- Barra de búsqueda global en el espacio clínico; resultados en < 300 ms percibidos mientras escribo (autocompletado desde 2 caracteres).
- Tolerante a errores tipográficos y acentos ("Lúna" encuentra "Luna"); resalta coincidencias.
- Solo pacientes vinculados o en custodia; los de custodia se marcan "acceso limitado".
- Sin OpenSearch disponible, la barra funciona con búsqueda simple (nombre/microchip exactos) y muestra aviso (PRD-010).

**[PP-F3-PRD-002] (MUST)** Como veterinario quiero buscar en los historiales de mis pacientes por texto libre ("otitis", "prednisolona", "vómito crónico") con filtros.
- Busca en diagnósticos, anamnesis, hallazgos, plan, adendas y entradas de carnet de encuentros **firmados** accesibles.
- Filtros: especie, rango de fechas, tipo de atención, profesional, con/sin diagnóstico confirmado; facetas con conteos.
- Resultados agrupados por mascota con fragmentos resaltados; enlace al encuentro; paginación.
- Sinónimos veterinarios básicos (p. ej. "vómito/emesis", "picazón/prurito") mantenidos por SuperAdmin.

**[PP-F3-PRD-003] (MUST)** Como tutor quiero buscar en el historial y carnet de mis mascotas ("cuándo fue la última rabia", "antibiótico de mayo").
- Misma experiencia reducida a mis mascotas; en español e inglés según mi idioma.

**[PP-F3-PRD-010] (MUST)** Como usuario quiero saber si los resultados están desactualizados o degradados.
- Indicador de frescura (reutiliza FE-014 de F2) y banner "Búsqueda limitada temporalmente" cuando la API degrada a PostgreSQL.

### E16 — Catálogo de medicamentos

**[PP-F3-PRD-004] (MUST)** Como veterinario quiero elegir el medicamento del plan desde un catálogo con autocompletado, sin perder la posibilidad de escribir libremente.
- Autocompletado por nombre comercial, principio activo o sinónimo; muestra forma, concentración, presentación, laboratorio y si requiere receta; filtra por especie de la mascota (con opción de ver todos).
- Al elegir, el ítem guarda `catalog_id` y prellena vía y unidad de dosis habituales (si el catálogo las tiene).
- Si no existe, "Usar texto libre" y, opcionalmente, "Sugerir para el catálogo" (nombre + foto del empaque opcional).

**[PP-F3-PRD-005] (MUST)** Como SuperAdmin quiero mantener el catálogo.
- Listado con búsqueda y filtros; alta/edición/baja lógica con motivo; fusión de duplicados (redirección de `catalog_id`); revisión de sugerencias (aprobar → crea; rechazar → motivo al veterinario).
- Importador por lotes (CSV/JSON de fuente pública) con previsualización, validación y reporte; cada lote es auditable y reversible (baja lógica del lote).
- Métrica visible: % de ítems de planes con `catalog_id` por semana.

### E17 — Descubrimiento y cohortes

**[PP-F3-PRD-006] (SHOULD)** Como tutor quiero encontrar clínicas en PetPassport por nombre o ciudad para vincular a mi mascota sin pedir el código.
- Solo organizaciones `active` que activaron "Aparecer en el directorio" (opt-in, por defecto desactivado); datos mostrados: nombre, ciudad, dirección aproximada (barrio/zona), teléfono si la org lo autoriza, servicios (texto corto).
- Desde el resultado se puede solicitar vínculo "al revés" (tutor → clínica) que equivale a conceder el vínculo (flujo F1 con `PublicCode` resuelto automáticamente).

**[PP-F3-PRD-007] (SHOULD)** Como admin de clínica quiero listas de pacientes por criterio para campañas de salud preventiva.
- Cohortes predefinidas: vacunas vencidas o que vencen en N días (por vacuna), sin visita en > 12 meses, con diagnóstico confirmado X en los últimos N meses, por especie/edad/peso.
- Resultado exportable (CSV con nombre de mascota, tutor y teléfono — auditado) y acción "Enviar recordatorio" (email a tutores con preferencia `preventive_care`, plantilla de la clínica con texto libre moderado ≤ 500).

**[PP-F3-PRD-008] (SHOULD)** Como SuperAdmin quiero un panel de plataforma: usuarios activos, organizaciones por estado, mascotas, consultas firmadas, citas, vacunas, recordatorios enviados, por día/semana/mes; top clínicas por actividad; errores de proyección/indexación.

### E18 — Rendimiento percibido y límites

**[PP-F3-PRD-009] (MUST)** Como usuario quiero que la aplicación responda rápido en móvil con red 4G.
- Objetivos de producto (medidos con RUM en staging y en el piloto): pantalla inicial interactiva < 2,5 s (LCP), interacciones < 200 ms (INP), listas y fichas cargan en < 1 s percibido con *skeletons*; búsqueda con resultados en < 500 ms.

**[PP-F3-PRD-014] (MUST)** Como usuario quiero entender cuándo he superado un límite de uso en lugar de ver un error opaco.
- Mensajes `429` con tiempo de espera y motivo ("Demasiadas búsquedas; inténtalo en 30 s"); límites documentados en ayuda; los límites por organización son más altos que por usuario para no bloquear clínicas grandes.

### E19 — Pendientes de F2

**[PP-F3-PRD-011] (SHOULD)** Como tutor quiero recibir recordatorios como notificaciones push en el navegador/móvil (PWA) además del email.
- Opt-in explícito por dispositivo; mismas categorías y preferencias; contenido mínimo ("Luna tiene cita mañana a las 10:00"); al tocar, abre la pantalla correspondiente; gestión de dispositivos en preferencias.

**[PP-F3-PRD-012] (SHOULD)** Como clínica quiero registrar un paciente cuyo tutor aún no tiene cuenta para no perder la historia de la consulta de hoy.
- "Paciente provisional": mascota creada por la clínica con datos mínimos + email o teléfono del tutor; la clínica tiene vínculo automático; el tutor recibe invitación para reclamarla (crea cuenta → la mascota pasa a ser suya con toda su historia). Sin reclamar en 90 días, el paciente sigue accesible para la clínica pero marcado "sin tutor en PetPassport".
- Un paciente provisional reclamado se fusiona si el tutor ya tenía esa mascota (por microchip) con confirmación.

**[PP-F3-PRD-013] (COULD)** Importación CSV de pacientes/vacunas (PRD-022 de F2 si no entró), ahora creando pacientes provisionales.

## 3. Reglas de negocio F3

| Regla | Detalle |
|---|---|
| RN-16 | La búsqueda nunca devuelve documentos fuera del nivel de acceso del actor; ante duda (índice desactualizado) se oculta. |
| RN-17 | Los datos de contacto del tutor no se indexan; se resuelven al mostrar y solo si el actor tiene `pet:guardian_contact:read`. |
| RN-18 | El catálogo de medicamentos no modifica planes firmados; los cambios aplican a nuevos ítems. |
| RN-19 | El directorio público es opt-in y nunca expone datos de pacientes ni de miembros. |
| RN-20 | Las exportaciones de cohortes con datos de contacto quedan auditadas con motivo y se limitan a 1.000 filas por exportación. |
| RN-21 | Un paciente provisional es propiedad de nadie hasta reclamarse: solo la clínica creadora lo ve y edita. |

## 4. Métricas F3

- p95 de búsqueda de pacientes (autocompletado) < 150 ms servidor; búsqueda clínica < 300 ms.
- % de búsquedas sin resultados (< 15%) y % con clic en el primer resultado (> 60%).
- % ítems de medicación con `catalog_id` (≥ 70% en piloto).
- Uso de cohortes por clínica/mes; recordatorios de campaña enviados y tasa de citas resultantes.
- Error budget mensual consumido por SLO (07-rendimiento).
