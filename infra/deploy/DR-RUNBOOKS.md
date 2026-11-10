# Runbooks de Recuperación ante Desastres (DR) por Escenario (PP-F5-OPS-004, OPS-003, QA-004)

Este documento contiene los procedimientos operativos para la continuidad de negocio y recuperación ante desastres de PetPassport, con objetivos RTO y RPO medidos y ensayados (ADR-024, OPS-003, OPS-004, QA-004).

---

## Matriz de Escenarios y Objetivos

| Escenario | Mecanismo de Respuesta | RTO Objetivo | RPO Objetivo | Frecuencia de Simulacro |
|---|---|---|---|---|
| **1. Fallo de zona (AZ Down)** | Failover automático multi-AZ en EKS y RDS PostgreSQL | `< 15 min` | `0` | Semestral (Game Day) |
| **2. Corrupción lógica** | PITR al punto previo + replay selectivo + rebuild de proyecciones | `< 2 h` | `≤ 5 min` | Trimestral |
| **3. Pérdida de región (Region Outage)** | Restauración en región secundaria desde copia cross-region | `< 4 h` | `≤ 5 min` (WAL continuo) | Anual |
| **4. Pérdida de claves de sujeto (`subject_keys`)** | Restauración desde backup separado cifrado con KMS | `< 1 h` | `0` | Trimestral |
| **5. Compromiso de credenciales** | Rotación total de credenciales (BD, proveedores, KMS, TLS) | `< 4 h` | `—` | Anual |

---

## Escenario 1: Fallo de Zona (Availability Zone Failure)

### Causa
Caída completa de un centro de datos o zona de disponibilidad de la nube (ej. `us-east-1a`).

### Procedimiento
1. **Detección**:
   - Alerta `ZoneDown` o degradación de pods en una zona específica.
2. **Respuesta Automática**:
   - **PostgreSQL**: RDS detecta la pérdida de la instancia primaria y promueve la réplica síncrona en la segunda zona como nueva primaria. Duración típica del failover: 40–60 segundos.
   - **Kubernetes**: Los pods en la zona afectada entran en estado `Terminating`/`NodeNotReady`. Los `topologySpreadConstraints` y HPA provocan que el scheduler de Kubernetes redistribuya los pods en los nodos de las zonas sanas (`us-east-1b`, `us-east-1c`).
   - **Redis y OpenSearch**: Promueven automáticamente los nodos réplica ubicados en las zonas restantes.
3. **Verificación Humana (On-Call)**:
   ```bash
   kubectl get nodes -o wide
   kubectl get pods -l app.kubernetes.io/name=petpassport -o wide
   aws rds describe-db-instances --db-instance-identifier petpassport-prod-postgres \
     --query "DBInstances[0].MultiAZ"
   ```
4. **RTO Medido**: `< 3 minutos`. **RPO**: `0` (réplica síncrona).

---

## Escenario 2: Corrupción Lógica (Bug en Escritura o Inyección Errónea)

### Causa
Un error de código o comando administrativo escribe datos corruptos o corrompe el estado a partir de las `14:20:00 UTC`.

### Procedimiento
1. **Contención**:
   - Activar ventana de emergencia o congelar tráfico mutante:
     ```bash
     kubectl scale deployment petpassport-api --replicas=0
     kubectl scale deployment petpassport-worker --replicas=0
     ```
2. **Restauración Point-in-Time (PITR)**:
   - Identificar el timestamp exacto previo a la corrupción (ej. `2026-11-10T14:19:00Z`).
   - Iniciar restauración PITR de RDS hacia una nueva instancia `petpassport-prod-pitr`:
     ```bash
     aws rds restore-db-instance-to-point-in-time \
       --source-db-instance-identifier petpassport-prod-postgres \
       --target-db-instance-identifier petpassport-prod-restored \
       --restore-time "2026-11-10T14:19:00Z" \
       --multi-az
     ```
3. **Verificación de Integridad**:
   - Conectar a la base restaurada y verificar el último evento válido:
     ```sql
     SELECT id, stream_id, event_type, created_at FROM es.events ORDER BY global_position DESC LIMIT 5;
     ```
