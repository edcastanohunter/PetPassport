# Plan de Migración Blue/Green: VM (Podman Quadlet) → Kubernetes Multi-Zona (PP-F5-OPS-001, OPS-002)

- **Objetivo**: Migrar el backend (`api`, `worker`), frontend y servicios auxiliares desde la VM única (ADR-008) a clúster Kubernetes gestionado multi-zona (EKS) con PostgreSQL multi-AZ y connection pooler PgBouncer (ADR-024, OPS-001, OPS-002).
- **Criterio de éxito**: Cero pérdida de datos (`count(es.events)` y checksums idénticos), ventana de corte `< 15 minutos`, disponibilidad SLOs cumplidos 7 días posteriores.
- **Rollback**: Disponible durante 24 horas mediante switch DNS en WAF/Cloudflare (TTL 60s).

---

## 1. Calendario y estrategia de ejecución

1. **Staging (Semana T-4)**:
   - Despliegue completo de la infraestructura en Kubernetes con `values-staging.yaml`.
   - Ensayo general del corte con datos a escala y réplica lógica.
   - Período de estabilización de 4 semanas en staging antes de la migración de producción.
2. **Producción (Semana T)**:
   - Ventana de mantenimiento programada en horario de baja actividad (domingo 02:00–04:00 COT).
   - Anuncio previo de mantenimiento en la Status Page con ≥ 72 h de antelación.

---

## 2. Fases de la migración

### Fase A: Preparación previa (T-7 días)

1. **Aprovisionamiento IaC**:
   - Ejecutar `terraform apply` en `infra/terraform/environments/prod` (VPC multi-AZ, EKS, RDS multi-AZ, Redis multi-AZ, OpenSearch multi-AZ, KMS, S3 buckets con Object Lock).
2. **Despliegue del chart de Helm en EKS**:
   - Desplegar `infra/helm/petpassport` con `values-prod.yaml`:
     - `api` con 3 réplicas + HPA + PDB (`minAvailable: 2`) + `topologySpreadConstraints`.
     - `worker` con 2 réplicas + PDB (`minAvailable: 1`).
     - `frontend` con 3 réplicas.
     - Connection pooler `pgbouncer` con 2 réplicas.
     - `NetworkPolicy` activa.
     - External Secrets sincronizados con Secrets Manager.
3. **Réplica lógica de PostgreSQL (VM → RDS Multi-AZ)**:
   - Configurar publicación en la BD origen (VM):
     ```sql
     CREATE PUBLICATION pp_migration_pub FOR ALL TABLES;
     ```
   - Configurar suscripción en RDS PostgreSQL 17 Multi-AZ:
     ```sql
     CREATE SUBSCRIPTION pp_migration_sub
     CONNECTION 'host=vm.origin.internal port=5432 user=pp_migrator password=... dbname=petpassport'
     PUBLICATION pp_migration_pub;
     ```
   - Monitorear lag de replicación hasta que `replay_lag = 0 bytes`.

---

### Fase B: Ventana de corte (< 15 min)

```
[Cliente] ──► [Cloudflare WAF (TTL 60s)]
                   │
         ┌─────────┴─────────┐
         │ (Switch DNS)      │
         ▼                   ▼
    [Blue: VM]          [Green: K8s]
 (Modo drenado)      (Activo Multi-AZ)
```

1. **Inicio de ventana (T = 00:00)**:
   - Activar banner de mantenimiento en Status Page.
   - Poner Traefik de la VM en modo drenado/congelación de escrituras temporales (HTTP 503 con Retry-After: 60s para mutaciones).
2. **Verificación de paridad de datos (T + 00:03)**:
   - Confirmar que no hay escrituras pendientes en `es.events`:
     ```sql
     -- En VM y RDS simultáneamente:
     SELECT max(global_position), count(*) FROM es.events;
     ```
   - Validar que ambos valores coinciden exactamente.
3. **Promoción de RDS y corte de réplica (T + 00:05)**:
   - Detener suscripción lógica en RDS:
     ```sql
     ALTER SUBSCRIPTION pp_migration_sub DISABLE;
     DROP SUBSCRIPTION pp_migration_sub;
     ```
   - Reiniciar secuencias de IDs en RDS al valor máximo actual.
4. **Validación de servicios Green en Kubernetes (T + 00:08)**:
   - Ejecutar suite de smoke tests contra el ingress interno de EKS:
     ```bash
     K8S_INGRESS=https://k8s-ingress.petpassport.app infra/deploy/smoke.sh
     ```
5. **Cambio de DNS en Cloudflare (T + 00:10)**:
   - Actualizar CNAMEs de `app.`, `api.`, `auth.`, `partners.` para apuntar al Ingress NLB del clúster EKS.
   - TTL de Cloudflare está en 60 segundos; la propagación es inmediata a nivel de proxy.
6. **Verificación post-corte (T + 00:13)**:
   - Comprobar flujo de peticiones entrantes en los pods de `petpassport-api`.
   - Comprobar que los workers procesan proyecciones y outbox sin retrasos (`projection_lag < 1s`).
   - Fin de ventana de corte: tiempo total transcurrido < 15 min.

---

### Fase C: Observación y Rollback (24 horas)

1. **Monitoreo intensivo**:
   - Supervisión de métricas: tasa de error 5xx < 0.1%, latencia p95 < 300 ms, utilización de conexiones en PgBouncer.
2. **Procedimiento de Rollback (en caso de incidencia crítica)**:
   - Si se detecta fallo bloqueante en EKS:
     1. Revertir CNAMEs en Cloudflare hacia la IP origen de la VM.
     2. Tiempo de rollback: `< 2 minutos`.
3. **Cierre de migración (T + 24 horas)**:
   - Tras 24 horas con SLOs 100% verdes, apagar los servicios en la VM origen (`systemctl --user stop pp-*`).
   - Desmantelar la VM tras 7 días de retención de seguridad.
