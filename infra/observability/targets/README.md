# Targets de Prometheus (file_sd)

Job `tls` (alerta `CertificateExpiringSoon`). En staging, `infra/deploy/deploy.sh`
escribe `tls.json` con las URLs públicas del entorno en el directorio montado
(`$PP_STATE_DIR/prom-targets`). En dev este directorio no tiene targets.
