# PetPassport — objetivos de desarrollo (docs/phase-1/09 §1).
SHELL := /bin/bash
# Motor de contenedores: Podman rootless (ADR-026). El entorno dev son unidades
# Quadlet (infra/quadlet/dev) bajo `systemctl --user`; sin compose ni root. En
# Windows/macOS dev-quadlet.sh se reejecuta dentro de la podman machine.
ENGINE ?= podman
DEV := infra/scripts/dev-quadlet.sh
# testcontainers usa la API compatible de Podman. En Linux, el socket del servicio
# (`systemctl --user enable --now podman.socket`); con podman machine (Windows/macOS)
# la máquina ya expone la API en la ruta por defecto y no se fija DOCKER_HOST.
PODMAN_SOCKET := $(shell [ "$$($(ENGINE) info --format '{{.Host.ServiceIsRemote}}' 2>/dev/null)" = false ] && $(ENGINE) info --format '{{.Host.RemoteSocket.Path}}' 2>/dev/null)
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)

.PHONY: help dev-up dev-down dev-reset dev-seed dev-certs dev-logs dev-obs dev-status dev-export migrate gen gen-check lint test test-integration e2e load-test build images specs-status specs-check

help: ## Lista los objetivos
	@grep -E '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2}'

# --- Entorno de desarrollo ---
dev-certs: ## Genera certificados TLS locales (mkcert)
	infra/scripts/dev-certs.sh

dev-up: ## Levanta todo el entorno (traefik, postgres, keycloak, minio, mailpit, api, frontend)
	$(DEV) up

dev-down: ## Detiene el entorno
	$(DEV) down

dev-reset: ## Detiene y borra volúmenes (datos, caches)
	$(DEV) reset

dev-logs: ## Logs de api y frontend (journal)
	$(DEV) logs api frontend

dev-status: ## Estado de las unidades del entorno
	$(DEV) status

dev-obs: ## Levanta la observabilidad (otel, tempo, prometheus, alertmanager, blackbox, loki, alloy, grafana)
	$(DEV) obs

dev-export: ## Respaldo de es.events contra el MinIO local (DAT-009)
	$(DEV) export

dev-seed: ## Crea usuarios semilla en Keycloak (owner@, vet@, admin@dev.local)
	infra/scripts/dev-seed.sh http://localhost:8180

migrate: ## Aplica migraciones (unidad oneshot pp-dev-migrate)
	$(DEV) migrate

# --- Generación de código ---
gen: ## Regenera servidor Go y tipos TS desde OpenAPI
	cd backend && go generate ./api/...
	cd frontend && npm run gen:api

gen-check: gen ## Falla si la generación produce cambios no commiteados
	git diff --exit-code -- backend/api/gen frontend/src/shared/api/schema.d.ts

# --- Calidad ---
lint: ## Lint backend y frontend
	cd backend && golangci-lint run ./...
	cd frontend && npm run lint

test: ## Pruebas unitarias
	cd backend && go test -race -cover ./...
	cd frontend && npm test

test-integration: ## Pruebas de integración (requiere Podman con su API activa; testcontainers)
	cd backend && DOCKER_HOST=$${DOCKER_HOST:-$(PODMAN_SOCKET)} TESTCONTAINERS_RYUK_DISABLED=true \
	  go test -race -tags integration -count=1 ./test/integration/...

e2e: ## E2E Playwright de los flujos críticos + axe (requiere make dev-up; PP-F1-QA-005)
	cd e2e && npm ci --no-audit --no-fund && npx playwright install chromium && npx playwright test

load-test: ## k6 ligero contra staging: 50 VUs, 5 min, p95 < 300 ms (requiere k6 y K6_* ; ver tests/load)
	k6 run tests/load/k6/api-smoke.js

# --- Build ---
build: ## Compila binarios y frontend
	cd backend && CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=$(VERSION)" -o bin/api ./cmd/api && \
	  CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o bin/migrate ./cmd/migrate && \
	  CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o bin/worker ./cmd/worker && \
	  CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o bin/backup ./cmd/backup
	cd frontend && npm run build

images: ## Construye las imágenes OCI con Podman
	$(ENGINE) build --build-arg VERSION=$(VERSION) -t localhost/petpassport-backend:$(VERSION) backend
	$(ENGINE) build -f frontend/Dockerfile -t localhost/petpassport-frontend:$(VERSION) .

# --- Specs ---
specs-status: ## Avance de las specs
	scripts/specs-status.sh

specs-check: ## Consistencia specs <-> tracking
	scripts/specs-status.sh --check
