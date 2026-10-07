package handlers

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"inventario/internal/models"
	"inventario/internal/repository"
	"inventario/internal/services"
)

func TestMain(m *testing.M) {
	log.SetOutput(io.Discard) // el middleware y los 500 loguean; no ensuciar la salida
	os.Exit(m.Run())
}

func multipartBody(t *testing.T, filename string, content []byte) (*bytes.Buffer, string) {
	t.Helper()
	body := &bytes.Buffer{}
	w := multipart.NewWriter(body)
	part, err := w.CreateFormFile("file", filename)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return body, w.FormDataContentType()
}

const jsonType = "application/json"

var (
	notebook = models.Item{ID: 1, InventoryNumber: "1001", HasInventory: true, DeviceType: "Notebook",
		Brand: "Lenovo", Model: "ThinkPad T14", Quantity: 1, Location: "Sistemas", Status: models.StatusOperational}
	mouse = models.Item{ID: 2, DeviceType: "Mouse", Brand: "Logitech", Model: "M280", Quantity: 12,
		Location: "Sistemas", Status: models.StatusOperational}

	validBody = `{"numero_inventario":"1001","tiene_inventario":true,"tipo_dispositivo":"Notebook","marca":"Lenovo",` +
		`"modelo":"ThinkPad T14","numero_serie":"ABC123","cantidad":1,"ubicacion":"Sistemas","estado":"OPERATIVO","observacion":"Nuevo"}`

	notFound = fmt.Errorf("%w: id 99", repository.ErrItemNotFound)
)

// newTestServer arma la misma cadena que main.go (rutas + middleware) sobre un fake.
func newTestServer(svc services.InventoryService, corsOrigins ...string) http.Handler {
	mux := http.NewServeMux()
	NewInventoryHandler(svc).RegisterRoutes(mux)
	return WithMiddleware(mux, corsOrigins)
}

func do(h http.Handler, method, path, body, contentType string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// dataOf decodifica {"data": ...} verificando status y Content-Type.
func dataOf[T any](t *testing.T, rec *httptest.ResponseRecorder, wantStatus int) T {
	t.Helper()
	if rec.Code != wantStatus {
		t.Fatalf("status = %d; se esperaba %d. Cuerpo: %s", rec.Code, wantStatus, rec.Body)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, jsonType) {
		t.Errorf("Content-Type = %q; se esperaba JSON", ct)
	}
	var body struct{ Data T }
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("respuesta no es {data: ...}: %v. Cuerpo: %s", err, rec.Body)
	}
	return body.Data
}

// assertError verifica status, código y el formato {"error": {"code", "message"}}.
func assertError(t *testing.T, rec *httptest.ResponseRecorder, wantStatus int, wantCode string) errorBody {
	t.Helper()
	if rec.Code != wantStatus {
		t.Fatalf("status = %d; se esperaba %d. Cuerpo: %s", rec.Code, wantStatus, rec.Body)
	}
	var body map[string]errorBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || len(body) != 1 {
		t.Fatalf("el error no tiene el formato {\"error\": {...}}: %s", rec.Body)
	}
	e := body["error"]
	if e.Code != wantCode || e.Message == "" {
		t.Errorf("error = %+v; se esperaba código %s y un mensaje", e, wantCode)
	}
	return e
}

func assertNotCalled(t *testing.T, svc *fakeService) {
	t.Helper()
	if len(svc.calls) > 0 {
		t.Errorf("no debería llamar al servicio; llamó a %v", svc.calls)
	}
}

// ============================================================
// GET /api/inventory
// ============================================================

func TestList(t *testing.T) {
	pagination := models.Pagination{Page: 1, PageSize: 2, TotalItems: 2, TotalPages: 1}
	svc := &fakeService{items: []models.Item{notebook, mouse}, pagination: pagination}
	page := dataOf[models.ItemPage](t, do(newTestServer(svc), "GET", "/api/inventory", "", ""), http.StatusOK)

	if len(page.Items) != 2 || page.Items[0].Brand != "Lenovo" || page.Items[1].Quantity != 12 {
		t.Errorf("items = %+v", page.Items)
	}
	if page.Pagination != pagination {
		t.Errorf("pagination = %+v; se esperaba %+v", page.Pagination, pagination)
	}
	if svc.gotQuery != (models.ItemQuery{}) {
		t.Errorf("sin parámetros la consulta debería ir vacía (valores por defecto del servicio); fue %+v", svc.gotQuery)
	}
}

