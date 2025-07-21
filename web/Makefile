TEMPL_VERSION := v0.2.793

.PHONY: install-templ
install-templ:
	go install github.com/a-h/templ/cmd/templ@$(TEMPL_VERSION)

.PHONY: generate
generate:
	go run github.com/a-h/templ/cmd/templ@latest generate

.PHONY: build
build: generate
	go build -o bin/server ./cmd/server

.PHONY: dev
dev: generate
	go run ./cmd/server

.PHONY: clean
clean:
	rm -rf bin/
	rm -f *_templ.go

.PHONY: watch
watch:
	go run github.com/a-h/templ/cmd/templ@latest generate --watch --proxy="http://localhost:8080" --cmd="go run ./cmd/server"
