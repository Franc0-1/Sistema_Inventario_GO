package handlers

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"inventario/internal/models"
	"inventario/internal/repository"
	"inventario/internal/services"
)

type fakeInsumoService struct {
	services.InsumoService
	err        error
	movimiento models.MovimientoInsumo
	filtro     models.FiltroMovimientosInsumo
	usuario    int
}

func (f *fakeInsumoService) ObtenerInsumo(_ context.Context, id int) (models.Insumo, error) {
	return models.Insumo{ID: id}, nil
}

func (f *fakeInsumoService) RegistrarMovimiento(ctx context.Context, m models.MovimientoInsumo) (models.MovimientoInsumo, models.Insumo, error) {
	f.movimiento, f.usuario = m, services.UsuarioDe(ctx)
	return m, models.Insumo{ID: m.InsumoID, Stock: 7}, f.err
}

func (f *fakeInsumoService) ListarMovimientos(_ context.Context, filtro models.FiltroMovimientosInsumo) ([]models.MovimientoInsumo, error) {
	f.filtro = filtro
	return nil, f.err
}

func insumoServer(f *fakeInsumoService) *http.ServeMux {
	mux := http.NewServeMux()
	NewInsumoHandler(f).RegisterRoutes(mux)
	return mux
}

func TestInsumos_RegistrarMovimiento(t *testing.T) {
	f := &fakeInsumoService{}
	rec := pedir(insumoServer(f), http.MethodPost, "/api/insumos/3/movimientos",
		`{"tipo":"SALIDA","cantidad":2,"persona_id":5,"observacion":"Teclado roto"}`, "X-Usuario-ID", "1")
	if rec.Code != http.StatusCreated {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	if f.movimiento.InsumoID != 3 || f.movimiento.Cantidad != 2 || f.movimiento.PersonaID != 5 || f.usuario != 1 {
		t.Errorf("movimiento = %+v, usuario %d", f.movimiento, f.usuario)
	}
	if body := rec.Body.String(); !strings.Contains(body, `"movimiento":`) || !strings.Contains(body, `"stock":7`) {
		t.Errorf("respuesta = %s", body)
	}

	f.err = fmt.Errorf("%w: hay 1 unidades", repository.ErrStockInsuficiente)
	rec = pedir(insumoServer(f), http.MethodPost, "/api/insumos/3/movimientos", `{"tipo":"SALIDA","cantidad":2,"oficina_id":1}`)
	if rec.Code != http.StatusConflict || codigoError(t, rec) != CodeInsufficientStock {
		t.Errorf("sin stock: status %d, %s", rec.Code, rec.Body)
	}
}

func TestInsumos_ReporteDeMovimientos(t *testing.T) {
	f := &fakeInsumoService{}
	rec := pedir(insumoServer(f), http.MethodGet, "/api/movimientos-insumo?oficina_id=2&tipo=SALIDA&desde=2026-10-01&hasta=2026-10-31&limite=50", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	desde := time.Date(2026, 10, 1, 0, 0, 0, 0, time.Local)
	if f.filtro.OficinaID != 2 || f.filtro.Tipo != models.MovimientoSalida || !f.filtro.Desde.Equal(desde) ||
		f.filtro.Hasta.Day() != 31 || f.filtro.Limite != 50 {
		t.Errorf("filtro = %+v", f.filtro)
	}
}

func TestInsumos_PeticionesInvalidas(t *testing.T) {
	mux := insumoServer(&fakeInsumoService{})
	for _, c := range []struct {
		nombre, method, url, body string
		status                    int
		code                      string
	}{
		{"editar el stock", http.MethodPut, "/api/insumos/1", `{"tipo_id":3,"stock":10,"version":"00"}`, 400, CodeInvalidData},
		{"fecha inválida", http.MethodGet, "/api/movimientos-insumo?desde=01/10/2026", "", 400, CodeInvalidQuery},
		{"límite no numérico", http.MethodGet, "/api/insumos/1/movimientos?limite=todos", "", 400, CodeInvalidQuery},
		{"bandera inválida", http.MethodGet, "/api/insumos?bajo_stock=si", "", 400, CodeInvalidQuery},
	} {
		rec := pedir(mux, c.method, c.url, c.body)
		if rec.Code != c.status || codigoError(t, rec) != c.code {
			t.Errorf("%s: status %d, código %q (se esperaba %d %s)", c.nombre, rec.Code, codigoError(t, rec), c.status, c.code)
		}
	}
}
