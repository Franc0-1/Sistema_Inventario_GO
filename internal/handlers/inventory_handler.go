// Package handlers es la capa HTTP: lee y valida el formato de la petición,
// llama a InventoryService y responde JSON. No contiene reglas de negocio ni
// accede al Excel.
package handlers

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"inventario/internal/models"
	"inventario/internal/repository"
	"inventario/internal/services"
)

// InventoryHandler expone InventoryService como API REST. Guarda cada área del
// servicio por separado: cada endpoint solo puede usar la parte que le corresponde.
type InventoryHandler struct {
	items     services.ItemService
	stock     services.StockService
	equipment services.EquipmentService
	reports   services.ReportService
	sheets    services.SpreadsheetService
	checker   services.HealthService
}

func NewInventoryHandler(service services.InventoryService) *InventoryHandler {
	return &InventoryHandler{items: service, stock: service, equipment: service, reports: service, sheets: service, checker: service}
}

// RegisterRoutes registra las rutas de la API. Cada ruta despacha por método
// para responder 405 en JSON (con Allow) en lugar del texto plano de ServeMux.
func (h *InventoryHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.Handle("/api/equipment", methods{http.MethodGet: h.equipments})
	mux.Handle("/api/inventory/{id}/equipment", methods{http.MethodPut: h.setEquipment})
	mux.Handle("/api/inventory/summary", methods{http.MethodGet: h.summary})
	mux.Handle("/api/inventory/reports", methods{http.MethodGet: h.report})
	mux.Handle("/api/inventory", methods{
		http.MethodGet:  h.list,
		http.MethodPost: h.create,
	})
	// La búsqueda general es el mismo listado: acepta q y todos los filtros,
	// el orden y la paginación, y responde igual. Literal: tiene prioridad sobre {id}.
	mux.Handle("/api/inventory/search", methods{
		http.MethodGet: h.list,
	})
	mux.Handle("/api/inventory/export", methods{
		http.MethodGet: h.exportInventory,
	})
	mux.Handle("/api/inventory/import", methods{
		http.MethodPost: h.importInventory,
	})
	mux.Handle("/api/inventory/{id}", methods{
		http.MethodGet:    h.get,
		http.MethodPut:    h.update,
		http.MethodDelete: h.delete,
	})
	mux.Handle("/api/inventory/{id}/stock", methods{
		http.MethodPatch: h.updateStock,
	})
	mux.Handle("/api/inventory/{id}/movements", methods{
		http.MethodGet: h.itemMovements,
	})
	mux.Handle("/api/movements", methods{
		http.MethodGet: h.movements,
	})
	mux.Handle("/api/health", methods{
		http.MethodGet: h.health,
	})
	mux.HandleFunc("/api/", func(w http.ResponseWriter, _ *http.Request) {
		writeError(w, http.StatusNotFound, CodeNotFound, "ruta inexistente")
	})
}

// GET /api/inventory/export
func (h *InventoryHandler) exportInventory(w http.ResponseWriter, r *http.Request) {
	data, err := h.sheets.ExportInventory(r.Context())
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	filename := fmt.Sprintf("inventario_%s.xlsx", time.Now().Format("2006-01-02_15-04-05"))
	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write(data); err != nil {
		log.Printf("no se pudo escribir el Excel exportado: %v", err)
	}
}

// POST /api/inventory/import (multipart/form-data, campo "file")
func (h *InventoryHandler) importInventory(w http.ResponseWriter, r *http.Request) {
	const maxImportBytes = 20 << 20
	r.Body = http.MaxBytesReader(w, r.Body, maxImportBytes)
	if err := r.ParseMultipartForm(maxImportBytes); err != nil {
		writeError(w, http.StatusBadRequest, CodeInvalidRequest, "archivo de importación inválido")
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, CodeInvalidRequest, "se esperaba un archivo .xlsx en el campo file")
		return
	}
	defer file.Close()
	if strings.ToLower(filepath.Ext(header.Filename)) != ".xlsx" {
		writeError(w, http.StatusBadRequest, CodeInvalidRequest, "el archivo debe tener extensión .xlsx")
		return
	}
	imported, err := h.sheets.ImportInventory(r.Context(), file)
	if err != nil {
		writeImportError(w, err)
		return
	}
	writeData(w, http.StatusOK, map[string]int{"imported": imported})
}

func writeImportError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, repository.ErrSheetMissing),
		errors.Is(err, repository.ErrInvalidCell),
		errors.Is(err, repository.ErrInvalidItem):
		writeError(w, http.StatusBadRequest, CodeInvalidRequest, err.Error())
	case errors.Is(err, repository.ErrInvalidWorkbook):
		writeError(w, http.StatusBadRequest, CodeInvalidRequest, "archivo Excel inválido")
	default:
		writeError(w, http.StatusInternalServerError, CodeInternal, "error interno del servidor")
	}
}

// GET /api/inventory?brand=Dell&location=Sistemas&sort=brand&order=asc&page=1&pageSize=20
//
// Responde {"data": {"items": [...], "pagination": {...}}}. Sin page ni
// pageSize devuelve todos los resultados en una sola página.
func (h *InventoryHandler) list(w http.ResponseWriter, r *http.Request) {
	q, reqErr := parseItemQuery(r.URL.Query())
	if reqErr != nil {
		writeRequestError(w, reqErr)
		return
	}
	page, err := h.items.Query(r.Context(), q)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	page.Items = nonNil(page.Items)
	writeData(w, http.StatusOK, page)
}

