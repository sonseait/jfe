.DEFAULT_GOAL := help
.PHONY: help dev scanner downloader transcoder generate openapi migrate test test-integration lint build docker-build docker-push

-include .env
export

REGISTRY ?= sgp.vultrcr.com/695w
IMAGE_NAME ?= jfe-backend
IMAGE_TAG ?= latest
IMAGE = $(if $(strip $(REGISTRY)),$(patsubst %/,%,$(strip $(REGISTRY)))/)$(IMAGE_NAME):$(IMAGE_TAG)

help:
	@echo "Targets: dev scanner downloader transcoder generate openapi migrate test test-integration lint build docker-build docker-push"
	@echo "Docker: REGISTRY=<host/namespace> IMAGE_NAME=jfe-backend IMAGE_TAG=latest"

dev:
	go run ./cmd/api

scanner:
	go run ./cmd/scanner

downloader:
	go run ./cmd/downloader

transcoder:
	go run ./cmd/transcoder

generate:
	sqlc generate
	$(MAKE) openapi

openapi:
	go run ./cmd/api openapi > openapi.json

migrate:
	@test -n "$$DATABASE_URL" || { echo "Set DATABASE_URL in backend/.env" >&2; exit 1; }
	migrate -path db/migrations -database "$$DATABASE_URL" up

test:
	go test ./...

test-integration:
	bash scripts/test-integration.sh

lint:
	go vet ./...

build:
	mkdir -p bin
	go build -o bin/api ./cmd/api
	go build -o bin/scanner ./cmd/scanner
	go build -o bin/downloader ./cmd/downloader
	go build -o bin/transcoder ./cmd/transcoder

docker-build:
	docker build --platform linux/amd64 --tag "$(IMAGE)" .

docker-push:
	@test -n "$(strip $(REGISTRY))" || { echo "Set REGISTRY to a registry host/namespace (for example ghcr.io/your-org)" >&2; exit 1; }
	$(MAKE) docker-build
	docker push "$(IMAGE)"
