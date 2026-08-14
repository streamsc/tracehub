.PHONY: build test check dist clean

build:
	CGO_ENABLED=0 go build -trimpath -o tracehub ./cmd/tracehub

test:
	go test ./...

check:
	go vet ./...
	go test -race ./...

dist: clean
	mkdir -p dist
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -trimpath -o dist/tracehub-darwin-arm64 ./cmd/tracehub
	CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build -trimpath -o dist/tracehub-darwin-amd64 ./cmd/tracehub
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -o dist/tracehub-linux-arm64 ./cmd/tracehub
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -o dist/tracehub-linux-amd64 ./cmd/tracehub

clean:
	rm -f tracehub
	rm -rf dist
