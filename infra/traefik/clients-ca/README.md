# CA de Certificados de Clientes mTLS para Partners B2B

Este directorio contiene la Autoridad de Certificación (CA) de clientes utilizada por Traefik para la autenticación mutua TLS (mTLS) en el borde B2B (`PP-F4-ARQ-010`, `PP-F4-INF-001`).

## Arquitectura

- **Entrypoint**: `partners-mtls` (puerto `:8443` o SNI dedicado).
- **Validación**: Traefik verifica criptográficamente que la conexión entrante presente un certificado de cliente válido firmado por la CA (`clientAuthType: RequireAndVerifyClientCert`).
- **Archivo de CA**: `/etc/traefik/clients-ca/client-ca.pem`.

## Generación de Certificados para Pruebas y Desarrollo

Para generar la CA de desarrollo y un certificado de cliente para un partner:

```bash
# 1. Generar clave privada y certificado de la CA raíz de clientes
openssl genrsa -out client-ca-key.pem 4096
openssl req -x509 -new -nodes -key client-ca-key.pem -sha256 -days 1825 \
  -out client-ca.pem \
  -subj "/C=CO/ST=Bogota/O=PetPassport/OU=Partners/CN=PetPassport Partner Client CA"

# 2. Generar certificado para un partner específico (ej. partner-farmacia-aliada)
openssl genrsa -out partner-client-key.pem 2048
openssl req -new -key partner-client-key.pem -out partner-client.csr \
  -subj "/C=CO/ST=Bogota/O=Farmacia Aliada SAS/OU=Integration/CN=partner-farmacia-aliada"

openssl x509 -req -in partner-client.csr -CA client-ca.pem -CAkey client-ca-key.pem \
  -CAcreateserial -out partner-client.pem -days 365 -sha256
```

## Uso en Peticiones B2B con curl

```bash
curl -k --cert partner-client.pem --key partner-client-key.pem \
  -H "Authorization: Bearer <partner_jwt_token>" \
  https://partners.pp.localhost:8443/partner/v1/orders
```
