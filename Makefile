DEV_DB ?= postgres://zelda:zelda@127.0.0.1:5433/zelda?sslmode=disable
# The Go tests wipe their database, so they use a separate one.
TEST_DB ?= postgres://zelda:zelda@127.0.0.1:5433/zelda_test?sslmode=disable

.PHONY: web build run test test-go test-web db icons lint

web: ## build the editor bundle into web/dist
	cd web && npm ci && npm run build

build: web
	go build -o bin/zelda ./cmd/zelda

db: ## start local Postgres
	docker compose -f docker-compose.dev.yml up -d

run: db ## run against local Postgres (mail links are logged)
	ZELDA_DATABASE_URL=$(DEV_DB) ZELDA_BASE_URL=http://localhost:8080 go run ./cmd/zelda

test: test-go test-web

test-go: db
	@until docker compose -f docker-compose.dev.yml exec -T postgres pg_isready -U zelda >/dev/null 2>&1; do sleep 1; done
	-@docker compose -f docker-compose.dev.yml exec -T postgres createdb -U zelda zelda_test 2>/dev/null
	ZELDA_TEST_DATABASE_URL=$(TEST_DB) go test ./...

test-web:
	cd web && npm run typecheck && npm test

lint:
	go vet ./...
	test -z "$$(gofmt -l .)"

icons: ## regenerate brand icons from simple-icons
	cd scripts/icons && npm install && npm run gen
	gofmt -w internal/icons
