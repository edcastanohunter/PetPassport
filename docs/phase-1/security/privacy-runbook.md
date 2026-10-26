# Runbook de privacidad F1: exportación y supresión manuales (PP-F1-SEC-009)

- Quién: un **super_admin** con acceso a la BD de staging/prod: rol de solo lectura
  `petpassport_reader` (miembro de `pp_app_reader`) para la exportación y `petpassport_migrator` para la
  supresión (nunca el rol de la API).
- Plazo: **≤ 15 días** naturales desde la solicitud.
- Registro: cada solicitud tiene un ticket (`PRIV-<n>`) con fechas de entrada, verificación, ejecución y respuesta.
- Aviso de privacidad: `/legal/privacy` (enlazado en el pie de todas las vistas). El texto definitivo
  llega con la revisión legal (PRD-020, M1.5).

En F1 los derechos se atienden a mano. La automatización y el crypto-shredding llegan en F5 (ADR-023).

## 1. Verificar la solicitud

1. La solicitud debe llegar **desde el email de la cuenta** o confirmarse respondiendo desde él.
   Nunca se atiende por otro canal sin esa confirmación.
2. Localizar la cuenta en **Administración → Usuarios** (búsqueda por email) y anotar el `id`
   (UUID; es el `sub` de Keycloak) en el ticket.
3. Si la persona es **último administrador** de una organización, se le pide primero transferir el rol
   (regla O2). La supresión no deja organizaciones sin administrador.

## 2. Exportación (derecho de acceso y portabilidad)

Conexión de solo lectura (`petpassport_reader`). Variables de psql: `uid` y `email`.

```sql
\set uid '00000000-0000-0000-0000-000000000000'
\set email 'persona@example.com'
\pset format unaligned
\pset tuples_only on
\o export-:uid.json

SELECT jsonb_pretty(jsonb_build_object(
  'generated_at', now(),
  'account', (SELECT to_jsonb(u) FROM identity.user_accounts u WHERE u.id = :'uid'),
  'memberships', (SELECT coalesce(jsonb_agg(to_jsonb(m) ORDER BY m.joined_at), '[]')
                  FROM read_partnernetwork.org_members m WHERE m.user_id = :'uid'),
  -- Mascotas de las que es o fue tutor: eventos completos mientras fue tutor
  -- (para las transferidas, hasta el evento de transferencia incluido).
  'pets', (
    WITH tenure AS (
      SELECT e.stream_id,
             max(e.stream_version) FILTER (
               WHERE e.event_type = 'PetGuardianshipTransferred'
                 AND e.payload->>'from_guardian_id' = :'uid') AS until_version
      FROM es.events e
      WHERE e.stream_type = 'Pet'
        AND :'uid' IN (e.payload->>'guardian_id', e.payload->>'from_guardian_id', e.payload->>'to_guardian_id')
      GROUP BY e.stream_id
    )
    SELECT coalesce(jsonb_agg(jsonb_build_object(
             'pet_id', t.stream_id,
             'events', (SELECT jsonb_agg(jsonb_build_object(
                          'type', e.event_type, 'version', e.schema_version,
                          'occurred_at', e.occurred_at, 'payload', e.payload) ORDER BY e.stream_version)
                        FROM es.events e
                        WHERE e.stream_id = t.stream_id
                          AND (t.until_version IS NULL OR e.stream_version <= t.until_version)))), '[]')
    FROM tenure t),
  -- PII de la persona en eventos de otros (invitaciones y transferencias recibidas): solo el pii{}.
  'mentions', (SELECT coalesce(jsonb_agg(jsonb_build_object(
                 'type', e.event_type, 'occurred_at', e.occurred_at, 'pii', e.payload->'pii')
                 ORDER BY e.global_position), '[]')
               FROM es.events e
               WHERE e.event_type IN ('MemberInvited', 'GuardianshipTransferInitiated')
                 AND lower(:'email') IN (lower(e.payload->'pii'->>'email'), lower(e.payload->'pii'->>'to_email'))),
  'audit', (SELECT coalesce(jsonb_agg(jsonb_build_object(
              'occurred_at', a.occurred_at, 'action', a.action, 'reason', a.reason,
              'as', CASE WHEN a.actor_id = :'uid' THEN 'actor' ELSE 'target' END)
              ORDER BY a.occurred_at), '[]')
            FROM platform.audit_log a WHERE :'uid' IN (a.actor_id, a.target_id))
));
\o
```

Además:

- **Keycloak**: consola de administración (desde la VPN) → realm `petpassport` → Users → el usuario →
  copiar atributos, roles y "Sessions". No se exportan credenciales.
- **Fotos** de las mascotas: claves `photo_object_key` de `read_petregistry.pets`. Se descargan del
  bucket y se adjuntan.

Entrega: un ZIP cifrado (contraseña por canal distinto) con `export-<uid>.json`, los datos de
Keycloak y las fotos, enviado al email de la cuenta. Anotar fecha de entrega en el ticket.

## 3. Supresión (derecho de supresión)

