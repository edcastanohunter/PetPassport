# F3 · 06 — Frontend (Vue 3)

## 1. Búsqueda

**[PP-F3-FE-001] (MUST)** Búsqueda global en el espacio clínico.
- Barra en la cabecera (`Ctrl/Cmd+K`): autocompletado de pacientes (debounce 150 ms, cancelación de peticiones en vuelo, 8 resultados con foto, nombre, especie, tutor, sufijo de microchip, badge "acceso limitado"); `Enter` abre la página de resultados.
- Página `/org/:orgId/search?q=`: pestañas Pacientes / Historial; filtros laterales (especie, tipo, profesional, fechas, solo confirmados, "solo mi clínica") con conteos de facetas; resultados agrupados por mascota con fragmentos resaltados (`<mark>` saneado: el backend devuelve texto plano con marcadores que el front convierte, nunca HTML); paginación por cursor; estado vacío con sugerencias; búsquedas recientes (localStorage, solo texto).
- Banner de degradación y frescura (FE-011).

**[PP-F3-FE-002] (MUST)** Búsqueda del tutor (`/app/search?q=`): mis mascotas; atajos "última vacuna de rabia", "medicamentos recetados" como consultas sugeridas; resultados enlazan a historial/carnet.

## 2. Catálogo de medicamentos

**[PP-F3-FE-003] (MUST)** *Medication picker* en el editor de consulta (reemplaza el campo de texto de `medication_name`): autocompletado desde `/suggest/medications` filtrado por especie (toggle "todas las especies"); al seleccionar muestra chip con nombre, forma y concentración, prellena vía/unidad; opción "Usar texto libre" (marca `free_text`), "Sugerir al catálogo" (modal con nombre, notas, foto); ítems antiguos (`catalog_id = null`) se muestran con icono "texto libre" y acción "Asociar al catálogo" (solo en borrador).

**[PP-F3-FE-004] (MUST)** Administración del catálogo (`/admin/catalogs/medications`): tabla con búsqueda/filtros, formulario completo, baja lógica con motivo, fusión (buscar destino, previsualizar), importador (subir archivo → previsualización con errores por fila → confirmar), bandeja de sugerencias (aprobar con edición previa / rechazar con motivo), panel de uso (`ratio` por semana, gráfico). Para veterinarios: `/org/:orgId/catalog/suggestions` con el estado de sus sugerencias.

## 3. Cohortes y campañas

**[PP-F3-FE-005] (SHOULD)** `/org/:orgId/cohorts`: tarjetas por cohorte con formulario de parámetros (fechas, vacuna, meses, diagnóstico con autocompletado); resultados en tabla (mascota, tutor, teléfono si permitido, último encuentro, motivo); exportar CSV (modal con motivo obligatorio y aviso de auditoría); "Enviar recordatorio" (editor de mensaje ≤ 500 con previsualización, cuenta de destinatarios y de excluidos por preferencia, confirmación).

## 4. Panel de plataforma

**[PP-F3-FE-006] (SHOULD)** `/admin/platform`: tarjetas KPI, series temporales (usuarios activos, consultas, citas, vacunas, notificaciones), tabla de organizaciones top, estado de proyecciones e índices (lag, fallos) con acciones (reindex, retry, purge cache — modal con motivo). Gráficos según `dataviz` del proyecto; selector de granularidad y rango.

## 5. Directorio

**[PP-F3-FE-007] (SHOULD)** Público `/directory` (búsqueda por nombre/ciudad, geolocalización opcional "cerca de mí" con permiso del navegador, tarjetas con servicios y distancia); ficha pública mínima; desde la cuenta del tutor, botón "Vincular a mi mascota" (selector de mascota). Ajustes de organización: sección "Directorio" con opt-in, servicios (chips), mostrar teléfono, barrio/zona, posición en mapa (clic para fijar `geo`), previsualización de la ficha pública.

## 6. PWA y push

**[PP-F3-FE-008] (SHOULD)** PWA instalable (`manifest.webmanifest`, service worker con `vite-plugin-pwa`, estrategia *network-first* para API y *cache-first* para assets); recepción de Web Push (`push` → `showNotification` con `data.url`; `notificationclick` enfoca/abre la ruta); opt-in en `/app/settings/notifications` por dispositivo ("Activar notificaciones en este dispositivo") con estado del permiso y lista de dispositivos; renovación de suscripción al cambiar (`pushsubscriptionchange`).

## 7. Pacientes provisionales

**[PP-F3-FE-009] (SHOULD)** Clínica: "Nuevo paciente sin cuenta" (formulario de mascota + contacto del tutor + enviar invitación), lista de provisionales con estado de reclamación y reenviar; ficha marcada "Provisional — sin tutor en PetPassport". Tutor: `/pet-claims/:token` con previsualización, detección de duplicado por microchip ("¿Es la misma mascota que Luna?") y aceptación con o sin fusión.

## 8. Rendimiento del cliente

**[PP-F3-FE-010] (MUST)**
- Presupuesto: JS inicial ≤ 180 KB gz (ruta `/app`), ≤ 250 KB (espacio clínico); `rollup-plugin-visualizer` en CI con fallo si se supera +10%.
- *Code splitting* por feature y por ruta; prefetch de rutas probables (`<RouterLink>` con `prefetch` en hover/viewport); carga diferida de gráficos, editor y mapa.
- Imágenes: `srcset` con miniaturas 256/512, `loading="lazy"`, `decoding="async"`, dimensiones explícitas (CLS).
- Datos: consultas con `stale-while-revalidate` (TanStack Query) y *skeletons*; listas virtualizadas > 100 filas; cancelación de peticiones al navegar.
- RUM: Web Vitals (LCP, INP, CLS, TTFB) enviados a `/client-events` con muestreo 20% en piloto; panel en Grafana (INF-003).
- Objetivos (PRD-009) verificados con Lighthouse CI en staging (móvil, 4G simulado): Performance ≥ 85, LCP < 2,5 s, INP < 200 ms.

## 9. Degradación y frescura en búsqueda

**[PP-F3-FE-011] (MUST)** Reutiliza FE-014 (F2): `X-PP-Search-Mode: degraded` → banner "Búsqueda limitada temporalmente: solo nombre y microchip" con reintento automático cada 30 s; `freshness.lag_seconds > 10` → "Los resultados pueden no incluir cambios de los últimos N s". Errores `429` → mensaje con cuenta atrás de `Retry-After` y deshabilitado del input.
