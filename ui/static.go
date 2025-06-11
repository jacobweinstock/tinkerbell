package ui

import (
	"embed"
	"net/http"
)

//go:embed static/*
var staticFiles embed.FS

func RegisterStatic(mux *http.ServeMux) {
	mux.Handle("/static/", http.FileServer(http.FS(staticFiles)))
}
