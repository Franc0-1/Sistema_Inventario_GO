package handlers

import (
	"fmt"
	"net/http"
	"testing"

	"inventario/internal/models"
	"inventario/internal/services"
)

// ============================================================
// GET /api/inventory: filtros, orden y paginación
// ============================================================

func TestList_TraduceLosParametros(t *testing.T) {
	svc := &fakeService{}
	path := "/api/inventory?q=think&deviceType=Notebook&excludeDeviceType=Aire+acondicionado&brand=Dell" +
		"&model=Latitude&inventoryNumber=100&serialNumber=SN-9&location=Oficina+2&status=OPERATIVO" +
		"&availability=DISPONIBLE&hasInventory=false&includeRetired=TRUE&sort=brand&order=desc&page=3&pageSize=50"
	dataOf[models.ItemPage](t, do(newTestServer(svc), "GET", path, "", ""), http.StatusOK)

	got := svc.gotQuery
	if got.Filter.HasInventory == nil || *got.Filter.HasInventory {
		t.Fatalf("hasInventory=false llegó como %v", got.Filter.HasInventory)
	}
	got.Filter.HasInventory = nil // el puntero no se compara con ==
	want := models.ItemQuery{
		Filter: models.ItemFilter{
			Text: "think", DeviceType: "Notebook", ExcludeDeviceType: "Aire acondicionado", Brand: "Dell",
			Model: "Latitude", InventoryNumber: "100", SerialNumber: "SN-9", Location: "Oficina 2",
			Status: models.StatusOperational, Availability: models.Available, IncludeRetired: true,
		},
		Sort: models.SortByBrand, Order: models.Descending, Page: 3, PageSize: 50,
	}
	if got != want {
		t.Errorf("consulta = %+v\nse esperaba  %+v", got, want)
	}
}

func TestList_HasInventoryTrue(t *testing.T) {
	svc := &fakeService{}
	do(newTestServer(svc), "GET", "/api/inventory?hasInventory=true", "", "")
	if h := svc.gotQuery.Filter.HasInventory; h == nil || !*h {
		t.Errorf("hasInventory=true llegó como %v", h)
	}
}

// Los valores de estado y orden no se validan en el handler: los pasa tal
// cual y la lista blanca la aplica el servicio.
func TestList_ValoresSinValidarLlegan(t *testing.T) {
	svc := &fakeService{}
	do(newTestServer(svc), "GET", "/api/inventory?status=operativo&sort=Brand", "", "")
	if svc.gotQuery.Filter.Status != "operativo" || svc.gotQuery.Sort != "Brand" {
		t.Errorf("consulta = %+v", svc.gotQuery)
	}
}

func TestList_ParametrosConFormatoInvalido(t *testing.T) {
	for _, query := range []string{
		"Brand=Dell",            // desconocido (mayúsculas)
		"sort=brand&sort=model", // repetido
		"page=0",                // menor a 1
		"page=-2",
		"page=dos", // no numérico
		"page=1.5",
		"pageSize=0",
		"pageSize=abc",
		"page=99999999999999999999", // desborda int
		"hasInventory=si",           // solo true/false
		"includeRetired=1",
		"id=1", // no es un filtro
	} {
		t.Run(query, func(t *testing.T) {
			for _, ruta := range []string{"/api/inventory?", "/api/inventory/search?"} {
				svc := &fakeService{}
				assertError(t, do(newTestServer(svc), "GET", ruta+query, "", ""), http.StatusBadRequest, CodeInvalidQuery)
				assertNotCalled(t, svc)
			}
		})
	}
}

// Parámetros vacíos (p. ej. un formulario sin completar) equivalen a no enviarlos.
func TestList_ParametrosVaciosNoFiltran(t *testing.T) {
	svc := &fakeService{}
	dataOf[models.ItemPage](t, do(newTestServer(svc), "GET", "/api/inventory?brand=&page=&hasInventory=&sort=", "", ""), http.StatusOK)
	if svc.gotQuery != (models.ItemQuery{}) {
		t.Errorf("consulta = %+v; se esperaba vacía", svc.gotQuery)
	}
}

func TestList_ConsultaRechazadaPorElServicio(t *testing.T) {
	svc := &fakeService{err: fmt.Errorf("%w: campo de orden \"password\" desconocido", services.ErrInvalidQuery)}
	e := assertError(t, do(newTestServer(svc), "GET", "/api/inventory?sort=password", "", ""), http.StatusBadRequest, CodeInvalidQuery)
	if e.Message != `consulta inválida: campo de orden "password" desconocido` {
		t.Errorf("mensaje = %q", e.Message)
	}
}
