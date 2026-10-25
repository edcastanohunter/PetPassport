# F1 · 01 — Especificación de producto

## 1. Personas

| Persona | Rol técnico | Necesidad principal en F1 |
|---|---|---|
| **Tutor** (dueño) | `pet_owner` (global) | Crear su cuenta, registrar sus mascotas, compartirlas con su clínica. |
| **Administrador de clínica** | `clinic_admin` (org) | Registrar la clínica, invitar a su equipo, ver pacientes vinculados. |
| **Veterinario** | `veterinarian` (org) | Ver sus pacientes y sus fichas (lectura en F1; escritura clínica en F2). |
| **Recepcionista** | `receptionist` (org) | Ver pacientes y datos de contacto del tutor. |
| **Administrador de farmacia** | `pharmacy_admin` (org) | Registrar la farmacia y su equipo (operación real en F4). |
| **SuperAdmin** (PetPassport) | `super_admin` (global) | Verificar organizaciones, actuar ante abuso. |

## 2. Épicas de la fase

| Épica | Historias |
|---|---|
| E1 Identidad y onboarding | PRD-001, 002, 003, 020 |
| E2 Perfil de tutor | PRD-004 |
| E3 Mascotas | PRD-005 … 010 |
| E4 Organizaciones y miembros | PRD-011 … 014 |
| E5 Vínculo mascota–clínica | PRD-015, 016 |
| E6 Administración | PRD-017, 018 |
| E7 Transversal | PRD-019 |

## 3. Historias de usuario y criterios de aceptación

Formato: **[ID] (Prioridad)** Como *persona* quiero *acción* para *beneficio*. Criterios en Given/When/Then.

### E1 — Identidad y onboarding

**[PP-F1-PRD-001] (MUST)** Como visitante quiero registrarme con email y contraseña para tener una cuenta.
- Dado un email no registrado, cuando completo el formulario de Keycloak, entonces recibo un correo de verificación y no puedo entrar a la app hasta verificarlo.
- Dado un email ya registrado, cuando intento registrarme, entonces veo un mensaje genérico (sin revelar existencia de la cuenta) y se me ofrece recuperar contraseña.
- La contraseña cumple la política (IAM-004); el formulario muestra los requisitos.

**[PP-F1-PRD-002] (MUST)** Como usuario quiero iniciar y cerrar sesión y recuperar mi contraseña.
- Login por email+contraseña con PKCE desde la SPA; tras login vuelvo a la ruta que intentaba abrir.
- Logout cierra la sesión en Keycloak y limpia el estado de la SPA.
- "Olvidé mi contraseña" envía enlace con caducidad; tras cambiarla, las demás sesiones se invalidan.
- Tras 5 intentos fallidos se aplica bloqueo temporal progresivo (IAM-005).

**[PP-F1-PRD-003] (MUST)** Como usuario nuevo quiero completar un onboarding para elegir cómo uso PetPassport.
- En el primer acceso se me piden: aceptar términos y privacidad (versión registrada), nombre para mostrar, país y teléfono (opcional).
- Elijo "Soy tutor" (voy a mi panel) o "Registro una organización" (voy a PRD-011). Puedo hacer ambas cosas después.
- Si cambia la versión de los términos, se me vuelve a pedir aceptación al entrar.

**[PP-F1-PRD-020] (MUST)** Como responsable del producto quiero páginas legales públicas y consentimiento versionado.
- `/legal/terms` y `/legal/privacy` públicas, con número de versión y fecha.
- El sistema guarda `terms_version`, `privacy_version` y fecha de aceptación por usuario (API-003).

### E2 — Perfil de tutor

**[PP-F1-PRD-004] (MUST)** Como tutor quiero ver y editar mi perfil (nombre, teléfono, país, idioma, dirección opcional, contacto de emergencia opcional).
- El email no se edita en la app (se gestiona en Keycloak "Mi cuenta").
- Los cambios quedan en `platform.audit_log`.

