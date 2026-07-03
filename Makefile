NAME		= inception
COMPOSE		= srcs/docker-compose.yml
DATA_DIR	= /home/kyfontan/data

all: up

up: setup
	docker compose -f Docker/docker-compose.yml up

down:
	docker compose -f Docker/docker-compose.yml down

check:
	docker ps

clean-down:
	docker compose -f Docker/docker-compose.yml down -v

clean: down
	docker image prune -a -f

clean-all: clean
	docker system prune -a

re: clean all

.PHONY: all up down check clean-down clean clean-all re