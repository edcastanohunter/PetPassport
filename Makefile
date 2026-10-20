# PetPassport — objetivos de desarrollo (docs/phase-1/09 §1).
SHELL := /bin/bash
COMPOSE := docker compose -f infra/compose/docker-compose.yml
COMPOSE_OBS := $(COMPOSE) -f infra/compose/docker-compose.observability.yml
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)

.PHONY: help dev-up dev-down dev-reset dev-seed dev-certs dev-logs dev-obs migrate gen gen-check lint test test-integration e2e build images specs-status specs-check

help: ## Lista los objetivos
	@grep -E '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2}'

# --- Entorno de desarrollo ---
dev-certs: ## Genera certificados TLS locales (mkcert)
	infra/scripts/dev-certs.sh

dev-up: ## Levanta todo el entorno (traefik, postgres, keycloak, minio, mailpit, api, frontend)
	$(COMPOSE) up -d --build
	@echo "app: https://app.pp.localhost  api: https://api.pp.localhost  auth: https://auth.pp.localhost  mail: http://localhost:8025"

dev-down: ## Detiene el entorno
	$(COMPOSE) down

dev-reset: ## Detiene y borra volúmenes (datos, caches)
	$(COMPOSE) down -v

dev-logs: ## Logs de api y frontend
	$(COMPOSE) logs -f api frontend

dev-obs: ## Levanta el perfil de observabilidad (otel, prometheus, grafana, tempo)
	$(COMPOSE_OBS) up -d otel-collector prometheus grafana tempo

dev-seed: ## Crea usuarios semilla en Keycloak (owner@, vet@, admin@dev.local)
	infra/scripts/dev-seed.sh http://localhost:8180

migrate: ## Aplica migraciones (contenedor one-shot)
	$(COMPOSE) run --rm migrate

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

test-integration: ## Pruebas de integración (requiere Docker)
	cd backend && go test -race -tags integration -count=1 ./test/integration/...

e2e: ## E2E Playwright (se añade en M1.1)
	@echo "e2e: pendiente (PP-F1-QA-005, M1.5)"

# --- Build ---
build: ## Compila binarios y frontend
	cd backend && CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=$(VERSION)" -o bin/api ./cmd/api && \
	  CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o bin/migrate ./cmd/migrate && \
	  CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o bin/worker ./cmd/worker
	cd frontend && npm run build

images: ## Construye imágenes Docker
	docker build --build-arg VERSION=$(VERSION) -t petpassport/backend:$(VERSION) backend
	docker build -f frontend/Dockerfile -t petpassport/frontend:$(VERSION) .

# --- Specs ---
specs-status: ## Avance de las specs
	scripts/specs-status.sh

specs-check: ## Consistencia specs <-> tracking
	scripts/specs-status.sh --check
