# ADR-002: Event Store v0 desde la Fase 1; Event Sourcing selectivo por agregado

- Estado: Aceptada
- Fecha: 2026-10-20
- Fase: F1

## Contexto

El roadmap original situaba el Event Store en F2. Si F1 implementa mascotas y organizaciones como CRUD,
en F2 habría que migrar esos agregados a ES (reconstruir historia a partir de filas sin eventos), rehacer
repositorios y romper la API. Es el error clásico y caro.

Por otro lado, aplicar ES a todo (incluido el perfil de usuario) añade complejidad sin valor: el perfil
no tiene historia de negocio relevante y choca con el derecho de supresión de datos personales.

## Decisión

1. **F1 entrega el Event Store v0** en PostgreSQL: tablas `es.streams`, `es.events`, `es.outbox`; API Go
   `Append`/`Load` con concurrencia optimista por versión de stream; registro de tipos de evento con
   `schema_version`; metadatos (actor, correlation, causation).
2. **Proyecciones síncronas en F1**: los proyectores se ejecutan dentro de la misma transacción que el `Append`.
   El `outbox` se escribe desde F1 pero solo se consume en F2 (proyecciones asíncronas, notificaciones).
3. **ES selectivo**:
   - Event-sourced: `Pet` (Pet Registry), `Organization` (Partner Network) y, en F2+, todo lo clínico,
     citas, prescripciones y pedidos.
   - CRUD: `UserAccount`/`UserProfile` (Identity & Access) — espejo de Keycloak + datos de perfil.
     Sus cambios relevantes se registran en `platform.audit_log`.
4. **Datos personales dentro de eventos**: todo evento que contenga PII de una persona la encapsula en campos
   marcados como `pii` en el esquema del evento. En F1 se documenta el diseño de *crypto-shredding*
   (clave por sujeto; borrar la clave = anonimizar). La implementación se activa en F5, pero los esquemas de
   eventos de F1 ya separan la PII en un sub-objeto para no tener que re-versionar.

## Consecuencias

- F2 añade *consumidores*, *checkpoints*, *snapshots*, *upcasters* y *rebuild*, no cambia el modelo de escritura.
- Un comando = una transacción: `load stream → ejecutar → append → proyectar → outbox`.
- Las consultas nunca leen `es.events`; leen `read_*`.
- La unicidad global (microchip, código público) no puede garantizarse solo con eventos: se usan
  tablas de reserva en la misma transacción (`microchip_reservations`, `public_code_reservations`).

## Alternativas consideradas

- **CRUD en F1 y migrar en F2**: descartado (ver contexto).
- **Event store externo (EventStoreDB, Axon, Kafka como store)**: descartado en F1; PostgreSQL cubre el volumen
  esperado por años y reduce componentes. Revisable en F6.
- **ES para Identity**: descartado por PII y por ausencia de reglas de negocio con historia.
