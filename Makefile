# Pinned tool versions (bump deliberately; `go run pkg@version` is verified
# against the Go checksum database).
GOVULNCHECK_VERSION ?= v1.9.0

VERSION ?= $(shell git describe --tags --always --dirty)
DIST_DIR ?= dist

.PHONY: clean generate install build test vulncheck dist

clean:
	rm -f cmd/changelog-markdown.tmpl
	rm -rf $(DIST_DIR)

generate:
	go generate cmd/generate.go

install: clean generate
	go install

build: clean generate
	go build -v ./...

test: clean generate
	go test -v ./...

vulncheck: generate
	go run golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION) ./...

# Cross-compiled, packaged release assets in $(DIST_DIR).
dist: clean generate
	./scripts/build-release.sh "$(VERSION)" "$(DIST_DIR)"