### E3 — Mascotas

**[PP-F1-PRD-005] (MUST)** Como tutor quiero registrar una mascota con nombre, especie, raza, sexo, esterilización, fecha de nacimiento (exacta o aproximada), color/señas y foto opcional.
- Nombre obligatorio (1–60). Especie de lista; "otra" exige texto.
- Fecha de nacimiento ≤ hoy y ≤ 40 años. Si "aproximada", se guarda el indicador.
- Al crear, la mascota aparece inmediatamente en mi lista (proyección síncrona).

**[PP-F1-PRD-006] (MUST)** Como tutor quiero ver y editar el perfil de mi mascota.
- Veo todos los campos, el estado (activa/fallecida), el historial de vínculos y la versión.
- No puedo editar una mascota fallecida (salvo revocar vínculos).
- Si otra pestaña modificó la mascota, al guardar recibo un conflicto (409) y la opción de recargar.

**[PP-F1-PRD-007] (MUST)** Como tutor quiero registrar el microchip de mi mascota para identificarla de forma única.
- Formato ISO 11784/11785 (15 dígitos) o legado (9/10 dígitos alfanuméricos); se normaliza (sin espacios/guiones).
- Si el microchip ya está asignado a otra mascota, recibo error `MICROCHIP_ALREADY_ASSIGNED` sin revelar a qué mascota.
- Se puede corregir una vez por el tutor; correcciones posteriores requieren SuperAdmin (F2: clínica).

**[PP-F1-PRD-008] (SHOULD)** Como tutor quiero subir una foto de mi mascota.
- JPEG/PNG/WebP de entrada, HEIC rechazado con mensaje (decisión M1.2: sin libheif); ≤ 5 MB; el sistema re-encodea a JPEG 1024 px máx y genera miniatura 256 px (ver ARQ-012).
- La foto anterior se conserva en el storage 30 días (recuperable por soporte) y se elimina después.

**[PP-F1-PRD-009] (SHOULD)** Como tutor quiero marcar a mi mascota como fallecida (fecha opcional) para que deje de aparecer como activa sin perder su historia.
- La mascota pasa a una sección "En memoria"; sigue visible para clínicas vinculadas en lectura.
- La acción pide confirmación; se puede deshacer en 24 h (SuperAdmin después).

**[PP-F1-PRD-010] (SHOULD)** Como tutor quiero transferir la tutela de una mascota a otro usuario registrado.
- Indico el email del nuevo tutor; si existe y está activo, recibe una invitación por correo y debe aceptar en 7 días.
- Al aceptar, la mascota desaparece de mi lista, aparece en la suya, los vínculos con clínicas se mantienen y la historia es la misma `PetID`.
- Si no existe el usuario, se le invita a registrarse y la transferencia queda pendiente.

### E4 — Organizaciones y miembros

**[PP-F1-PRD-011] (MUST)** Como profesional quiero registrar mi clínica o farmacia para operar en PetPassport.
- Campos: tipo, razón social, nombre comercial, identificación fiscal (país + número), dirección, email y teléfono de contacto, sitio web opcional.
- Al crear, quedo como `clinic_admin`/`pharmacy_admin`, la organización está `pending_verification` y recibe un `PublicCode`.
- Mientras está pendiente puedo editar el perfil e invitar miembros, pero no puede recibir vínculos de mascotas.

**[PP-F1-PRD-012] (MUST)** Como admin de organización quiero editar el perfil de la organización.
- Cambios en identificación fiscal o razón social de una organización ya verificada la devuelven a `pending_verification` y notifican al SuperAdmin.

**[PP-F1-PRD-013] (MUST)** Como admin de organización quiero invitar miembros por email con un rol y que ellos acepten.
- Invitación con token de un solo uso, caduca a los 7 días; se puede revocar y reenviar.
- Al aceptar (con sesión del email invitado, o tras registrarse con ese email) el usuario pasa a ser miembro con el rol indicado.
- Roles válidos por tipo: clínica → `clinic_admin`, `veterinarian`, `receptionist`; farmacia → `pharmacy_admin`, `pharmacist`.

