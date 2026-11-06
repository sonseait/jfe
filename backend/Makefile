.DEFAULT_GOAL := help
.PHONY: help dev scanner downloader transcoder generate openapi migrate lint build

-include .env
export

help:
	@echo "Targets: dev scanner downloader transcoder generate openapi migrate lint build"

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

lint:
	go vet ./...

build:
	mkdir -p bin
	go build -o bin/api ./cmd/api
	go build -o bin/scanner ./cmd/scanner
	go build -o bin/downloader ./cmd/downloader
	go build -o bin/transcoder ./cmd/transcoder