// GET /api/inventory/{id}
func (h *InventoryHandler) get(w http.ResponseWriter, r *http.Request) {
	id, reqErr := pathID(r)
	if reqErr != nil {
		writeRequestError(w, reqErr)
		return
	}
	item, err := h.items.GetByID(r.Context(), id)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, item)
}

// POST /api/inventory
func (h *InventoryHandler) create(w http.ResponseWriter, r *http.Request) {
	var req CreateItemRequest
	if reqErr := decodeJSON(w, r, &req); reqErr != nil {
		writeRequestError(w, reqErr)
		return
	}
	item, err := h.items.Create(r.Context(), req.toItem())
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	writeData(w, http.StatusCreated, item)
}

// PUT /api/inventory/{id}. El ID sale solo de la URL.
func (h *InventoryHandler) update(w http.ResponseWriter, r *http.Request) {
	id, reqErr := pathID(r)
	if reqErr != nil {
		writeRequestError(w, reqErr)
		return
	}
	var req UpdateItemRequest
	if reqErr := decodeJSON(w, r, &req); reqErr != nil {
		writeRequestError(w, reqErr)
		return
	}
	if req.EquipmentID != nil {
		writeError(w, http.StatusBadRequest, CodeInvalidRequest, "use el endpoint de asociacion para cambiar equipo_id")
		return
	}
	item := req.toItem()
	item.ID = id
	updated, err := h.items.Update(r.Context(), id, item)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, updated)
}

// DELETE /api/inventory/{id}
func (h *InventoryHandler) delete(w http.ResponseWriter, r *http.Request) {
	id, reqErr := pathID(r)
	if reqErr != nil {
		writeRequestError(w, reqErr)
		return
	}
	if err := h.items.Delete(r.Context(), id); err != nil {
		writeServiceError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// PATCH /api/inventory/{id}/stock  {"cantidad": 10} o {"tipo": "stock_out", "cantidad": 3} (ver UpdateStockRequest)
func (h *InventoryHandler) updateStock(w http.ResponseWriter, r *http.Request) {
	id, reqErr := pathID(r)
	if reqErr != nil {
		writeRequestError(w, reqErr)
		return
	}
	var req UpdateStockRequest
	if reqErr := decodeJSON(w, r, &req); reqErr != nil {
		writeRequestError(w, reqErr)
		return
	}
	op, reqErr := req.toOperation()
	if reqErr != nil {
		writeRequestError(w, reqErr)
		return
	}
	var (
		item models.Item
		err  error
	)
	if req.Type == "" && req.DestinationLocation == "" && req.Notes == "" {
		item, err = h.stock.UpdateStock(r.Context(), id, op.Quantity) // cuerpo original {"cantidad": n}
	} else {
		item, err = h.stock.ApplyMovement(r.Context(), id, op)
	}
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, item)
}

// GET /api/movements
func (h *InventoryHandler) movements(w http.ResponseWriter, r *http.Request) {
	movs, err := h.stock.GetMovements(r.Context())
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, nonNil(movs))
}

// GET /api/inventory/{id}/movements. Incluye el historial de ítems ya eliminados.
func (h *InventoryHandler) itemMovements(w http.ResponseWriter, r *http.Request) {
	id, reqErr := pathID(r)
	if reqErr != nil {
		writeRequestError(w, reqErr)
		return
	}
	movs, err := h.stock.GetMovementsByItemID(r.Context(), id)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, nonNil(movs))
}

// GET /api/health: 200 {"status":"ok"} si el Excel es accesible y su
// estructura es válida; si no, 503 con el formato de error de la API. El
// detalle (rutas, motivo) queda solo en el log.
func (h *InventoryHandler) health(w http.ResponseWriter, r *http.Request) {
	if err := h.checker.CheckHealth(r.Context()); err != nil {
		log.Printf("health: inventario no disponible: %v", err)
		writeError(w, http.StatusServiceUnavailable, CodeServiceUnavailable, "el inventario no está disponible")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// nonNil garantiza que una lista vacía se serialice como [] y no como null.
func nonNil[T any](items []T) []T {
	if items == nil {
		return []T{}
	}
	return items
}

// methods despacha una ruta por método HTTP. Un método no registrado recibe
// 405 en el formato de error de la API, con el encabezado Allow.
type methods map[string]http.HandlerFunc

func (m methods) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	handler, ok := m[r.Method]
	if !ok && r.Method == http.MethodHead {
		handler, ok = m[http.MethodGet]
	}
	if !ok {
		w.Header().Set("Allow", m.allowed())
		writeError(w, http.StatusMethodNotAllowed, CodeMethodNotAllowed, "método "+r.Method+" no permitido en esta ruta")
		return
	}
	handler(w, r)
}

func (m methods) allowed() string {
	list := make([]string, 0, len(m)+1)
	for method := range m {
		list = append(list, method)
	}
	if _, ok := m[http.MethodGet]; ok {
		list = append(list, http.MethodHead)
	}
	slices.Sort(list)
	return strings.Join(list, ", ")
}
