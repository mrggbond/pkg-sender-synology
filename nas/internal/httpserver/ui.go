package httpserver

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed web/*
var uiAssets embed.FS

func newUIHandler() http.Handler {
	sub, err := fs.Sub(uiAssets, "web")
	if err != nil {
		panic(err)
	}
	return http.FileServer(http.FS(sub))
}
