package handlers

import (
	"net/http"
	"strings"
	"time"

	"inventario/internal/models"
	"inventario/internal/services"
)

// PrestamoHandler expone los préstamos en el día:
//
//	GET  /api/prestamos?abiertos=1&vencidos=1&equipo_id=&persona_id=&limite=
//	POST /api/prestamos                    {"equipo_id", "persona_id", "devolucion_prevista"?, "observacion"?}
//	GET  /api/prestamos/{id}
//	POST /api/prestamos/{id}/devolucion    {"nota"?}
//
// devolucion_prevista acepta RFC 3339 o AAAA-MM-DDTHH:MM (hora local, como
// un <input type="datetime-local">); sin ella vence hoy a la hora de salida.
// Las escrituras exigen X-Usuario-ID.
type PrestamoHandler struct {
	service services.PrestamoService
}

func NewPrestamoHandler(service services.PrestamoService) *PrestamoHandler {
	return &PrestamoHandler{service: service}
}

type PrestamoRequest struct {
	EquipoID           int    `json:"equipo_id"`
	PersonaID          int    `json:"persona_id"`
	DevolucionPrevista string `json:"devolucion_prevista"`
	Observacion        string `json:"observacion"`
}

type DevolucionRequest struct {
	Nota string `json:"nota"`
}

func (h *PrestamoHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.Handle("/api/prestamos", methods{http.MethodGet: h.listar, http.MethodPost: h.prestar})
	mux.Handle("/api/prestamos/{id}", methods{http.MethodGet: h.obtener})
	mux.Handle("/api/prestamos/{id}/devolucion", methods{http.MethodPost: h.devolver})
}

func parseFechaHora(v string) (time.Time, *requestError) {
	v = strings.TrimSpace(v)
	if v == "" {
		return time.Time{}, nil
	}
	if t, err := time.Parse(time.RFC3339, v); err == nil {
		return t, nil
	}
	if t, err := time.ParseInLocation("2006-01-02T15:04", v, time.Local); err == nil {
		return t, nil
	}
	return time.Time{}, badRequest(CodeInvalidData, "devolucion_prevista debe ser AAAA-MM-DDTHH:MM (%q)", v)
}

func (h *PrestamoHandler) listar(w http.ResponseWriter, r *http.Request) {
	q := consulta{r: r}
	f := models.FiltroPrestamos{EquipoID: q.id("equipo_id"), PersonaID: q.id("persona_id"),
		Abiertos: q.flag("abiertos"), Vencidos: q.flag("vencidos"), Limite: q.entero("limite")}
	if q.err != nil {
		writeRequestError(w, q.err)
		return
	}
	list, err := h.service.ListarPrestamos(r.Context(), f)
	responder(w, r, http.StatusOK, nonNil(list), err)
}

func (h *PrestamoHandler) obtener(w http.ResponseWriter, r *http.Request) {
	id, e := pathID(r)
	if e != nil {
		writeRequestError(w, e)
		return
	}
	p, err := h.service.ObtenerPrestamo(r.Context(), id)
	responder(w, r, http.StatusOK, p, err)
}

func (h *PrestamoHandler) prestar(w http.ResponseWriter, r *http.Request) {
	var req PrestamoRequest
	r, _, ok := escritura(w, r, &req, false)
	if !ok {
		return
	}
	prevista, e := parseFechaHora(req.DevolucionPrevista)
	if e != nil {
		writeRequestError(w, e)
		return
	}
	p, err := h.service.Prestar(r.Context(), models.Prestamo{EquipoID: req.EquipoID, PersonaID: req.PersonaID,
		DevolucionPrevista: prevista, Observacion: req.Observacion})
	responder(w, r, http.StatusCreated, p, err)
}

func (h *PrestamoHandler) devolver(w http.ResponseWriter, r *http.Request) {
	var req DevolucionRequest
	r, id, ok := escritura(w, r, &req, true)
	if !ok {
		return
	}
	p, err := h.service.Devolver(r.Context(), id, req.Nota)
	responder(w, r, http.StatusOK, p, err)
}
