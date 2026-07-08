FROM golang:1.22 AS build
WORKDIR /src
COPY go.mod go.sum* ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /moulinette ./cmd/server

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends git docker.io unrar-free ca-certificates \
    && rm -rf /var/lib/apt/lists/*
COPY --from=build /moulinette /usr/local/bin/moulinette
COPY tests /app/tests
WORKDIR /app
ENTRYPOINT ["moulinette"]