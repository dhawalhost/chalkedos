# Common tasks. Run `make help` to see this list.
#
# Assumes: golang-migrate and sqlc are installed (see README.md for
# install commands — neither is wired up yet, this Makefile targets
# them proactively so the commands exist the moment they are).

.PHONY: help run build test vet fmt migrate-up migrate-down sqlc-generate docker-build

help:
	@echo "make run              - run the API locally (go run ./cmd/api)"
	@echo "make build            - build the binary to bin/chalked-api"
	@echo "make test             - go test ./..."
	@echo "make vet              - go vet ./..."
	@echo "make fmt              - gofmt -w ."
	@echo "make migrate-up       - apply all migrations (requires DATABASE_URL in .env)"
	@echo "make migrate-down     - roll back the most recent migration"
	@echo "make sqlc-generate    - regenerate internal/db/sqlc from db/queries/*.sql"
	@echo "make docker-build     - build the production Docker image"

run:
	go run ./cmd/api

build:
	go build -o bin/chalked-api ./cmd/api

test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -w .

# Requires: brew install golang-migrate  (or see golang-migrate's own
# install docs for your platform)
migrate-up:
	migrate -path db/migrations -database "$$DATABASE_URL" up

migrate-down:
	migrate -path db/migrations -database "$$DATABASE_URL" down 1

# Requires: go install github.com/sqlc-dev/sqlc/cmd/sqlc@latest
sqlc-generate:
	sqlc generate

docker-build:
	docker build -t chalked-api:local .
