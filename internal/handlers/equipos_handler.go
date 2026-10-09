package handlers

import (
	"net/http"
	"strconv"
	"strings"

	"inventario/internal/models"
	"inventario/internal/services"
)

// EquipoHandler expone equipos y componentes del modelo nuevo:
//
//	GET  /api/equipos?q=&tipo_id=&oficina_id=&persona_id=&estado=&incluir_bajas=1&pendientes=1
//	POST /api/equipos
//	GET  /api/equipos/{id}             detalle con sus componentes
//	PUT  /api/equipos/{id}             reemplazo completo; exige la "version" leída
//	GET  /api/equipos/{id}/historial
//
//	GET  /api/componentes?q=&tipo_id=&equipo_id=&oficina_id=&sueltos=1&incluir_bajas=1
//	POST /api/componentes · GET/PUT /api/componentes/{id} · GET /api/componentes/{id}/historial
//
// Las escrituras exigen X-Usuario-ID: el cambio queda en el historial. "nota"
// es la observación del historial (p. ej. el motivo de un traslado).
type EquipoHandler struct {
	service services.EquipoService
}

func NewEquipoHandler(service services.EquipoService) *EquipoHandler {
	return &EquipoHandler{service: service}
}

type EquipoRequest struct {
	NumeroInventario string            `json:"numero_inventario"`
	TipoID           int               `json:"tipo_id"`
	Marca            string            `json:"marca"`
	Modelo           string            `json:"modelo"`
	NumeroSerie      string            `json:"numero_serie"`
	Estado           models.ItemStatus `json:"estado"`
	OficinaID        int               `json:"oficina_id"` // 0 con persona = la oficina de la persona
	PersonaID        int               `json:"persona_id"`
	Observacion      string            `json:"observacion"`
	MotivoBaja       string            `json:"motivo_baja"`
	Nota             string            `json:"nota"`
	Version          string            `json:"version"`
}

// ComponenteRequest: con equipo_id, oficina_id se ignora (el componente está
// en la oficina de su equipo).
type ComponenteRequest struct {
	NumeroInventario string            `json:"numero_inventario"`
	TipoID           int               `json:"tipo_id"`
	Marca            string            `json:"marca"`
	Modelo           string            `json:"modelo"`
	NumeroSerie      string            `json:"numero_serie"`
	Estado           models.ItemStatus `json:"estado"`
	EquipoID         int               `json:"equipo_id"`
	OficinaID        int               `json:"oficina_id"`
	Observacion      string            `json:"observacion"`
	MotivoBaja       string            `json:"motivo_baja"`
	Nota             string            `json:"nota"`
	Version          string            `json:"version"`
}

func (req EquipoRequest) equipo(id int) models.Equipo {
	return models.Equipo{ID: id, NumeroInventario: req.NumeroInventario, TipoID: req.TipoID, Marca: req.Marca,
		Modelo: req.Modelo, NumeroSerie: req.NumeroSerie, Estado: req.Estado, OficinaID: req.OficinaID,
		PersonaID: req.PersonaID, Observacion: req.Observacion, MotivoBaja: req.MotivoBaja, Version: req.Version}
}

func (req ComponenteRequest) componente(id int) models.Componente {
	return models.Componente{ID: id, NumeroInventario: req.NumeroInventario, TipoID: req.TipoID, Marca: req.Marca,
		Modelo: req.Modelo, NumeroSerie: req.NumeroSerie, Estado: req.Estado, EquipoID: req.EquipoID,
		OficinaID: req.OficinaID, Observacion: req.Observacion, MotivoBaja: req.MotivoBaja, Version: req.Version}
}

func (h *EquipoHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.Handle("/api/equipos", methods{http.MethodGet: h.listarEquipos, http.MethodPost: h.crearEquipo})
	mux.Handle("/api/equipos/{id}", methods{http.MethodGet: h.obtenerEquipo, http.MethodPut: h.actualizarEquipo})
	mux.Handle("/api/equipos/{id}/historial", methods{http.MethodGet: h.historialEquipo})
	mux.Handle("/api/componentes", methods{http.MethodGet: h.listarComponentes, http.MethodPost: h.crearComponente})
	mux.Handle("/api/componentes/{id}", methods{http.MethodGet: h.obtenerComponente, http.MethodPut: h.actualizarComponente})
	mux.Handle("/api/componentes/{id}/historial", methods{http.MethodGet: h.historialComponente})
}

// consulta lee los parámetros enteros y booleanos de un listado; el primer
// error queda guardado y se responde una sola vez.
type consulta struct {
	r   *http.Request
	err *requestError
}

