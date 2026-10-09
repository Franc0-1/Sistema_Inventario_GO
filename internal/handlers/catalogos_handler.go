package handlers

import (
	"net/http"
	"strconv"
	"strings"

	"inventario/internal/models"
	"inventario/internal/services"
)

// CatalogoHandler expone oficinas, puestos, usuarios, personas y tipos:
//
//	GET  /api/{catalogo}?activos=1   listado (activos=1: solo los activos)
//	POST /api/{catalogo}             alta
//	GET  /api/{catalogo}/{id}        detalle
//	PUT  /api/{catalogo}/{id}        reemplazo completo; exige la "version" leída
//
// No hay DELETE: lo que no se usa se desactiva con "activo": false. El
// usuario que hace el cambio va en el encabezado X-Usuario-ID (opcional por
// ahora).
type CatalogoHandler struct {
	service services.CatalogoService
}

func NewCatalogoHandler(service services.CatalogoService) *CatalogoHandler {
	return &CatalogoHandler{service: service}
}

// CatalogoRequest es el cuerpo de alta y edición de oficinas, puestos y
// usuarios. Activo omitido = true.
type CatalogoRequest struct {
	Nombre  string `json:"nombre"`
	Activo  *bool  `json:"activo"`
	Version string `json:"version"`
}

type PersonaRequest struct {
	Nombre    string `json:"nombre"`
	Apellido  string `json:"apellido"`
	PuestoID  int    `json:"puesto_id"`
	OficinaID int    `json:"oficina_id"`
	Activo    *bool  `json:"activo"`
	Version   string `json:"version"`
}

type TipoRequest struct {
	Nombre    string       `json:"nombre"`
	Clase     models.Clase `json:"clase"`
	Prestable bool         `json:"prestable"`
	Activo    *bool        `json:"activo"`
	Version   string       `json:"version"`
}

func activo(v *bool) bool { return v == nil || *v }

func (h *CatalogoHandler) RegisterRoutes(mux *http.ServeMux) {
	for ruta, t := range map[string]models.TablaCatalogo{
		"oficinas": models.CatalogoOficinas,
		"puestos":  models.CatalogoPuestos,
		"usuarios": models.CatalogoUsuarios,
	} {
		mux.Handle("/api/"+ruta, methods{http.MethodGet: h.listarCatalogo(t), http.MethodPost: h.crearCatalogo(t)})
		mux.Handle("/api/"+ruta+"/{id}", methods{http.MethodGet: h.obtenerCatalogo(t), http.MethodPut: h.actualizarCatalogo(t)})
	}
	mux.Handle("/api/personas", methods{http.MethodGet: h.listarPersonas, http.MethodPost: h.crearPersona})
	mux.Handle("/api/personas/{id}", methods{http.MethodGet: h.obtenerPersona, http.MethodPut: h.actualizarPersona})
	mux.Handle("/api/tipos", methods{http.MethodGet: h.listarTipos, http.MethodPost: h.crearTipo})
	mux.Handle("/api/tipos/{id}", methods{http.MethodGet: h.obtenerTipo, http.MethodPut: h.actualizarTipo})
}

// conUsuario agrega al contexto el usuario de X-Usuario-ID, si viene.
func conUsuario(r *http.Request) (*http.Request, *requestError) {
	raw := strings.TrimSpace(r.Header.Get("X-Usuario-ID"))
	if raw == "" {
		return r, nil
	}
	id, err := strconv.Atoi(raw)
	if err != nil || id <= 0 {
		return r, badRequest(CodeInvalidUser, "X-Usuario-ID debe ser un ID de usuario (%q)", raw)
	}
	return r.WithContext(services.ConUsuario(r.Context(), id)), nil
}

func soloActivos(r *http.Request) (bool, *requestError) {
	switch strings.ToLower(r.URL.Query().Get("activos")) {
	case "", "0", "false":
		return false, nil
	case "1", "true":
		return true, nil
	}
	return false, badRequest(CodeInvalidQuery, "activos debe ser 1 o 0")
}

// escritura decodifica el cuerpo y el usuario; para PUT también el ID.
func escritura(w http.ResponseWriter, r *http.Request, dst any, conID bool) (*http.Request, int, bool) {
	id := 0
	if conID {
		var e *requestError
		if id, e = pathID(r); e != nil {
			writeRequestError(w, e)
			return r, 0, false
		}
	}
	r, e := conUsuario(r)
	if e == nil {
		e = decodeJSON(w, r, dst)
	}
	if e != nil {
		writeRequestError(w, e)
		return r, 0, false
	}
	return r, id, true
}

// responder escribe el resultado del servicio o su error.
func responder[T any](w http.ResponseWriter, r *http.Request, status int, v T, err error) {
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	writeData(w, status, v)
}

// ---- Oficinas, puestos y usuarios ----

func (h *CatalogoHandler) listarCatalogo(t models.TablaCatalogo) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		activos, e := soloActivos(r)
		if e != nil {
			writeRequestError(w, e)
			return
		}
		list, err := h.service.ListarCatalogo(r.Context(), t, activos)
		responder(w, r, http.StatusOK, nonNil(list), err)
	}
}

