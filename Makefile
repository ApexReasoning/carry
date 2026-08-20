GO ?= go

.PHONY: build check check-go check-product check-web format generate test test-db

build:
	$(GO) build ./cmd/carry-server ./cmd/carry
	pnpm --dir apps/web build

format:
	$(GO) fmt ./...
	pnpm --dir apps/web format:write

generate:
	$(GO) tool sqlc generate
	pnpm --dir apps/web generate

test:
	$(GO) test ./...
	pnpm --dir apps/web test

test-db:
	./scripts/test-db ./internal/postgres/...
	./scripts/test-db ./cmd/carry-server

check-go:
	./scripts/check-go-format
	$(GO) tool sqlc diff
	$(GO) mod tidy -diff
	./scripts/check-boundaries
	$(GO) vet ./...
	$(GO) test ./...
	$(MAKE) test-db
	$(GO) build ./cmd/carry-server ./cmd/carry

check-web:
	./scripts/check-web-generated
	pnpm --dir apps/web format
	pnpm --dir apps/web lint
	pnpm --dir apps/web typecheck
	pnpm --dir apps/web test
	pnpm --dir apps/web build

check-product:
	./scripts/test-db ./e2e/...

check: check-go check-web check-product
