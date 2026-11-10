# Runbook de Rotación Programada de Secretos (PP-F5-INF-002)

Este documento especifica la política, los intervalos y los procedimientos automatizados y manuales para la rotación programada de credenciales en Kubernetes mediante External Secrets Operator (ESO) y AWS Secrets Manager (ADR-024, INF-002).

## 1. Calendario de rotación

| Tipo de secreto | Frecuencia | Mecanismo | Impacto de servicio |
|---|---|---|---|
| **Credenciales de BD (PostgreSQL)** | Trimestral (90 días) | Dual-user (usuario A y usuario B con permisos idénticos) | Cero downtime |
| **Credenciales de proveedores (Stripe/Wompi/DIAN)** | Semestral (180 días) | Clave secundaria en proveedor -> actualización secret -> rotación de clave primaria | Cero downtime |
| **KMS Master Key (KEK)** | Anual (365 días) | AWS KMS automatic key rotation (re-wrap transparente) | Cero downtime |
| **Claves de tokens internos (soporte/impersonación)** | Mensual (30 días) | Clave actual + clave previa (overlap 48h de verificación) | Cero downtime |
| **Certificados TLS de origen** | Anual | cert-manager / Cloudflare Origin CA | Cero downtime |

## 2. Procedimiento: Base de datos (Dual-user rotation)

1. El DBA o script de rotación crea la credencial alternativa en PostgreSQL:
   ```sql
   -- Si la actual es pp_api_a:
   CREATE USER pp_api_b WITH PASSWORD '<nuevo-secreto-generado>';
   GRANT pp_api_role TO pp_api_b;
   ```
2. Se actualiza el secret en AWS Secrets Manager:
   ```bash
   aws secretsmanager update-secret --secret-id petpassport/production \
     --secret-string '{"db_user":"pp_api_b","db_password":"<nuevo-secreto-generado>"}'
   ```
3. External Secrets Operator refresca el Secret en Kubernetes (`refreshInterval: 1h` o trigger manual):
   ```bash
   kubectl annotate externalsecret petpassport-secrets force-sync=$(date +%s) -n petpassport
   ```
4. Se reinician gradualmente los pods de API (`kubectl rollout restart deployment petpassport-api`).
5. Tras 24 horas de verificación sin conexiones del usuario antiguo:
   ```sql
   DROP USER pp_api_a;
   ```

## 3. Procedimiento: KEK de KMS (Envelope Encryption)

1. AWS KMS tiene habilitada la rotación anual automática (`enable_key_rotation = true` en `infra/terraform/modules/kms/main.tf`).
2. AWS genera una nueva versión de la clave sin cambiar el Key ARN ni invalidar los datos cifrados previamente.
3. Para re-cifrar DEKs existentes (Crypto-shredding ADR-023), se ejecuta el comando de mantenimiento:
   ```bash
   kubectl exec -it deployment/petpassport-worker -- /worker kms-rewrap-deks
   ```

## 4. Auditoría de acceso

- Cada lectura de secretos desde AWS Secrets Manager o KMS se registra en AWS CloudTrail.
- Alerta configurada para accesos anómalos o intentos de descifrado masivos (`KMSDecryptSpike`).
