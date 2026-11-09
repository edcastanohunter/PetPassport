# Autenticación en PetPassport Partner API (ARQ-005, PTN-002, SEC-002)

La Partner API utiliza el estándar **OAuth 2.0 Client Credentials Grant** (RFC 6749) a través de Keycloak como servidor de autorización OIDC.

## Entornos

| Entorno | Auth Token Endpoint | Base API URL | Realm |
|---|---|---|---|
| **Sandbox** | `https://auth.sandbox.petpassport.app/realms/petpassport-sandbox/protocol/openid-connect/token` | `https://sandbox.petpassport.app/partner/v1` | `petpassport-sandbox` |
| **Producción** | `https://auth.petpassport.app/realms/petpassport/protocol/openid-connect/token` | `https://api.petpassport.app/partner/v1` | `petpassport` |

---

## 1. Obtención de Access Token

El partner realiza una petición `POST` al endpoint de token con sus credenciales `client_id` y `client_secret`:

```bash
curl -X POST "https://auth.sandbox.petpassport.app/realms/petpassport-sandbox/protocol/openid-connect/token" \
  -H "Content-Type: application/x-www-form-urlencoded" \
  -d "grant_type=client_credentials" \
  -d "client_id=partner-YOUR_ORG_ID" \
  -d "client_secret=ppsec_YOUR_SECRET_KEY" \
  -d "scope=partner:read partner:write"
```

### Respuesta Exitosa (200 OK)

```json
{
  "access_token": "<JWT_ACCESS_TOKEN>",
  "expires_in": 300,
  "token_type": "Bearer",
  "scope": "partner:read partner:write"
}
```

> [!IMPORTANT]
> **Separación de Audiencias (ARQ-005)**: El access token emitido tiene como audiencia obligatoria `aud = petpassport-partner-api`. 
> Los tokens de usuario final (`aud = petpassport-api`) son rechazados con `401 Unauthorized` en `/partner/v1`.
> De igual forma, el token B2B de partner es rechazado en `/api/v1`.

---

## 2. Uso del Token en Peticiones

Envíe el token en la cabecera `Authorization: Bearer <access_token>`:

```bash
curl -X GET "https://sandbox.petpassport.app/partner/v1/quote-requests?status=open" \
  -H "Authorization: Bearer <JWT_ACCESS_TOKEN>" \
  -H "Accept: application/json"
```

---

## 3. Rate Limits (PTN-006)

Las llamadas a la Partner API están sujetas a cuotas por partner:

* **Lecturas (`GET`)**: 600 peticiones por minuto.
* **Escrituras (`POST`, `PUT`, `DELETE`)**: 120 peticiones por minuto.

Todas las respuestas incluyen las cabeceras estándar IETF:
* `RateLimit-Limit`: Cuota máxima permitida por ventana.
* `RateLimit-Remaining`: Peticiones restantes en la ventana actual.
* `RateLimit-Reset`: Segundos restantes para el reinicio de la ventana.
* `Retry-After`: En caso de recibir `429 Too Many Requests`, tiempo en segundos a esperar antes de reintentar.
