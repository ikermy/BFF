.PHONY: help dev-up test-up prod-up obs-up up-all down logs ps config-dev config-test config-prod config-obs test

COMPOSE := docker compose
BASE := -f docker-compose.yml
DEV := -f docker-compose.yml -f docker-compose.dev.yml
TEST := -f docker-compose.yml -f docker-compose.dev.yml -f docker-compose.test.yml
PROD := -f docker-compose.yml -f docker-compose.prod.yml
OBS := -f docker-compose.yml -f docker-compose.observability.yml

help:
	@echo "Available targets:"
	@echo "  make dev-up      # local dev stack with mock downstreams"
	@echo "  make test-up     # test stack with relaxed auth/notifications"
	@echo "  make prod-up     # prod-like stack with real downstream URLs from env"
	@echo "  make obs-up      # observability stack (Prometheus + Loki + Promtail + Grafana)"
	@echo "  make up-all      # app stack + observability stack"
	@echo "  make down        # stop all containers and remove orphans"
	@echo "  make logs        # follow logs for app + observability"
	@echo "  make ps          # list running services"
	@echo "  make config-dev  # render docker compose config for dev"
	@echo "  make config-test # render docker compose config for test"
	@echo "  make config-prod # render docker compose config for prod-like"
	@echo "  make config-obs  # render docker compose config for observability"
	@echo "  make test        # run Go tests"

dev-up:
	$(COMPOSE) $(DEV) up -d --build

test-up:
	$(COMPOSE) $(TEST) up -d --build bff worker zookeeper kafka redis

prod-up:
	$(COMPOSE) $(PROD) up -d --build

obs-up:
	$(COMPOSE) $(OBS) up -d prometheus loki promtail grafana

up-all:
	$(COMPOSE) $(OBS) up -d --build

down:
	$(COMPOSE) $(OBS) down --remove-orphans

logs:
	$(COMPOSE) $(OBS) logs -f --tail=200 bff worker prometheus loki promtail grafana

ps:
	$(COMPOSE) $(OBS) ps

config-dev:
	$(COMPOSE) $(DEV) config

config-test:
	$(COMPOSE) $(TEST) config

config-prod:
	$(COMPOSE) $(PROD) config

config-obs:
	$(COMPOSE) $(OBS) config

test:
	go test ./... -count=1

