# Carga ligera (criterio de salida 5 de F1, QA-006)

`k6/api-smoke.js`: 50 VUs, 30 s de rampa + 5 min sostenidos + 30 s de bajada contra la API de
staging. Cada iteración hace las lecturas más frecuentes de la SPA (`GET /me`, `GET /pets`,
`GET /pets/{id}`, `GET /legal/current`) y espera 1 s.

Umbrales (fallan la ejecución con código ≠ 0): p95 de `kind:api` < 300 ms, 0 respuestas 5xx,
0 respuestas 429, > 99 % de checks OK.

## Precondiciones (staging)

1. **Usuarios de carga**: 5–10 cuentas `pet_owner` sintéticas, verificadas, sin OTP, con el
   onboarding hecho y 1–3 mascotas cada una (se crean como `SMOKE_USER`, RUNBOOK §2).
2. **Límites por IP**: el WAF (SEC-001) y Traefik limitan la API por IP (~300 req/min). 50 VUs
   desde una sola máquina los superan, y un 429 invalida la medición. Durante la ventana de la
   prueba: en Cloudflare, regla `skip` del *rate limiting* con `ip.src eq <ip-generador>`; en
   la VM, subir temporalmente `rl-api-read.average` (p. ej. a 6000) en
   `infra/traefik/staging/routes.yml` (el *file provider* lo recarga en caliente). Al terminar,
   revertir ambos y comprobar con `git diff` en la VM. Anotar la ventana en el canal del equipo.
   El login (`rl-login`, 10/min) solo se usa en `setup()`: no más de 10 usuarios en `K6_USERS`.
3. Avisar en el canal: la alerta `ApiHighLatencyP95` puede dispararse si la prueba falla.

## Ejecutar

```bash
K6_DOMAIN=staging.<dominio> \
K6_USERS='load1@staging.<dominio>:<pw>,load2@staging.<dominio>:<pw>' \
make load-test
```

Genera `tests/load/k6/report.md` (tabla para pegar en el PR) y `summary.json` (no se commitean).
Contrastar con el panel de latencia de Grafana (`api-overview`) para la misma ventana.