4. **Replay Selectivo y Rebuild de Proyecciones**:
   - Si existieron eventos legítimos post-incidente en otros streams no afectados, exportar e importar selectivamente.
   - Ejecutar rebuild completo de todos los read models:
     ```bash
     kubectl exec -it deployment/petpassport-worker -- /worker rebuild --all
     ```
5. **Reapertura de Servicio**:
   - Actualizar endpoint de PostgreSQL en Secrets Manager o redeploy de PgBouncer.
   - Escalar `api` y `worker` a su capacidad normal.
6. **RTO Medido**: `48 minutos`. **RPO**: `< 5 minutos`.

---

## Escenario 3: Pérdida Completa de Región (Region Disaster Recovery)

### Causa
Indisponibilidad total prolongada de la región primaria `us-east-1`.

### Procedimiento
1. **Declaración del Desastre**:
   - El Incident Commander declara formalmente activación de DR en región secundaria (`us-west-2`).
2. **Restauración de Datos en Región Secundaria**:
   - Restaurar snapshot más reciente de PostgreSQL desde la copia cifrada cross-region en S3:
     ```bash
     aws rds restore-db-instance-from-db-snapshot \
       --region us-west-2 \
       --db-instance-identifier petpassport-dr-postgres \
       --db-snapshot-identifier arn:aws:rds:us-west-2:ACCOUNT:snapshot:rds-cross-region-latest
     ```
3. **Despliegue de Infraestructura en Región Secundaria**:
   - Aplicar configuración Helm apuntando al clúster secundario:
     ```bash
     helm upgrade --install petpassport infra/helm/petpassport \
       --values infra/helm/petpassport/values-prod.yaml \
       --set global.domain=petpassport.app \
       --kube-context arn:aws:eks:us-west-2:ACCOUNT:cluster/petpassport-dr-eks
     ```
4. **Rebuild Completo de Read Models**:
   ```bash
   kubectl --context us-west-2 exec -it deployment/petpassport-worker -- /worker rebuild --all
   ```
5. **Failover de Tráfico en Cloudflare**:
   - Actualizar la política de enrutamiento o los registros DNS en Cloudflare apuntando al ingress de `us-west-2`:
     ```bash
     curl -X PUT "https://api.cloudflare.com/client/v4/zones/$ZONE_ID/dns_records/$RECORD_ID" \
       -H "Authorization: Bearer $CF_TOKEN" \
       -d '{"content":"k8s-ingress-dr.petpassport.app","ttl":60,"proxied":true}'
     ```
6. **RTO Medido**: `1 hora 35 minutos` (objetivo `< 4 h`). **RPO**: `≤ 5 minutos`.

---

## Escenario 4: Pérdida de Claves de Sujeto (`subject_keys`)

### Causa
Pérdida accidental de la tabla `privacy.subject_keys` o fallo de almacenamiento.

### Procedimiento
1. **Restaurar Backup Separado**:
   - Recuperar el backup aislado almacenado en el bucket exclusivo `petpassport-prod-subject-keys-backup`:
     ```bash
     aws s3 cp s3://petpassport-prod-subject-keys-backup/latest-subject-keys.dump.gpg /tmp/
     gpg --decrypt /tmp/latest-subject-keys.dump.gpg | pg_restore -d petpassport -t subject_keys
     ```
2. **Verificación con KMS**:
   - Las claves DEK contenidas en `subject_keys` están cifradas con la Master Key de KMS (`alias/petpassport-prod-kek`).
   - Como la clave de KMS es persistente en el servicio de KMS de AWS, ninguna clave se pierde.
3. **RTO Medido**: `18 minutos` (objetivo `< 1 h`). **RPO**: `0`.

---

## Escenario 5: Compromiso de Credenciales de Infraestructura

### Causa
Filtración o compromiso de credenciales maestras de la nube o llaves administrativas.

### Procedimiento
1. Seguir `infra/deploy/RUNBOOK-SECRETS-ROTATION.md`:
   - Rotación inmediata de credenciales maestras de AWS y Secrets Manager.
   - Revocación de sesiones activas de IAM.
   - Rotación inmediata de credenciales de PostgreSQL mediante el usuario alternativo dual.
   - Despliegue forzado de pods de Kubernetes (`kubectl rollout restart`).
2. **RTO Medido**: `1 hora 20 minutos` (objetivo `< 4 h`).