func TestList_FormatoDeRespuesta(t *testing.T) {
	svc := &fakeService{items: []models.Item{notebook}, pagination: models.Pagination{Page: 2, PageSize: 20, TotalItems: 143, TotalPages: 8}}
	body := do(newTestServer(svc), "GET", "/api/inventory?page=2&pageSize=20", "", "").Body.String()
	want := `"pagination":{"page":2,"pageSize":20,"totalItems":143,"totalPages":8}`
	if !strings.HasPrefix(body, `{"data":{"items":[`) || !strings.Contains(body, want) {
		t.Errorf("cuerpo = %s; se esperaba {\"data\":{\"items\":[...],%s}}", body, want)
	}
}

func TestList_UsaLosNombresJSONDelModelo(t *testing.T) {
	rec := do(newTestServer(&fakeService{items: []models.Item{notebook}}), "GET", "/api/inventory", "", "")
	for _, field := range []string{`"numero_inventario":"1001"`, `"tipo_dispositivo":"Notebook"`, `"created_at"`} {
		if !strings.Contains(rec.Body.String(), field) {
			t.Errorf("la respuesta no contiene %s: %s", field, rec.Body)
		}
	}
}

func TestList_VaciaDevuelveArrayNoNull(t *testing.T) {
	rec := do(newTestServer(&fakeService{items: nil}), "GET", "/api/inventory", "", "")
	if got := rec.Body.String(); !strings.Contains(got, `"items":[]`) {
		t.Errorf("cuerpo = %s; se esperaba \"items\":[]", got)
	}
}

// ============================================================
// GET /api/inventory/{id}
// ============================================================

func TestGet(t *testing.T) {
	svc := &fakeService{item: notebook}
	item := dataOf[models.Item](t, do(newTestServer(svc), "GET", "/api/inventory/1", "", ""), http.StatusOK)
	if item.ID != 1 || svc.gotID != 1 {
		t.Errorf("item.ID = %d, el servicio recibió %d; se esperaba 1", item.ID, svc.gotID)
	}
}

func TestGet_IDNoNumerico(t *testing.T) {
	svc := &fakeService{}
	assertError(t, do(newTestServer(svc), "GET", "/api/inventory/abc", "", ""), http.StatusBadRequest, CodeInvalidID)
	assertNotCalled(t, svc)
}

func TestGet_IDNoPositivoLoDecideElServicio(t *testing.T) {
	svc := &fakeService{err: fmt.Errorf("%w: 0", services.ErrInvalidID)}
	assertError(t, do(newTestServer(svc), "GET", "/api/inventory/0", "", ""), http.StatusBadRequest, CodeInvalidID)
}

func TestGet_Inexistente(t *testing.T) {
	svc := &fakeService{err: notFound}
	assertError(t, do(newTestServer(svc), "GET", "/api/inventory/99", "", ""), http.StatusNotFound, CodeItemNotFound)
}

// ============================================================
// GET /api/inventory/search
// ============================================================

func TestSearch(t *testing.T) {
	svc := &fakeService{items: []models.Item{notebook}}
	page := dataOf[models.ItemPage](t, do(newTestServer(svc), "GET", "/api/inventory/search?q=think+pad", "", ""), http.StatusOK)

	if svc.gotQuery.Filter.Text != "think pad" {
		t.Errorf("el servicio recibió q = %q; se esperaba \"think pad\"", svc.gotQuery.Filter.Text)
	}
	if len(page.Items) != 1 || page.Items[0].ID != 1 {
		t.Errorf("items = %+v", page.Items)
	}
}

// La búsqueda general acepta los mismos filtros, orden y paginación que el listado.
func TestSearch_AceptaLosMismosParametrosQueElListado(t *testing.T) {
	const params = "?q=dell&brand=Dell&status=OPERATIVO&sort=brand&order=desc&page=2&pageSize=5"
	porListado, porBusqueda := &fakeService{}, &fakeService{}
	do(newTestServer(porListado), "GET", "/api/inventory"+params, "", "")
	do(newTestServer(porBusqueda), "GET", "/api/inventory/search"+params, "", "")
	if porListado.gotQuery != porBusqueda.gotQuery || porBusqueda.gotQuery.Page != 2 {
		t.Errorf("listado %+v, búsqueda %+v; deberían recibir la misma consulta", porListado.gotQuery, porBusqueda.gotQuery)
	}
}