func (h *CatalogoHandler) obtenerCatalogo(t models.TablaCatalogo) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, e := pathID(r)
		if e != nil {
			writeRequestError(w, e)
			return
		}
		c, err := h.service.ObtenerCatalogo(r.Context(), t, id)
		responder(w, r, http.StatusOK, c, err)
	}
}

func (h *CatalogoHandler) crearCatalogo(t models.TablaCatalogo) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req CatalogoRequest
		r, _, ok := escritura(w, r, &req, false)
		if !ok {
			return
		}
		c, err := h.service.CrearCatalogo(r.Context(), t, models.Catalogo{Nombre: req.Nombre, Activo: activo(req.Activo)})
		responder(w, r, http.StatusCreated, c, err)
	}
}

func (h *CatalogoHandler) actualizarCatalogo(t models.TablaCatalogo) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req CatalogoRequest
		r, id, ok := escritura(w, r, &req, true)
		if !ok {
			return
		}
		c, err := h.service.ActualizarCatalogo(r.Context(), t,
			models.Catalogo{ID: id, Nombre: req.Nombre, Activo: activo(req.Activo), Version: req.Version})
		responder(w, r, http.StatusOK, c, err)
	}
}

// ---- Personas ----

// GET /api/personas?oficina_id=&q=&activos=
func (h *CatalogoHandler) listarPersonas(w http.ResponseWriter, r *http.Request) {
	activos, e := soloActivos(r)
	if e != nil {
		writeRequestError(w, e)
		return
	}
	f := models.FiltroPersonas{Texto: r.URL.Query().Get("q"), SoloActivas: activos}
	if raw := r.URL.Query().Get("oficina_id"); raw != "" {
		id, err := strconv.Atoi(raw)
		if err != nil || id <= 0 {
			writeRequestError(w, badRequest(CodeInvalidQuery, "oficina_id debe ser un ID (%q)", raw))
			return
		}
		f.OficinaID = id
	}
	list, err := h.service.ListarPersonas(r.Context(), f)
	responder(w, r, http.StatusOK, nonNil(list), err)
}

func (h *CatalogoHandler) obtenerPersona(w http.ResponseWriter, r *http.Request) {
	id, e := pathID(r)
	if e != nil {
		writeRequestError(w, e)
		return
	}
	p, err := h.service.ObtenerPersona(r.Context(), id)
	responder(w, r, http.StatusOK, p, err)
}

func (req PersonaRequest) persona(id int) models.Persona {
	return models.Persona{ID: id, Nombre: req.Nombre, Apellido: req.Apellido, PuestoID: req.PuestoID,
		OficinaID: req.OficinaID, Activo: activo(req.Activo), Version: req.Version}
}

func (h *CatalogoHandler) crearPersona(w http.ResponseWriter, r *http.Request) {
	var req PersonaRequest
	r, _, ok := escritura(w, r, &req, false)
	if !ok {
		return
	}
	p, err := h.service.CrearPersona(r.Context(), req.persona(0))
	responder(w, r, http.StatusCreated, p, err)
}

func (h *CatalogoHandler) actualizarPersona(w http.ResponseWriter, r *http.Request) {
	var req PersonaRequest
	r, id, ok := escritura(w, r, &req, true)
	if !ok {
		return
	}
	p, err := h.service.ActualizarPersona(r.Context(), req.persona(id))
	responder(w, r, http.StatusOK, p, err)
}

// ---- Tipos ----

// GET /api/tipos?clase=EQUIPO&activos=1
func (h *CatalogoHandler) listarTipos(w http.ResponseWriter, r *http.Request) {
	activos, e := soloActivos(r)
	if e != nil {
		writeRequestError(w, e)
		return
	}
	list, err := h.service.ListarTipos(r.Context(), r.URL.Query().Get("clase"), activos)
	responder(w, r, http.StatusOK, nonNil(list), err)
}

func (h *CatalogoHandler) obtenerTipo(w http.ResponseWriter, r *http.Request) {
	id, e := pathID(r)
	if e != nil {
		writeRequestError(w, e)
		return
	}
	t, err := h.service.ObtenerTipo(r.Context(), id)
	responder(w, r, http.StatusOK, t, err)
}

func (req TipoRequest) tipo(id int) models.Tipo {
	return models.Tipo{ID: id, Nombre: req.Nombre, Clase: req.Clase, Prestable: req.Prestable,
		Activo: activo(req.Activo), Version: req.Version}
}

func (h *CatalogoHandler) crearTipo(w http.ResponseWriter, r *http.Request) {
	var req TipoRequest
	r, _, ok := escritura(w, r, &req, false)
	if !ok {
		return
	}
	t, err := h.service.CrearTipo(r.Context(), req.tipo(0))
	responder(w, r, http.StatusCreated, t, err)
}

func (h *CatalogoHandler) actualizarTipo(w http.ResponseWriter, r *http.Request) {
	var req TipoRequest
	r, id, ok := escritura(w, r, &req, true)
	if !ok {
		return
	}
	t, err := h.service.ActualizarTipo(r.Context(), req.tipo(id))
	responder(w, r, http.StatusOK, t, err)
}
