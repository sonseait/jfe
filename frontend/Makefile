.DEFAULT_GOAL := help
.PHONY: help install dev generate typecheck lint check build preview format format-check

OPENAPI_SOURCE ?= ../backend/openapi.json

help:
	@echo "Targets: install dev generate typecheck lint check build preview format format-check"

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

check: typecheck lint

build:
	pnpm build

preview:
	pnpm preview

format:
	pnpm format

format-check:
	pnpm format:check
