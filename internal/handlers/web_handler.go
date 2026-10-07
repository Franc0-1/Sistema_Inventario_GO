package handlers

import (
	"html/template"
	"net/http"
	"path/filepath"
)

// WebHandler sirve el frontend ya construido (templates/web) y /healthz.
type WebHandler struct {
	templates *template.Template
	webDir    string
}

func NewWebHandler(webDir string) (*WebHandler, error) {
	tpl, err := template.ParseGlob(filepath.Join(webDir, "*.html"))
	if err != nil {
		return nil, err
	}
	return &WebHandler{templates: tpl, webDir: webDir}, nil
}

func (h *WebHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /{$}", h.index)
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.Dir(filepath.Join(h.webDir, "static")))))
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"estado": "ok"})
	})
}

func (h *WebHandler) index(w http.ResponseWriter, _ *http.Request) {
	if err := h.templates.ExecuteTemplate(w, "index.html", nil); err != nil {
		http.Error(w, "error al renderizar", http.StatusInternalServerError)
	}
}
