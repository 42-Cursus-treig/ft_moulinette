.PHONY: help build up down restart logs ps sh sandbox run tidy fclean re

COMPOSE = docker compose

help:
	@echo "make build     - build les images (server + caddy)"
	@echo "make sandbox   - build l'image sandbox (correction des rendus)"
	@echo "make up        - lance les services en arriere-plan"
	@echo "make down      - stoppe et supprime les services"
	@echo "make restart   - redemarre les services"
	@echo "make logs      - suit les logs des services"
	@echo "make ps        - liste les services actifs"
	@echo "make sh        - ouvre un shell dans le conteneur server"
	@echo "make run       - lance le serveur en local (go run)"
	@echo "make tidy      - go mod tidy"
	@echo "make fclean    - down + suppression des volumes et images du projet"
	@echo "make re        - fclean + up"

build:
	$(COMPOSE) build

sandbox:
	$(COMPOSE) build sandbox-image

up: sandbox
	$(COMPOSE) up -d --build

down:
	$(COMPOSE) down

restart: down up

logs:
	$(COMPOSE) logs -f

ps:
	$(COMPOSE) ps

sh:
	$(COMPOSE) exec server sh

run:
	go run ./cmd/server

tidy:
	go mod tidy

fclean: down
	$(COMPOSE) down -v --rmi local

re: fclean up
