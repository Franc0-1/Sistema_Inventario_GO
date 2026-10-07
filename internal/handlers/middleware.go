package handlers

import (
	"log"
	"net/http"
	"strings"
	"time"
)

// WithMiddleware aplica, de afuera hacia adentro: recuperación de panics,
// log de peticiones a /api y CORS (solo si hay orígenes configurados).
func WithMiddleware(next http.Handler, corsOrigins []string) http.Handler {
	return withRecovery(withLogging(withCORS(next, corsOrigins)))
}

// statusRecorder captura el código HTTP para el log.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

// withLogging registra método, ruta, estado y duración. No registra cuerpos
// ni parámetros: no hay datos sensibles en el log.
func withLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		if strings.HasPrefix(r.URL.Path, "/api/") {
			log.Printf("%s %s -> %d (%s)", r.Method, r.URL.Path, rec.status, time.Since(start).Round(time.Millisecond))
		}
	})
}

// withRecovery convierte un panic en un 500 JSON en lugar de cortar la conexión.
func withRecovery(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if p := recover(); p != nil {
				log.Printf("panic en %s %s: %v", r.Method, r.URL.Path, p)
				writeError(w, http.StatusInternalServerError, CodeInternal, "error interno del servidor")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// withCORS autoriza solo los orígenes configurados (nunca "*"). Sin orígenes
// no agrega nada: el frontend se sirve desde este mismo servidor.
func withCORS(next http.Handler, origins []string) http.Handler {
	if len(origins) == 0 {
		return next
	}
	allowed := make(map[string]bool, len(origins))
	for _, o := range origins {
		allowed[o] = true
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Vary", "Origin")
		origin := r.Header.Get("Origin")
		if !allowed[origin] {
			next.ServeHTTP(w, r)
			return
		}
		w.Header().Set("Access-Control-Allow-Origin", origin)
		if r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != "" {
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
			w.Header().Set("Access-Control-Max-Age", "600")
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
