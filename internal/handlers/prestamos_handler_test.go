package handlers

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"inventario/internal/models"
	"inventario/internal/repository"
	"inventario/internal/services"
)

type fakePrestamoService struct {
	services.PrestamoService
	err      error
	prestamo models.Prestamo
	filtro   models.FiltroPrestamos
	nota     string
}

func (f *fakePrestamoService) ListarPrestamos(_ context.Context, filtro models.FiltroPrestamos) ([]models.Prestamo, error) {
	f.filtro = filtro
	return nil, f.err
}

func (f *fakePrestamoService) Prestar(_ context.Context, p models.Prestamo) (models.Prestamo, error) {
	f.prestamo = p
	return p, f.err
}

func (f *fakePrestamoService) Devolver(_ context.Context, id int, nota string) (models.Prestamo, error) {
	f.nota = nota
	return models.Prestamo{ID: id}, f.err
}

func prestamoServer(f *fakePrestamoService) *http.ServeMux {
	mux := http.NewServeMux()
	NewPrestamoHandler(f).RegisterRoutes(mux)
	return mux
}

func TestPrestamos_PrestarConHoraLocal(t *testing.T) {
	f := &fakePrestamoService{}
	rec := pedir(prestamoServer(f), http.MethodPost, "/api/prestamos",
		`{"equipo_id":3,"persona_id":1,"devolucion_prevista":"2026-10-09T18:30"}`, "X-Usuario-ID", "1")
	if rec.Code != http.StatusCreated {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	if want := time.Date(2026, 10, 9, 18, 30, 0, 0, time.Local); !f.prestamo.DevolucionPrevista.Equal(want) || f.prestamo.EquipoID != 3 {
		t.Errorf("préstamo = %+v", f.prestamo)
	}
	rec = pedir(prestamoServer(f), http.MethodPost, "/api/prestamos", `{"equipo_id":3,"persona_id":1,"devolucion_prevista":"mañana"}`)
	if rec.Code != http.StatusBadRequest || codigoError(t, rec) != CodeInvalidData {
		t.Errorf("fecha inválida: status %d, %s", rec.Code, rec.Body)
	}
}

func TestPrestamos_DevolucionYErrores(t *testing.T) {
	f := &fakePrestamoService{}
	rec := pedir(prestamoServer(f), http.MethodPost, "/api/prestamos/5/devolucion", `{"nota":"Volvió sin cargador"}`)
	if rec.Code != http.StatusOK || f.nota != "Volvió sin cargador" {
		t.Errorf("status %d, nota %q", rec.Code, f.nota)
	}
	for _, c := range []struct {
		err  error
		code string
	}{
		{fmt.Errorf("%w: el préstamo ya se devolvió", repository.ErrEstadoInvalido), CodeInvalidState},
		{fmt.Errorf("%w: el equipo ya está prestado a Ana", repository.ErrEnUso), CodeInUse},
	} {
		rec := pedir(prestamoServer(&fakePrestamoService{err: c.err}), http.MethodPost, "/api/prestamos/5/devolucion", `{}`)
		if rec.Code != http.StatusConflict || codigoError(t, rec) != c.code {
			t.Errorf("%v: status %d, %s", c.err, rec.Code, rec.Body)
		}
	}
	rec = pedir(prestamoServer(f), http.MethodGet, "/api/prestamos?vencidos=1&persona_id=2", "")
	if rec.Code != http.StatusOK || !f.filtro.Vencidos || f.filtro.PersonaID != 2 {
		t.Errorf("listado: status %d, filtro %+v", rec.Code, f.filtro)
	}
}
