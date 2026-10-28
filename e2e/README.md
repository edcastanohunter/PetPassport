# E2E (PP-F1-QA-005)

Playwright contra el compose completo de desarrollo: Keycloak real, Mailpit para leer correos.

| Spec | Flujo crítico (docs/phase-1/10 §5) |
|---|---|
| `01-owner-registration` | 1. Registro de tutor → verificación por email → onboarding → crear mascota → ver en lista |
| `02-clinic-flows` | 2. Registrar clínica → SuperAdmin (con OTP) verifica → código público visible |
| | 3. Invitar veterinario → aceptar (segunda sesión) → aparece como miembro |
| | 4. Vincular por código → el veterinario ve paciente y teléfono → revocar → deja de verlo |
| `03-transfer` | 5. Transferencia de tutela entre dos usuarios |

Además, `expectAccessible` pasa axe (WCAG 2.1 A/AA) en home, onboarding, formulario y detalle de
mascota, lista, registro y resumen de organización, miembros, pacientes, ficha de paciente,
aceptación de transferencia y consola de organizaciones: cero violaciones *serious/critical*.

## Ejecutar en local

```bash
make dev-certs && make dev-up        # compose completo (https://app.pp.localhost)
make e2e                             # npm ci + playwright install chromium + playwright test
cd e2e && npx playwright show-report # informe HTML
```

Cada ejecución crea usuarios únicos (`e2e-*-<run>@e2e.petpassport.local`) por la Admin API de
Keycloak (`http://localhost:8180`, `admin/admin` en dev); el flujo 1 registra por la UI. No hace
falta `make dev-seed`. Variables para otro entorno con Mailpit: `E2E_APP_URL`, `E2E_KC_ADMIN_URL`,
`KC_ADMIN_USER`, `KC_ADMIN_PASSWORD`, `E2E_MAILPIT_URL`.

En CI corre cada noche y a demanda (`.github/workflows/e2e.yml`) y publica el informe como artefacto.