Alcance F1 (SEC-009): **suspensión + anonimización** de la cuenta. Los eventos no se modifican
(append-only, prohibido `UPDATE`/`DELETE` sobre `es.events`). La historia de las mascotas se conserva:
la anonimización afecta al tutor, no al animal.

1. **Suspender** desde Administración → Usuarios → Suspender, con motivo `Supresión PRIV-<n>`.
   Así se cortan el acceso en la plataforma, se deshabilita el usuario en Keycloak, se cierran sus
   sesiones y queda la entrada `admin.user.suspend` en `audit_log`.
2. **Cancelar transferencias pendientes** de sus mascotas (si existen) desde la ficha de la mascota o
   dejarlas caducar (7 días). No se ejecuta ninguna transición en su nombre.
3. **Anonimizar** en una sola transacción (`petpassport_migrator`; variables `uid`, `email`, `admin_uid`):

   ```sql
   BEGIN;
   UPDATE identity.user_accounts SET
     email = 'erased+' || id || '@invalid',
     email_verified = false,
     display_name = '',
     phone = NULL, address = NULL, emergency_contact = NULL, country = NULL,
     status = 'suspended', status_reason = 'Supresión PRIV-<n>',
     updated_at = now()
   WHERE id = :'uid';

   UPDATE read_petregistry.pets SET guardian_display_name = '' WHERE guardian_id = :'uid';
   UPDATE read_partnernetwork.invitations
     SET email = 'erased+' || id || '@invalid', status = CASE WHEN status = 'pending' THEN 'revoked' ELSE status END
   WHERE lower(email) = lower(:'email');

   INSERT INTO platform.audit_log (id, actor_id, actor_roles, action, target_type, target_id, reason, details)
   VALUES (gen_random_uuid(), :'admin_uid', '{super_admin}', 'admin.user.erase', 'user_account', :'uid',
           'Supresión PRIV-<n>', jsonb_build_object('ticket', 'PRIV-<n>'));
   COMMIT;
   ```

4. **Keycloak**: borrar el usuario (Users → Delete). Su UUID queda huérfano en la plataforma,
   que lo muestra como cuenta suspendida y anonimizada.
5. **Fotos**: las fotos son del animal y se conservan con la mascota.
6. **Responder** a la persona con lo suprimido y lo retenido (ver §5), y cerrar el ticket.

## 4. Inventario de PII en eventos (Registry, `PIISubjects`)

Verificado en M1.4 con `cmd/api/pii_inventory_test.go`. Si un evento con `pii{}` no implementa
`PIISubjects()` o no figura aquí, la prueba falla.

| Evento | Campo en `pii{}` | Sujeto | `PIISubjects()` | Motivo |
|---|---|---|---|---|
| `MemberInvited` | `email` | persona invitada | `nil` | al invitar puede no existir cuenta (no hay `UserID`) |
| `GuardianshipTransferInitiated` | `to_email` | destinatario de la transferencia | `nil` | ídem |

Otros datos personales fuera de `pii{}`, revisados:

- `OrganizationRegistered` / `OrganizationProfileUpdated`: `legal_name`, `tax_id`, `address` y
  `contact` (email/teléfono) son **datos de la organización**, no del tutor. En autónomos pueden ser
  personales: se señalan para la revisión legal (PRD-020).
- Campos `reason` de eventos y `audit_log`: texto libre de administradores. El modal de motivo indica
  "no incluyas datos personales".
- `guardian_id`, `user_id`, `actor_id`: identificadores seudónimos (UUID). Tras la supresión no
  enlazan con datos identificativos.

**Hallazgo para F5 (no se cambia la spec en este PR):** el diseño de SEC-009/ADR-023 cifra `pii{}`
con la clave del sujeto **por `UserID`**. Los dos únicos eventos con `pii{}` tienen sujetos
identificados por email y sin `UserID`. Hay que decidir en un PR de spec aparte cómo se asocia la clave:
por hash del email, por la clave de la organización o del tutor emisor, o resolviendo al `UserID` al
aceptar. Hasta entonces `PIISubjects()` devuelve `nil` de forma explícita.

## 5. Lo que se retiene y por qué (comunicar a la persona)

- **Eventos**: el email de la persona puede permanecer en `pii{}` de invitaciones o transferencias que
  recibió, porque el event store es append-only (integridad de la historia). Se eliminará de forma
  irreversible con crypto-shredding en F5. *Pendiente de validación legal (PRD-020).*
- **Reconstrucción de proyecciones**: si se reconstruyen proyecciones desde eventos, el email
  anonimizado de `read_partnernetwork.invitations` reaparecería. Cualquier rebuild debe reaplicar las
  supresiones registradas (`audit_log.action = 'admin.user.erase'`) antes de abrir tráfico.
- **Backups**: los datos permanecen en los backups gestionados hasta que caduque su retención (ventana
  PITR del proveedor). No se restauran backups anteriores a la supresión sin reaplicarla.
- **Auditoría**: las entradas de `audit_log` se conservan ≥ 2 años (SEC-007). No contienen PII en
  claro y referencian el UUID.
