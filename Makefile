.PHONY: build test lint snapshot sync-templates install clean

build:
	go build -o bin/gignore ./cmd/gignore

test:
	go test -race ./...

lint:
	gofmt -l . | (! grep .)
	go vet ./...
	golangci-lint run

# Build every release artifact locally without publishing.
snapshot:
	TAP_GITHUB_TOKEN=unused goreleaser release --snapshot --clean --skip=publish

# Refresh the embedded templates from github/gitignore.
sync-templates:
	rm -rf .cache/github-gitignore
	git clone --depth 1 https://github.com/github/gitignore .cache/github-gitignore
	go run ./tools/synctemplates -src .cache/github-gitignore

install:
	go install ./cmd/gignore

clean:
	rm -rf bin dist completions manpages .cache
