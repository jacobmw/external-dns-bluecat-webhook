.PHONY: build test tidy

build:
	go build -o bin/external-dns-bluecat-webhook ./cmd/webhook

test:
	go test ./...

tidy:
	go mod tidy
