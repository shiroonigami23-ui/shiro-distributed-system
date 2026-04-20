.PHONY: test build run docker-build docker-run tidy fmt

test:
	go test ./...

build:
	go build ./...

run:
	go run ./cmd/controlplane

docker-build:
	docker build -t shiro-distributed-system:local .

docker-run:
	docker run --rm -p 8080:8080 --env-file .env.example shiro-distributed-system:local

tidy:
	go mod tidy

fmt:
	gofmt -w ./cmd ./internal
