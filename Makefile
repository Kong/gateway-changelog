clean:
	rm -f cmd/changelog-markdown.tmpl

generate:
	go generate cmd/generate.go

install: clean generate
	go install

build: clean generate
	go build -v ./...

test: clean generate
	go test -v ./...
