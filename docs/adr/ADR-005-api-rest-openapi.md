# ADR-005: API REST/JSON con OpenAPI 3.1 como contrato fuente

- Estado: Aceptada
- Fecha: 2026-10-20
- Fase: F1

## Contexto

Consumidores: SPA Vue (F1), app móvil (F6) y partners B2B (F4). El equipo necesita un contrato explícito
que genere cliente y servidor y que permita detectar cambios incompatibles en CI.

## Decisión

1. API pública REST/JSON bajo `/api/v1`. El contrato vive en `backend/api/openapi.yaml` (OpenAPI 3.1) y es la
   **fuente de verdad**: el servidor Go se genera con `oapi-codegen` (tipos + interfaz de handlers con
   validación de request); el cliente TypeScript con `openapi-typescript` + `openapi-fetch`.
2. Comandos y consultas se exponen como recursos y **acciones explícitas** (`POST /pets/{id}/transfer`),
   no como PATCH genérico para cambios con semántica de dominio.
3. Errores en formato `application/problem+json` (RFC 9457) con `code` estable del catálogo.
4. Control de concurrencia opcional por cliente: `If-Match: "<version>"` en comandos sobre agregados; el
   servidor devuelve `version` en cada recurso y `ETag`.
5. Idempotencia de comandos de creación mediante cabecera `Idempotency-Key` (UUID), ventana 24 h.
6. Versionado por ruta (`/v1`). Los cambios compatibles no versionan; los incompatibles abren `/v2`
   (y `oasdiff` en CI bloquea *breaking changes* en `/v1`).

## Consecuencias

- Los DTOs HTTP son distintos de los agregados: siempre hay mapeo explícito en `http/`.
- La validación sintáctica la hace el middleware generado; la semántica, el dominio.

## Alternativas consideradas

- **GraphQL**: descartado en F1; añade superficie de ataque (consultas profundas) y complejidad de autorización por campo.
- **gRPC para el front**: descartado; se reconsidera para comunicación interna en F6.
