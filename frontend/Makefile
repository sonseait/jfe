.DEFAULT_GOAL := help
.PHONY: help install dev generate typecheck lint test check build preview test-e2e format format-check docker-build docker-push

OPENAPI_SOURCE ?= ../backend/openapi.json
REGISTRY ?= sgp.vultrcr.com/695w
IMAGE_NAME ?= jfe-frontend
IMAGE_TAG ?= latest
IMAGE = $(if $(strip $(REGISTRY)),$(patsubst %/,%,$(strip $(REGISTRY)))/)$(IMAGE_NAME):$(IMAGE_TAG)

help:
	@echo "Targets: install dev generate typecheck lint test check build preview test-e2e format format-check docker-build docker-push"
	@echo "Docker: REGISTRY=<host/namespace> IMAGE_NAME=jfe-frontend IMAGE_TAG=latest"

install:
	pnpm install --frozen-lockfile

dev:
	pnpm dev

generate:
	pnpm exec openapi-typescript "$(OPENAPI_SOURCE)" -o src/native/generated.ts
	pnpm exec prettier --write src/native/generated.ts

typecheck:
	pnpm typecheck

lint:
	pnpm lint

test:
	pnpm test

check: typecheck lint test

build:
	pnpm build

preview:
	pnpm preview

test-e2e:
	pnpm test:e2e

format:
	pnpm format

format-check:
	pnpm format:check

docker-build:
	docker build --platform linux/amd64 --tag "$(IMAGE)" .

docker-push:
	@test -n "$(strip $(REGISTRY))" || { echo "Set REGISTRY to a registry host/namespace (for example ghcr.io/your-org)" >&2; exit 1; }
	$(MAKE) docker-build
	docker push "$(IMAGE)"
