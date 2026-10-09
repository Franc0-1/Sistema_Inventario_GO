package handlers

import (
	"context"
	"net/http"
	"testing"

	"inventario/internal/models"
	"inventario/internal/services"
)

type fakeEquipoService struct {
	services.EquipoService
	err        error
	usuario    int
	nota       string
	equipo     models.Equipo
	componente models.Componente
	filtro     models.FiltroEquipos
	filtroComp models.FiltroComponentes
}

func (f *fakeEquipoService) ListarEquipos(_ context.Context, filtro models.FiltroEquipos) ([]models.Equipo, error) {
	f.filtro = filtro
	return nil, f.err
}

func (f *fakeEquipoService) ActualizarEquipo(ctx context.Context, e models.Equipo, nota string) (models.Equipo, error) {
	f.equipo, f.nota, f.usuario = e, nota, services.UsuarioDe(ctx)
	return e, f.err
}

func (f *fakeEquipoService) ListarComponentes(_ context.Context, filtro models.FiltroComponentes) ([]models.Componente, error) {
	f.filtroComp = filtro
	return nil, f.err
}

func (f *fakeEquipoService) CrearComponente(ctx context.Context, c models.Componente, nota string) (models.Componente, error) {
	f.componente, f.nota, f.usuario = c, nota, services.UsuarioDe(ctx)
	return c, f.err
}

func equipoServer(f *fakeEquipoService) *http.ServeMux {
	mux := http.NewServeMux()
	NewEquipoHandler(f).RegisterRoutes(mux)
	return mux
}

func TestEquipos_FiltrosDelListado(t *testing.T) {
	f := &fakeEquipoService{}
	rec := pedir(equipoServer(f), http.MethodGet, "/api/equipos?q=dell&tipo_id=1&oficina_id=2&persona_id=3&estado=baja&incluir_bajas=1&pendientes=true", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	want := models.FiltroEquipos{Texto: "dell", TipoID: 1, OficinaID: 2, PersonaID: 3, Estado: "baja", IncluirBajas: true, Pendientes: true}
	if f.filtro != want {
		t.Errorf("filtro = %+v", f.filtro)
	}
	rec = pedir(equipoServer(f), http.MethodGet, "/api/componentes?equipo_id=4&sueltos=0", "")
	if rec.Code != http.StatusOK || f.filtroComp.EquipoID != 4 {
		t.Errorf("componentes: status %d, filtro %+v", rec.Code, f.filtroComp)
	}
}

func TestEquipos_EscriturasConUsuarioYNota(t *testing.T) {
	f := &fakeEquipoService{}
	rec := pedir(equipoServer(f), http.MethodPut, "/api/equipos/8",
		`{"tipo_id":1,"oficina_id":2,"persona_id":3,"estado":"OPERATIVO","nota":"Pasa a Prensa","version":"00000000000007d1"}`,
		"X-Usuario-ID", "4")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	if f.equipo.ID != 8 || f.equipo.PersonaID != 3 || f.nota != "Pasa a Prensa" || f.usuario != 4 {
		t.Errorf("equipo %+v, nota %q, usuario %d", f.equipo, f.nota, f.usuario)
	}
	rec = pedir(equipoServer(f), http.MethodPost, "/api/componentes", `{"tipo_id":2,"equipo_id":8}`, "X-Usuario-ID", "4")
	if rec.Code != http.StatusCreated || f.componente.EquipoID != 8 {
		t.Errorf("componente: status %d, %+v", rec.Code, f.componente)
	}
}

func TestEquipos_PeticionesInvalidas(t *testing.T) {
	mux := equipoServer(&fakeEquipoService{})
	for _, c := range []struct {
		nombre, method, url, body string
		status                    int
		code                      string
	}{
		{"tipo_id no numérico", http.MethodGet, "/api/equipos?tipo_id=pc", "", 400, CodeInvalidQuery},
		{"bandera inválida", http.MethodGet, "/api/componentes?sueltos=quizas", "", 400, CodeInvalidQuery},
		{"campo de solo lectura", http.MethodPost, "/api/equipos", `{"tipo_id":1,"oficina":"Sistemas"}`, 400, CodeInvalidJSON},
		{"no se borra", http.MethodDelete, "/api/equipos/1", "", 405, CodeMethodNotAllowed},
		{"historial no se escribe", http.MethodPost, "/api/componentes/1/historial", "", 405, CodeMethodNotAllowed},
	} {
		rec := pedir(mux, c.method, c.url, c.body)
		if rec.Code != c.status || codigoError(t, rec) != c.code {
			t.Errorf("%s: status %d, código %q (se esperaba %d %s)", c.nombre, rec.Code, codigoError(t, rec), c.status, c.code)
		}
	}
}
