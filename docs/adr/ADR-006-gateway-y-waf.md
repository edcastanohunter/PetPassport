# ADR-006: WAF gestionado en el borde + Traefik como API Gateway; el JWT se valida en la aplicación

- Estado: Aceptada
- Fecha: 2026-10-20
- Fase: F1 (revisión en F4 para tráfico partner)

## Contexto

Arquitectura pública expuesta a Internet desde el MVP. Se necesita WAF, TLS, rate limiting y controles
de borde sin que el equipo opere un WAF propio.

## Decisión

Defensa en profundidad en tres capas:

| Capa | Componente | Responsabilidades F1 |
|---|---|---|
| Borde | WAF gestionado (recomendado: Cloudflare; alternativa: AWS WAF + CloudFront) | Reglas gestionadas OWASP, bot management, rate limiting por IP/ruta, DDoS, TLS 1.2+, ocultar IP de origen, geo-reglas. |
| Gateway | Traefik v3 | Enrutamiento `api.`/`app.`/`auth.`, TLS interno/ACME, cabeceras de seguridad, límite de tamaño de cuerpo, timeouts, rate limit de respaldo, allowlist de IP para `/admin` (opcional), acceso a Keycloak solo por rutas permitidas. |
| Aplicación | Go (`internal/platform/auth`, `authz`) | Validación de JWT (JWKS, iss, aud, exp, alg), autorización por política, validación de payload, idempotencia. |

El JWT **no** se valida en el gateway en F1: Traefik OSS no lo hace de forma nativa y la validación en la app
es obligatoria de todos modos (defensa en profundidad). En F4 se evalúa Kong/APISIX para el tráfico partner
(API keys, mTLS, cuotas, portal).

## Consecuencias

- El origen solo acepta tráfico del WAF (allowlist de IPs de Cloudflare / cabecera de autenticación de origen).
- Keycloak se publica detrás del mismo gateway en `auth.<dominio>`, con la consola de administración
  restringida por IP/VPN.
- Rate limiting distribuido en la app llega con Redis en F3; en F1 basta WAF + Traefik.

## Alternativas consideradas

- **Kong/APISIX en F1**: válido, pero más pesado de operar; diferido a F4 donde aporta valor (partners).
- **ModSecurity/Coraza autogestionado**: descartado en F1 por carga operativa; reconsiderable si hay requisito de soberanía.
