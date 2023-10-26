clean:
	rm -f cmd/changelog-markdown.tmpl

install: clean
	go generate cmd/*
	go install


