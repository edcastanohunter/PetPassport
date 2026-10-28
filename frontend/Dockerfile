# syntax=docker/dockerfile:1.7
# Imagen del frontend (docs/phase-1/09 §4). Contexto de build = raíz del monorepo:
#   podman build -f frontend/Dockerfile .
FROM node:22-alpine AS build
WORKDIR /src
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY frontend/ ./
COPY backend/api/openapi.yaml ../backend/api/openapi.yaml
RUN npm run gen:api && npm run build

FROM nginxinc/nginx-unprivileged:1.27-alpine
LABEL org.opencontainers.image.title="petpassport-frontend" \
      org.opencontainers.image.source="https://github.com/edcastanohunter/PetPassport"
COPY frontend/nginx.conf /etc/nginx/conf.d/default.conf
COPY --from=build /src/dist /usr/share/nginx/html
# /config.json se monta por entorno (en dev, Vite sirve public/; Quadlet en staging/prod, Helm en F3+); el de public/ es el de dev.
EXPOSE 8080