func TestSearch_SinResultadosNoEsError(t *testing.T) {
	rec := do(newTestServer(&fakeService{items: nil}), "GET", "/api/inventory/search?q=zzz", "", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"items":[]`) {
		t.Errorf("status %d, cuerpo %s; se esperaba 200 con \"items\":[]", rec.Code, rec.Body)
	}
}

// ============================================================
// Importación / exportación Excel
// ============================================================

func TestExportInventory(t *testing.T) {
	svc := &fakeService{exportBytes: []byte("xlsx")}
	rec := do(newTestServer(svc), "GET", "/api/inventory/export", "", "")
	if rec.Code != http.StatusOK || rec.Body.String() != "xlsx" {
		t.Fatalf("status %d, cuerpo %q; se esperaba descarga", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Header().Get("Content-Type"), "spreadsheetml.sheet") {
		t.Errorf("Content-Type = %q", rec.Header().Get("Content-Type"))
	}
	if cd := rec.Header().Get("Content-Disposition"); !strings.Contains(cd, "inventario_") || !strings.Contains(cd, ".xlsx") {
		t.Errorf("Content-Disposition = %q", cd)
	}
	if !svc.called("ExportInventory") {
		t.Error("no llamó a ExportInventory")
	}
}

func TestImportInventory(t *testing.T) {
	body, contentType := multipartBody(t, "inventario.xlsx", []byte("xlsx"))
	req := httptest.NewRequest(http.MethodPost, "/api/inventory/import", body)
	req.Header.Set("Content-Type", contentType)
	rec := httptest.NewRecorder()
	svc := &fakeService{imported: 120}
	newTestServer(svc).ServeHTTP(rec, req)

	data := dataOf[map[string]int](t, rec, http.StatusOK)
	if data["imported"] != 120 || !svc.called("ImportInventory") {
		t.Errorf("respuesta/importación = %v, llamadas = %v", data, svc.calls)
	}
}

func TestImportInventory_ExtensionInvalida(t *testing.T) {
	body, contentType := multipartBody(t, "inventario.csv", []byte("x"))
	req := httptest.NewRequest(http.MethodPost, "/api/inventory/import", body)
	req.Header.Set("Content-Type", contentType)
	rec := httptest.NewRecorder()
	svc := &fakeService{}
	newTestServer(svc).ServeHTTP(rec, req)

	assertError(t, rec, http.StatusBadRequest, CodeInvalidRequest)
	assertNotCalled(t, svc)
}

func TestImportInventory_ErrorValidacion(t *testing.T) {
	body, contentType := multipartBody(t, "inventario.xlsx", []byte("x"))
	req := httptest.NewRequest(http.MethodPost, "/api/inventory/import", body)
	req.Header.Set("Content-Type", contentType)
	rec := httptest.NewRecorder()
	err := &repository.CellError{Sheet: "Inventario", Row: 2, Column: 8, Field: "Quantity", Err: errors.New("cantidad negativa")}
	newTestServer(&fakeService{err: err}).ServeHTTP(rec, req)

	e := assertError(t, rec, http.StatusBadRequest, CodeInvalidRequest)
	if !strings.Contains(e.Message, "celda H2") || !strings.Contains(e.Message, "Quantity") {
		t.Errorf("mensaje = %q; debería indicar fila/campo", e.Message)
	}
}

// ============================================================
// POST /api/inventory
// ============================================================

func TestCreate(t *testing.T) {
	svc := &fakeService{item: notebook}
	item := dataOf[models.Item](t, do(newTestServer(svc), "POST", "/api/inventory", validBody, jsonType), http.StatusCreated)

	if item.ID != 1 {
		t.Errorf("respuesta = %+v", item)
	}
	want := models.Item{InventoryNumber: "1001", HasInventory: true, DeviceType: "Notebook", Brand: "Lenovo",
		Model: "ThinkPad T14", SerialNumber: "ABC123", Quantity: 1, Location: "Sistemas",
		Status: models.StatusOperational, Notes: "Nuevo"}
	if svc.gotItem != want {
		t.Errorf("el servicio recibió\n%+v\nse esperaba\n%+v", svc.gotItem, want)
	}
}

func TestCreate_AceptaCharsetEnContentType(t *testing.T) {
	rec := do(newTestServer(&fakeService{item: notebook}), "POST", "/api/inventory", validBody, "application/json; charset=utf-8")
	if rec.Code != http.StatusCreated {
		t.Errorf("status = %d; se esperaba 201", rec.Code)
	}
}

func TestCreate_PeticionInvalida(t *testing.T) {
	tests := []struct {
		name        string
		body        string
		contentType string
		status      int
		code        string
	}{
		{"sin Content-Type", validBody, "", http.StatusUnsupportedMediaType, CodeUnsupportedMediaType},
		{"Content-Type de texto", validBody, "text/plain", http.StatusUnsupportedMediaType, CodeUnsupportedMediaType},
		{"JSON mal formado", `{"marca": "Lenovo",}`, jsonType, http.StatusBadRequest, CodeInvalidJSON},
		{"JSON incompleto", `{"marca": "Lenovo"`, jsonType, http.StatusBadRequest, CodeInvalidJSON},
		{"cuerpo vacío", ``, jsonType, http.StatusBadRequest, CodeInvalidJSON},
		{"tipo incorrecto", `{"cantidad": "diez"}`, jsonType, http.StatusBadRequest, CodeInvalidJSON},
		{"dos objetos", validBody + validBody, jsonType, http.StatusBadRequest, CodeInvalidJSON},
		{"intenta fijar el ID", `{"id": 50, "marca": "Lenovo"}`, jsonType, http.StatusBadRequest, CodeInvalidJSON},
		{"intenta fijar CreatedAt", `{"created_at": "2020-01-01T00:00:00Z"}`, jsonType, http.StatusBadRequest, CodeInvalidJSON},
		{"intenta fijar el préstamo", `{"disponibilidad": "PRESTADO"}`, jsonType, http.StatusBadRequest, CodeInvalidJSON},
		{"cuerpo demasiado grande", `{"observacion":"` + strings.Repeat("x", maxBodyBytes) + `"}`, jsonType,
			http.StatusRequestEntityTooLarge, CodeRequestTooLarge},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &fakeService{}
			assertError(t, do(newTestServer(svc), "POST", "/api/inventory", tt.body, tt.contentType), tt.status, tt.code)
			assertNotCalled(t, svc)
		})
	}
}

func TestCreate_ErroresDeNegocio(t *testing.T) {
	tests := []struct {
		err    error
		status int
		code   string
	}{
		{fmt.Errorf("%w: falta la marca", services.ErrInvalidItem), http.StatusBadRequest, CodeInvalidItem},
		{services.ErrInventoryNumberRequired, http.StatusBadRequest, CodeInventoryNumberRequired},
		{fmt.Errorf("%w (-1)", services.ErrInvalidQuantity), http.StatusBadRequest, CodeInvalidQuantity},
		{fmt.Errorf("%w: \"1001\"", services.ErrInventoryNumberExists), http.StatusConflict, CodeInventoryNumberExists},
		{fmt.Errorf("%w: \"ABC\"", services.ErrSerialNumberExists), http.StatusConflict, CodeSerialNumberExists},
		{fmt.Errorf("crear: %w", repository.ErrConflict), http.StatusConflict, CodeConflict},
	}
	for _, tt := range tests {
		t.Run(tt.code, func(t *testing.T) {
			svc := &fakeService{err: tt.err}
			e := assertError(t, do(newTestServer(svc), "POST", "/api/inventory", validBody, jsonType), tt.status, tt.code)
			if e.Message != tt.err.Error() {
				t.Errorf("mensaje = %q; se esperaba el del servicio %q", e.Message, tt.err)
			}
		})
	}
}

// ============================================================
// PUT /api/inventory/{id}
// ============================================================

func TestUpdate(t *testing.T) {
	svc := &fakeService{item: notebook}
	item := dataOf[models.Item](t, do(newTestServer(svc), "PUT", "/api/inventory/7", validBody, jsonType), http.StatusOK)

	if item.ID != 1 {
		t.Errorf("respuesta = %+v", item)
	}
	if svc.gotID != 7 || svc.gotItem.ID != 7 {
		t.Errorf("el servicio recibió id=%d, item.ID=%d; ambos deberían ser 7 (de la URL)", svc.gotID, svc.gotItem.ID)
	}
	if svc.gotItem.Brand != "Lenovo" {
		t.Errorf("no llegaron los datos del cuerpo: %+v", svc.gotItem)
	}
}

func TestUpdate_Errores(t *testing.T) {
	tests := []struct {
		name, path, body string
		svcErr           error
		status           int
		code             string
	}{
		{"ID no numérico", "/api/inventory/x", validBody, nil, http.StatusBadRequest, CodeInvalidID},
		{"ID en el cuerpo", "/api/inventory/7", `{"id": 8, "marca": "Lenovo"}`, nil, http.StatusBadRequest, CodeInvalidJSON},
		{"JSON inválido", "/api/inventory/7", `{marca}`, nil, http.StatusBadRequest, CodeInvalidJSON},
		{"inexistente", "/api/inventory/99", validBody, notFound, http.StatusNotFound, CodeItemNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &fakeService{err: tt.svcErr}
			assertError(t, do(newTestServer(svc), "PUT", tt.path, tt.body, jsonType), tt.status, tt.code)
		})
	}
}

func TestUpdate_ContentTypeIncorrecto(t *testing.T) {
	svc := &fakeService{}
	assertError(t, do(newTestServer(svc), "PUT", "/api/inventory/7", validBody, "text/plain"),
		http.StatusUnsupportedMediaType, CodeUnsupportedMediaType)
	assertNotCalled(t, svc)
}

// ============================================================
// DELETE /api/inventory/{id}
// ============================================================

func TestDelete(t *testing.T) {
	svc := &fakeService{}
	rec := do(newTestServer(svc), "DELETE", "/api/inventory/3", "", "")
	if rec.Code != http.StatusNoContent || rec.Body.Len() != 0 {
		t.Errorf("status %d, cuerpo %q; se esperaba 204 sin cuerpo", rec.Code, rec.Body)
	}
	if svc.gotID != 3 {
		t.Errorf("el servicio recibió id %d; se esperaba 3", svc.gotID)
	}
}

func TestDelete_Errores(t *testing.T) {
	assertError(t, do(newTestServer(&fakeService{err: notFound}), "DELETE", "/api/inventory/99", "", ""),
		http.StatusNotFound, CodeItemNotFound)
	assertError(t, do(newTestServer(&fakeService{}), "DELETE", "/api/inventory/1.5", "", ""),
		http.StatusBadRequest, CodeInvalidID)
}

// ============================================================
// PATCH /api/inventory/{id}/stock
// ============================================================

func TestUpdateStock(t *testing.T) {
	svc := &fakeService{item: mouse}
	item := dataOf[models.Item](t, do(newTestServer(svc), "PATCH", "/api/inventory/2/stock", `{"cantidad": 10}`, jsonType), http.StatusOK)

	if svc.gotID != 2 || svc.gotQuantity != 10 {
		t.Errorf("el servicio recibió id=%d cantidad=%d; se esperaba 2 y 10", svc.gotID, svc.gotQuantity)
	}
	if item.ID != 2 {
		t.Errorf("respuesta = %+v", item)
	}
}

func TestUpdateStock_CeroEsValido(t *testing.T) {
	svc := &fakeService{item: mouse}
	do(newTestServer(svc), "PATCH", "/api/inventory/2/stock", `{"cantidad": 0}`, jsonType)
	if !svc.called("UpdateStock") || svc.gotQuantity != 0 {
		t.Error("cantidad 0 debería llegar al servicio")
	}
}

func TestUpdateStock_Errores(t *testing.T) {
	tests := []struct {
		name, path, body string
		svcErr           error
		status           int
		code             string
	}{
		{"sin cantidad", "/api/inventory/2/stock", `{}`, nil, http.StatusBadRequest, CodeInvalidRequest},
		{"cantidad no numérica", "/api/inventory/2/stock", `{"cantidad": "diez"}`, nil, http.StatusBadRequest, CodeInvalidJSON},
		{"intenta cambiar otro campo", "/api/inventory/2/stock", `{"cantidad": 5, "marca": "Otra"}`, nil, http.StatusBadRequest, CodeInvalidJSON},
		{"cantidad negativa", "/api/inventory/2/stock", `{"cantidad": -1}`,
			fmt.Errorf("%w (-1)", services.ErrInvalidQuantity), http.StatusBadRequest, CodeInvalidQuantity},
		{"ID no numérico", "/api/inventory/dos/stock", `{"cantidad": 5}`, nil, http.StatusBadRequest, CodeInvalidID},
		{"inexistente", "/api/inventory/99/stock", `{"cantidad": 5}`, notFound, http.StatusNotFound, CodeItemNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &fakeService{err: tt.svcErr}
			assertError(t, do(newTestServer(svc), "PATCH", tt.path, tt.body, jsonType), tt.status, tt.code)
		})
	}
}

// ============================================================
// Métodos, rutas y errores internos
// ============================================================

func TestMetodoIncorrecto(t *testing.T) {
	tests := []struct{ method, path, allow string }{
		{"DELETE", "/api/inventory", "GET, HEAD, POST"},
		{"PATCH", "/api/inventory/1", "DELETE, GET, HEAD, PUT"},
		{"POST", "/api/inventory/search", "GET, HEAD"},
		{"GET", "/api/inventory/1/stock", "PATCH"},
	}
	for _, tt := range tests {
		t.Run(tt.method+" "+tt.path, func(t *testing.T) {
			svc := &fakeService{}
			rec := do(newTestServer(svc), tt.method, tt.path, "", "")
			assertError(t, rec, http.StatusMethodNotAllowed, CodeMethodNotAllowed)
			if got := rec.Header().Get("Allow"); got != tt.allow {
				t.Errorf("Allow = %q; se esperaba %q", got, tt.allow)
			}
			assertNotCalled(t, svc)
		})
	}
}

func TestRutaInexistenteDevuelveJSON(t *testing.T) {
	assertError(t, do(newTestServer(&fakeService{}), "GET", "/api/items", "", ""), http.StatusNotFound, CodeNotFound)
}

func TestErrorInternoNoSeFiltra(t *testing.T) {
	internos := []error{
		errors.New("open /data/inventario.xlsx: permission denied"),
		fmt.Errorf("%w: /data/inventario.xlsx: zip: not a valid zip file", repository.ErrInvalidWorkbook),
	}
	for _, err := range internos {
		rec := do(newTestServer(&fakeService{err: err}), "GET", "/api/inventory", "", "")
		assertError(t, rec, http.StatusInternalServerError, CodeInternal)
		for _, leak := range []string{"inventario.xlsx", "permission", "zip", "/data"} {
			if strings.Contains(rec.Body.String(), leak) {
				t.Errorf("la respuesta filtra %q: %s", leak, rec.Body)
			}
		}
	}
}

func TestPanicDevuelve500JSON(t *testing.T) {
	assertError(t, do(newTestServer(&fakeService{panic: true}), "GET", "/api/inventory", "", ""),
		http.StatusInternalServerError, CodeInternal)
}

// ============================================================
// CORS
// ============================================================

func TestCORS(t *testing.T) {
	const permitido = "http://localhost:5173"

	t.Run("sin orígenes configurados no agrega encabezados", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/inventory", nil)
		req.Header.Set("Origin", permitido)
		rec := httptest.NewRecorder()
		newTestServer(&fakeService{}).ServeHTTP(rec, req)
		if rec.Header().Get("Access-Control-Allow-Origin") != "" {
			t.Error("no debería haber CORS si el frontend se sirve desde el mismo origen")
		}
	})

	t.Run("preflight de un origen permitido", func(t *testing.T) {
		req := httptest.NewRequest("OPTIONS", "/api/inventory/1", nil)
		req.Header.Set("Origin", permitido)
		req.Header.Set("Access-Control-Request-Method", "PUT")
		rec := httptest.NewRecorder()
		newTestServer(&fakeService{}, permitido).ServeHTTP(rec, req)
		if rec.Code != http.StatusNoContent || rec.Header().Get("Access-Control-Allow-Origin") != permitido ||
			!strings.Contains(rec.Header().Get("Access-Control-Allow-Methods"), "PUT") {
			t.Errorf("preflight: status %d, encabezados %v", rec.Code, rec.Header())
		}
	})

	t.Run("origen no permitido", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/inventory", nil)
		req.Header.Set("Origin", "http://malicioso.example")
		rec := httptest.NewRecorder()
		newTestServer(&fakeService{}, permitido).ServeHTTP(rec, req)
		if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
			t.Errorf("Access-Control-Allow-Origin = %q; no debería autorizar ese origen", got)
		}
	})
}
