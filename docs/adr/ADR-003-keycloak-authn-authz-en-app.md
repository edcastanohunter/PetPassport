# ADR-003: Keycloak solo autentica; autorización y membresías viven en la aplicación

- Estado: Aceptada
- Fecha: 2026-10-20
- Fase: F1

## Contexto

Los roles del producto son de dos tipos:

- **Globales**: `pet_owner` (todo usuario), `super_admin`.
- **Por organización**: `clinic_admin`, `veterinarian`, `receptionist`, `pharmacy_admin`, `pharmacist`.
  Un mismo usuario puede ser veterinario en dos clínicas y tutor de sus propias mascotas.

Modelar los roles por organización en Keycloak (grupos por organización, atributos, o la función
*Organizations* de Keycloak 26) acopla el modelo de negocio al IdP, infla el token y obliga a sincronizar
dos fuentes de verdad.

## Decisión

1. Keycloak es responsable de: registro, verificación de email, contraseñas, MFA, sesiones, emisión de tokens,
   *brute force protection* y roles **globales** (`pet_owner`, `super_admin`) en `realm_access.roles`.
2. La aplicación es responsable de: `UserAccount` (aprovisionado *just-in-time* en el primer request a partir
   del token), **membresías de organización y su rol** (parte del agregado `Organization`, proyectadas a
   `read_partnernetwork.org_members`), y de **toda decisión de autorización** mediante un paquete `authz`
   con políticas explícitas (*deny by default*).
3. El token no lleva membresías. El middleware construye un `Principal{UserID, Email, GlobalRoles, Memberships}`
   cargando membresías desde el read model (caché en memoria con TTL corto; Redis en F3).
4. El backend usa una *service account* de Keycloak (`petpassport-backend`) solo para operaciones
   administrativas puntuales (deshabilitar usuario suspendido, reenviar verificación).

## Consecuencias

- Keycloak queda intercambiable por cualquier OIDC (Auth0, Zitadel) sin tocar el modelo.
- Un usuario eliminado de una organización pierde acceso inmediatamente (no depende de la expiración del token).
- Costo: una consulta (cacheada) de membresías por request autenticado.

## Alternativas consideradas

- **Keycloak Organizations (KC 26)**: prometedor, pero inmaduro para roles por organización y *multi-membership*;
  revisar en F5.
- **Claims de membresía en el token**: descartado por tamaño del token y por latencia de revocación.
- **Keycloak Authorization Services (UMA)**: descartado por complejidad operativa y acoplamiento.
