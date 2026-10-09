package handlers

import (
	"net/http"
	"strconv"
	"time"

	"inventario/internal/models"
	"inventario/internal/services"
)

// InsumoHandler expone los insumos y su stock:
//
//	GET  /api/insumos?q=&tipo_id=&activos=1&bajo_stock=1
//	POST /api/insumos                       alta; "stock" es el stock inicial
//	GET  /api/insumos/{id}
//	PUT  /api/insumos/{id}                  no cambia el stock; exige "version"
//	GET  /api/insumos/{id}/movimientos?limite=
//	POST /api/insumos/{id}/movimientos      entrada, salida o ajuste (cantidad = stock contado)
//	GET  /api/movimientos-insumo?persona_id=&oficina_id=&tipo=&desde=AAAA-MM-DD&hasta=AAAA-MM-DD&limite=
//
// Las escrituras exigen X-Usuario-ID.
type InsumoHandler struct {
	service services.InsumoService
}

func NewInsumoHandler(service services.InsumoService) *InsumoHandler {
	return &InsumoHandler{service: service}
}

type InsumoRequest struct {
	TipoID      int    `json:"tipo_id"`
	Marca       string `json:"marca"`
	Modelo      string `json:"modelo"`
	Stock       *int   `json:"stock"` // solo en el alta
	StockMinimo int    `json:"stock_minimo"`
	Observacion string `json:"observacion"`
	Activo      *bool  `json:"activo"`
	Version     string `json:"version"`
}

type MovimientoInsumoRequest struct {
	Tipo        models.TipoMovimientoInsumo `json:"tipo"`
	Cantidad    int                         `json:"cantidad"`
	PersonaID   int                         `json:"persona_id"`
	OficinaID   int                         `json:"oficina_id"`
	Observacion string                      `json:"observacion"`
}

// MovimientoInsumoResponse devuelve el movimiento y el insumo con el stock nuevo.
type MovimientoInsumoResponse struct {
	Movimiento models.MovimientoInsumo `json:"movimiento"`
	Insumo     models.Insumo           `json:"insumo"`
}

func (req InsumoRequest) insumo(id int) models.Insumo {
	i := models.Insumo{ID: id, TipoID: req.TipoID, Marca: req.Marca, Modelo: req.Modelo, StockMinimo: req.StockMinimo,
		Observacion: req.Observacion, Activo: activo(req.Activo), Version: req.Version}
	if req.Stock != nil {
		i.Stock = *req.Stock
	}
	return i
}

func (h *InsumoHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.Handle("/api/insumos", methods{http.MethodGet: h.listar, http.MethodPost: h.crear})
	mux.Handle("/api/insumos/{id}", methods{http.MethodGet: h.obtener, http.MethodPut: h.actualizar})
	mux.Handle("/api/insumos/{id}/movimientos", methods{http.MethodGet: h.movimientosDelInsumo, http.MethodPost: h.registrarMovimiento})
	mux.Handle("/api/movimientos-insumo", methods{http.MethodGet: h.movimientos})
}

func (c *consulta) entero(name string) int {
	raw := c.r.URL.Query().Get(name)
	if raw == "" || c.err != nil {
		return 0
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		c.err = badRequest(CodeInvalidQuery, "%s debe ser un número (%q)", name, raw)
	}
	return v
}

// fecha lee un día AAAA-MM-DD en la hora local del servidor.
func (c *consulta) fecha(name string) time.Time {
	raw := c.r.URL.Query().Get(name)
	if raw == "" || c.err != nil {
		return time.Time{}
	}
	t, err := time.ParseInLocation(time.DateOnly, raw, time.Local)
	if err != nil {
		c.err = badRequest(CodeInvalidQuery, "%s debe ser una fecha AAAA-MM-DD (%q)", name, raw)
	}
	return t
}

func (h *InsumoHandler) listar(w http.ResponseWriter, r *http.Request) {
	q := consulta{r: r}
	f := models.FiltroInsumos{Texto: r.URL.Query().Get("q"), TipoID: q.id("tipo_id"),
		SoloActivos: q.flag("activos"), BajoStock: q.flag("bajo_stock")}
	if q.err != nil {
		writeRequestError(w, q.err)
		return
	}
	list, err := h.service.ListarInsumos(r.Context(), f)
	responder(w, r, http.StatusOK, nonNil(list), err)
}

func (h *InsumoHandler) obtener(w http.ResponseWriter, r *http.Request) {
	id, e := pathID(r)
	if e != nil {
		writeRequestError(w, e)
		return
	}
	i, err := h.service.ObtenerInsumo(r.Context(), id)
	responder(w, r, http.StatusOK, i, err)
}

func (h *InsumoHandler) crear(w http.ResponseWriter, r *http.Request) {
	var req InsumoRequest
	r, _, ok := escritura(w, r, &req, false)
	if !ok {
		return
	}
	i, err := h.service.CrearInsumo(r.Context(), req.insumo(0))
	responder(w, r, http.StatusCreated, i, err)
}

func (h *InsumoHandler) actualizar(w http.ResponseWriter, r *http.Request) {
	var req InsumoRequest
	r, id, ok := escritura(w, r, &req, true)
	if !ok {
		return
	}
	if req.Stock != nil {
		writeRequestError(w, badRequest(CodeInvalidData, "el stock no se edita: registrá una entrada, una salida o un ajuste"))
		return
	}
	i, err := h.service.ActualizarInsumo(r.Context(), req.insumo(id))
	responder(w, r, http.StatusOK, i, err)
}

func (h *InsumoHandler) registrarMovimiento(w http.ResponseWriter, r *http.Request) {
	var req MovimientoInsumoRequest
	r, id, ok := escritura(w, r, &req, true)
	if !ok {
		return
	}
	m, i, err := h.service.RegistrarMovimiento(r.Context(), models.MovimientoInsumo{InsumoID: id, Tipo: req.Tipo,
		Cantidad: req.Cantidad, PersonaID: req.PersonaID, OficinaID: req.OficinaID, Observacion: req.Observacion})
	responder(w, r, http.StatusCreated, MovimientoInsumoResponse{Movimiento: m, Insumo: i}, err)
}

func (h *InsumoHandler) movimientosDelInsumo(w http.ResponseWriter, r *http.Request) {
	id, e := pathID(r)
	if e != nil {
		writeRequestError(w, e)
		return
	}
	q := consulta{r: r}
	f := models.FiltroMovimientosInsumo{InsumoID: id, Limite: q.entero("limite")}
	if q.err != nil {
		writeRequestError(w, q.err)
		return
	}
	if _, err := h.service.ObtenerInsumo(r.Context(), id); err != nil {
		writeServiceError(w, r, err)
		return
	}
	list, err := h.service.ListarMovimientos(r.Context(), f)
	responder(w, r, http.StatusOK, nonNil(list), err)
}

func (h *InsumoHandler) movimientos(w http.ResponseWriter, r *http.Request) {
	q := consulta{r: r}
	f := models.FiltroMovimientosInsumo{
		InsumoID: q.id("insumo_id"), PersonaID: q.id("persona_id"), OficinaID: q.id("oficina_id"),
		Tipo: models.TipoMovimientoInsumo(r.URL.Query().Get("tipo")), Desde: q.fecha("desde"), Hasta: q.fecha("hasta"),
		Limite: q.entero("limite"),
	}
	if q.err != nil {
		writeRequestError(w, q.err)
		return
	}
	list, err := h.service.ListarMovimientos(r.Context(), f)
	responder(w, r, http.StatusOK, nonNil(list), err)
}
