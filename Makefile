.DEFAULT_GOAL := help

GO_PACKAGES := ./...
DOCKER_COMPOSE_FILE := docker-compose.dev.yml
CONTROL_ONE_CONFIG ?= controlplane/config/controlplane.example.yaml

.PHONY: help go-test go-lint go-fmt go-run controlplane controlone-agent ui-install ui-build ui-test docker-up docker-down docker-logs

help:
	@echo "Control One developer targets"
	@echo "  make go-test        # run go test ./..."
	@echo "  make go-lint        # run go vet ./..."
	@echo "  make go-fmt         # format go sources"
	@echo "  make go-run         # run control plane locally"
	@echo "  make ui-install     # install UI dependencies"
	@echo "  make ui-build       # build UI"
	@echo "  make ui-test        # run UI tests"
	@echo "  make docker-up      # start dev stack"
	@echo "  make docker-down    # stop dev stack"
	@echo "  make docker-logs    # tail dev stack logs"

go-test:
	go test $(GO_PACKAGES)

go-lint:
	go vet $(GO_PACKAGES)

go-fmt:
	go fmt $(GO_PACKAGES)

go-run:
	go run ./controlplane/cmd/controlplane --config $(CONTROL_ONE_CONFIG)

ui-install:
	cd ui && npm install

ui-build:
	cd ui && npm run build

ui-test:
	cd ui && npm test -- --watch=false

docker-up:
	docker compose -f $(DOCKER_COMPOSE_FILE) up -d

docker-down:
	docker compose -f $(DOCKER_COMPOSE_FILE) down

docker-logs:
	docker compose -f $(DOCKER_COMPOSE_FILE) logs -f
