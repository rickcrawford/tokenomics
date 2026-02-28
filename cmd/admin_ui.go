package cmd

import (
	"embed"
	"mime"
	"net/http"
	"path"
	"strings"

	"github.com/go-chi/chi/v5"
)

//go:embed web/admin/index.html web/admin/assets/*
var adminUI embed.FS

func registerAdminUIRoutes(r chi.Router, middlewares ...func(http.Handler) http.Handler) {
	r.Group(func(gr chi.Router) {
		for _, mw := range middlewares {
			gr.Use(mw)
		}
		gr.Get("/", serveAdminIndex)
		gr.Get("/admin", serveAdminIndex)
		gr.Get("/admin/*", serveAdminIndex)
		gr.Get("/assets/*", serveAdminAsset)
	})
}

func serveAdminIndex(w http.ResponseWriter, r *http.Request) {
	data, err := adminUI.ReadFile("web/admin/index.html")
	if err != nil {
		http.Error(w, "admin ui unavailable", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(data)
}

func serveAdminAsset(w http.ResponseWriter, r *http.Request) {
	assetPath := strings.TrimPrefix(r.URL.Path, "/assets/")
	if assetPath == "" || strings.Contains(assetPath, "..") {
		http.NotFound(w, r)
		return
	}
	clean := path.Clean(assetPath)
	data, err := adminUI.ReadFile("web/admin/assets/" + clean)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if ct := mime.TypeByExtension(path.Ext(clean)); ct != "" {
		w.Header().Set("Content-Type", ct)
	}
	_, _ = w.Write(data)
}
