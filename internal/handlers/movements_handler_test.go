package handlers

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"inventario/internal/models"
	"inventario/internal/services"
)

var historial = []models.Movement{
	{ID: 1, ItemID: 15, MovementType: models.MovementStockIn, Quantity: 5, PreviousQuantity: 10, NewQuantity: 15,
		Notes: "Ingreso de equipos", CreatedAt: time.Date(2026, 9, 29, 9, 0, 0, 0, time.UTC)},
	{ID: 2, ItemID: 15, MovementType: models.MovementStockOut, Quantity: 3, PreviousQuantity: 15, NewQuantity: 12},
}

// ============================================================
// GET /api/movements
// ============================================================

func TestMovements(t *testing.T) {
	svc := &fakeService{movements: historial}
	movs := dataOf[[]models.Movement](t, do(newTestServer(svc), "GET", "/api/movements", "", ""), http.StatusOK)

	if len(movs) != 2 || movs[0] != historial[0] {
		t.Errorf("movimientos = %+v", movs)
	}
	if !svc.called("GetMovements") {
		t.Error("no llamó a service.GetMovements")
	}
}

func TestMovements_FormatoJSON(t *testing.T) {
	rec := do(newTestServer(&fakeService{movements: historial[:1]}), "GET", "/api/movements", "", "")
	for _, campo := range []string{`"item_id":15`, `"tipo":"stock_in"`, `"cantidad":5`, `"cantidad_anterior":10`,
		`"cantidad_nueva":15`, `"ubicacion_origen":""`, `"ubicacion_destino":""`, `"observacion":"Ingreso de equipos"`, `"created_at"`} {
		if !strings.Contains(rec.Body.String(), campo) {
			t.Errorf("la respuesta no contiene %s: %s", campo, rec.Body)
		}
	}
}

func TestMovements_VacioDevuelveArray(t *testing.T) {
	rec := do(newTestServer(&fakeService{movements: nil}), "GET", "/api/movements", "", "")
	if got := strings.TrimSpace(rec.Body.String()); rec.Code != http.StatusOK || got != `{"data":[]}` {
		t.Errorf("status %d, cuerpo %s; se esperaba 200 {\"data\":[]}", rec.Code, got)
	}
}

// ============================================================
// GET /api/inventory/{id}/movements
// ============================================================

func TestItemMovements(t *testing.T) {
	svc := &fakeService{movements: historial}
	movs := dataOf[[]models.Movement](t, do(newTestServer(svc), "GET", "/api/inventory/15/movements", "", ""), http.StatusOK)

	if svc.gotID != 15 || len(movs) != 2 {
		t.Errorf("el servicio recibió id %d; respuesta %+v", svc.gotID, movs)
	}
}

func TestItemMovements_Errores(t *testing.T) {
	tests := []struct {
		name, path string
		svcErr     error
		status     int
		code       string
	}{
		{"ID no numérico", "/api/inventory/abc/movements", nil, http.StatusBadRequest, CodeInvalidID},
		{"ID no positivo", "/api/inventory/0/movements", fmt.Errorf("%w: 0", services.ErrInvalidID), http.StatusBadRequest, CodeInvalidID},
		{"elemento inexistente", "/api/inventory/99/movements", notFound, http.StatusNotFound, CodeItemNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertError(t, do(newTestServer(&fakeService{err: tt.svcErr}), "GET", tt.path, "", ""), tt.status, tt.code)
		})
	}
}

func TestMovements_MetodoIncorrecto(t *testing.T) {
	tests := []struct{ method, path string }{
		{"POST", "/api/movements"},
		{"DELETE", "/api/movements"},
		{"POST", "/api/inventory/15/movements"},
		{"PUT", "/api/inventory/15/movements"},
	}
	for _, tt := range tests {
		t.Run(tt.method+" "+tt.path, func(t *testing.T) {
			svc := &fakeService{}
			rec := do(newTestServer(svc), tt.method, tt.path, "", "")
			assertError(t, rec, http.StatusMethodNotAllowed, CodeMethodNotAllowed)
			if rec.Header().Get("Allow") != "GET, HEAD" {
				t.Errorf("Allow = %q; se esperaba \"GET, HEAD\"", rec.Header().Get("Allow"))
			}
			assertNotCalled(t, svc)
		})
	}
}

func TestMovements_ErrorInternoNoSeFiltra(t *testing.T) {
	rec := do(newTestServer(&fakeService{err: fmt.Errorf("leer hoja Movimientos: data/inventario.xlsx corrupto")}),
		"GET", "/api/movements", "", "")
	assertError(t, rec, http.StatusInternalServerError, CodeInternal)
	if strings.Contains(rec.Body.String(), "inventario.xlsx") {
		t.Errorf("la respuesta filtra detalles internos: %s", rec.Body)
	}
}