**[PP-F1-PRD-014] (MUST)** Como admin de organización quiero cambiar el rol de un miembro o retirarlo.
- No se puede retirar ni degradar al último administrador activo.
- Un miembro retirado pierde acceso en el siguiente request (sin esperar caducidad del token).

### E5 — Vínculo mascota–clínica

**[PP-F1-PRD-015] (MUST)** Como tutor quiero vincular mi mascota a una clínica introduciendo su código para que vean su ficha.
- Introduzco el `PublicCode`; veo nombre y ciudad de la clínica y confirmo.
- Solo clínicas `active`. Máximo 20 vínculos activos por mascota. Vincular dos veces es idempotente.
- Puedo revocar el vínculo cuando quiera; la clínica deja de ver la mascota.

**[PP-F1-PRD-016] (MUST)** Como miembro de clínica quiero ver la lista de pacientes vinculados y la ficha de cada uno (solo lectura).
- Lista con nombre, especie, raza, edad, foto, tutor (nombre y teléfono) y fecha de vínculo; búsqueda simple por nombre de mascota o tutor; paginación.
- La ficha muestra el perfil completo de la mascota y los datos de contacto del tutor. En F2 se añade la historia clínica.
- Si la organización está `suspended`, sus miembros ven un aviso y no acceden a pacientes.

### E6 — Administración (SuperAdmin)

**[PP-F1-PRD-017] (MUST)** Como SuperAdmin quiero ver la cola de organizaciones pendientes y verificar, rechazar (con motivo), suspender o reactivar.
- La verificación activa la organización y notifica por email al admin. El rechazo guarda el motivo y lo muestra al admin.
- Suspender inhabilita el acceso de los miembros a pacientes y oculta la organización a la búsqueda por código.

**[PP-F1-PRD-018] (MUST)** Como SuperAdmin quiero buscar usuarios por email/nombre y suspender/reactivar una cuenta.
- Suspender deshabilita el usuario en Keycloak (service account) y marca `suspended` en `UserAccount`; las sesiones activas se revocan.
- Toda acción de SuperAdmin queda en `platform.audit_log` con motivo obligatorio.

### E7 — Transversal

**[PP-F1-PRD-019] (SHOULD)** Como usuario quiero la aplicación en español e inglés, usable en móvil y accesible.
- Idioma por preferencia de usuario, por defecto el del navegador; correos de Keycloak en el mismo idioma.
- WCAG 2.1 AA en formularios y navegación; *responsive* desde 360 px.

## 4. Reglas de negocio transversales F1

| Regla | Detalle |
|---|---|
| RN-01 | Un usuario puede ser tutor y miembro de N organizaciones simultáneamente. |
| RN-02 | Una organización tiene exactamente un tipo (clínica o farmacia) y no cambia. |
| RN-03 | Solo las organizaciones `active` de tipo clínica reciben vínculos. |
| RN-04 | Un microchip pertenece a lo sumo a una mascota (activa o fallecida). |
| RN-05 | Los datos de contacto del tutor son visibles para clínicas con vínculo activo; nunca para farmacias en F1. |
| RN-06 | Toda acción de SuperAdmin exige motivo y queda auditada. |

## 5. Métricas del MVP (piloto)

- Activación: % de registros que crean ≥ 1 mascota en 7 días (objetivo ≥ 60%).
- Vinculación: % de mascotas con ≥ 1 vínculo activo a los 30 días (objetivo ≥ 30% en clínicas piloto).
- Tiempo de verificación de organización (objetivo < 24 h hábiles).
- Errores 5xx por 1.000 requests (< 1); p95 API < 300 ms.

## 6. Fuera de alcance explícito

Login social, co-tutores, chat, pagos, notificaciones push, importación masiva de pacientes desde otros sistemas
(se evalúa en F2 como herramienta para clínicas piloto).