func (c *consulta) id(name string) int {
	raw := c.r.URL.Query().Get(name)
	if raw == "" || c.err != nil {
		return 0
	}
	v, err := strconv.Atoi(raw)
	if err != nil || v <= 0 {
		c.err = badRequest(CodeInvalidQuery, "%s debe ser un ID (%q)", name, raw)
	}
	return v
}

func (c *consulta) flag(name string) bool {
	switch strings.ToLower(c.r.URL.Query().Get(name)) {
	case "", "0", "false":
		return false
	case "1", "true":
		return true
	}
	if c.err == nil {
		c.err = badRequest(CodeInvalidQuery, "%s debe ser 1 o 0", name)
	}
	return false
}

func (h *EquipoHandler) listarEquipos(w http.ResponseWriter, r *http.Request) {
	q := consulta{r: r}
	f := models.FiltroEquipos{
		Texto: r.URL.Query().Get("q"), TipoID: q.id("tipo_id"), OficinaID: q.id("oficina_id"),
		PersonaID: q.id("persona_id"), Estado: models.ItemStatus(r.URL.Query().Get("estado")),
		IncluirBajas: q.flag("incluir_bajas"), Pendientes: q.flag("pendientes"),
	}
	if q.err != nil {
		writeRequestError(w, q.err)
		return
	}
	list, err := h.service.ListarEquipos(r.Context(), f)
	responder(w, r, http.StatusOK, nonNil(list), err)
}

func (h *EquipoHandler) obtenerEquipo(w http.ResponseWriter, r *http.Request) {
	id, e := pathID(r)
	if e != nil {
		writeRequestError(w, e)
		return
	}
	eq, err := h.service.ObtenerEquipo(r.Context(), id)
	responder(w, r, http.StatusOK, eq, err)
}

func (h *EquipoHandler) crearEquipo(w http.ResponseWriter, r *http.Request) {
	var req EquipoRequest
	r, _, ok := escritura(w, r, &req, false)
	if !ok {
		return
	}
	eq, err := h.service.CrearEquipo(r.Context(), req.equipo(0), req.Nota)
	responder(w, r, http.StatusCreated, eq, err)
}

func (h *EquipoHandler) actualizarEquipo(w http.ResponseWriter, r *http.Request) {
	var req EquipoRequest
	r, id, ok := escritura(w, r, &req, true)
	if !ok {
		return
	}
	eq, err := h.service.ActualizarEquipo(r.Context(), req.equipo(id), req.Nota)
	responder(w, r, http.StatusOK, eq, err)
}

func (h *EquipoHandler) historialEquipo(w http.ResponseWriter, r *http.Request) {
	id, e := pathID(r)
	if e != nil {
		writeRequestError(w, e)
		return
	}
	list, err := h.service.HistorialEquipo(r.Context(), id)
	responder(w, r, http.StatusOK, nonNil(list), err)
}

func (h *EquipoHandler) listarComponentes(w http.ResponseWriter, r *http.Request) {
	q := consulta{r: r}
	f := models.FiltroComponentes{
		Texto: r.URL.Query().Get("q"), TipoID: q.id("tipo_id"), EquipoID: q.id("equipo_id"),
		OficinaID: q.id("oficina_id"), Sueltos: q.flag("sueltos"), IncluirBajas: q.flag("incluir_bajas"),
	}
	if q.err != nil {
		writeRequestError(w, q.err)
		return
	}
	list, err := h.service.ListarComponentes(r.Context(), f)
	responder(w, r, http.StatusOK, nonNil(list), err)
}

func (h *EquipoHandler) obtenerComponente(w http.ResponseWriter, r *http.Request) {
	id, e := pathID(r)
	if e != nil {
		writeRequestError(w, e)
		return
	}
	c, err := h.service.ObtenerComponente(r.Context(), id)
	responder(w, r, http.StatusOK, c, err)
}

func (h *EquipoHandler) crearComponente(w http.ResponseWriter, r *http.Request) {
	var req ComponenteRequest
	r, _, ok := escritura(w, r, &req, false)
	if !ok {
		return
	}
	c, err := h.service.CrearComponente(r.Context(), req.componente(0), req.Nota)
	responder(w, r, http.StatusCreated, c, err)
}

func (h *EquipoHandler) actualizarComponente(w http.ResponseWriter, r *http.Request) {
	var req ComponenteRequest
	r, id, ok := escritura(w, r, &req, true)
	if !ok {
		return
	}
	c, err := h.service.ActualizarComponente(r.Context(), req.componente(id), req.Nota)
	responder(w, r, http.StatusOK, c, err)
}

func (h *EquipoHandler) historialComponente(w http.ResponseWriter, r *http.Request) {
	id, e := pathID(r)
	if e != nil {
		writeRequestError(w, e)
		return
	}
	list, err := h.service.HistorialComponente(r.Context(), id)
	responder(w, r, http.StatusOK, nonNil(list), err)
}
