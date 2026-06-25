.PHONY: build test integration lint vet fmt generate schema docs flake-hash run-stdio run-http

build:
	go build ./...

test:
	go test ./...

integration:
	go test -tags=integration ./...

vet:
	go vet ./...

lint:
	golangci-lint run

fmt:
	gofmt -s -w .

generate: schema

schema:
	go run ./cmd/gen-schema

docs:
	gomarkdoc --output 'docs/{{.Dir}}.md' ./...

flake-hash:
	bash scripts/update-flake-vendor-hash.sh

run-stdio:
	go run ./cmd/mongodb-mcp --config ./config.json

run-http:
	go run ./cmd/mongodb-mcp --transport http --config ./config.json