// ============================================================
// PATCH /api/inventory/{id}/stock con tipo de movimiento (Etapa 10)
// ============================================================

func TestStock_ConTipoUsaApplyMovement(t *testing.T) {
	tests := []struct {
		body string
		want models.StockOperation
	}{
		{`{"tipo": "stock_in", "cantidad": 5, "observacion": "compra"}`,
			models.StockOperation{Type: models.MovementStockIn, Quantity: 5, Notes: "compra"}},
		{`{"tipo": "stock_out", "cantidad": 3}`, models.StockOperation{Type: models.MovementStockOut, Quantity: 3}},
		{`{"tipo": "adjustment", "cantidad": 0}`, models.StockOperation{Type: models.MovementAdjustment, Quantity: 0}},
		{`{"tipo": "stock_update", "cantidad": 10}`, models.StockOperation{Type: models.MovementStockUpdate, Quantity: 10}},
		{`{"tipo": "transfer", "ubicacion_destino": "Depósito"}`,
			models.StockOperation{Type: models.MovementTransfer, DestinationLocation: "Depósito"}},
		{`{"cantidad": 4, "observacion": "recuento"}`, // sin tipo pero con observación: fija la cantidad
			models.StockOperation{Type: models.MovementStockUpdate, Quantity: 4, Notes: "recuento"}},
		{`{"tipo": "robo", "cantidad": 1}`, models.StockOperation{Type: "robo", Quantity: 1}}, // lo valida el servicio
	}
	for _, tt := range tests {
		t.Run(tt.body, func(t *testing.T) {
			svc := &fakeService{item: mouse}
			dataOf[models.Item](t, do(newTestServer(svc), "PATCH", "/api/inventory/2/stock", tt.body, jsonType), http.StatusOK)
			if svc.gotID != 2 || svc.gotOperation != tt.want || svc.called("UpdateStock") {
				t.Errorf("llamadas %v, operación %+v; se esperaba %+v", svc.calls, svc.gotOperation, tt.want)
			}
		})
	}
}

// El cuerpo original {"cantidad": n} sigue funcionando igual.
func TestStock_SinTipoSigueUsandoUpdateStock(t *testing.T) {
	svc := &fakeService{item: mouse}
	do(newTestServer(svc), "PATCH", "/api/inventory/2/stock", `{"cantidad": 7}`, jsonType)
	if !svc.called("UpdateStock") || svc.gotQuantity != 7 || svc.called("ApplyMovement") {
		t.Errorf("llamadas %v, cantidad %d", svc.calls, svc.gotQuantity)
	}
}

func TestStock_ConTipoErrores(t *testing.T) {
	tests := []struct {
		name, body string
		svcErr     error
		status     int
		code       string
	}{
		{"tipo sin cantidad", `{"tipo": "stock_in"}`, nil, http.StatusBadRequest, CodeInvalidRequest},
		{"transferencia con cantidad", `{"tipo": "transfer", "cantidad": 2, "ubicacion_destino": "X"}`, nil, http.StatusBadRequest, CodeInvalidRequest},
		{"campo desconocido", `{"tipo": "stock_in", "cantidad": 1, "item_id": 9}`, nil, http.StatusBadRequest, CodeInvalidJSON},
		{"salida mayor al stock", `{"tipo": "stock_out", "cantidad": 99}`,
			fmt.Errorf("%w: se pidieron 99 unidades y hay 12", services.ErrInsufficientStock), http.StatusConflict, CodeInsufficientStock},
		{"tipo inválido", `{"tipo": "robo", "cantidad": 1}`,
			fmt.Errorf("%w: tipo \"robo\" desconocido", services.ErrInvalidMovement), http.StatusBadRequest, CodeInvalidMovement},
		{"cantidad negativa", `{"tipo": "stock_in", "cantidad": -1}`, services.ErrInvalidQuantity, http.StatusBadRequest, CodeInvalidQuantity},
		{"ítem inexistente", `{"tipo": "stock_in", "cantidad": 1}`, notFound, http.StatusNotFound, CodeItemNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &fakeService{err: tt.svcErr}
			assertError(t, do(newTestServer(svc), "PATCH", "/api/inventory/2/stock", tt.body, jsonType), tt.status, tt.code)
			if tt.svcErr == nil {
				assertNotCalled(t, svc)
			}
		})
	}
}
