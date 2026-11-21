package main

import (
	"embed"
	"io/fs"
	"net/http"
)

// Собранный фронтенд (frontend/dist) копируется в web/dist перед go build
// (см. Dockerfile). В git лежит placeholder — бинарь собирается всегда.

//go:embed all:web/dist
var webDist embed.FS

// staticHandler отдаёт SPA: index.html на корень, ассеты по путям.
// Роутер фронтенда hash-based (#/book) — server-side fallback не нужен.
func staticHandler() http.Handler {
	sub, err := fs.Sub(webDist, "web/dist")
	if err != nil {
		panic(err)
	}
	return http.FileServerFS(sub)
}
