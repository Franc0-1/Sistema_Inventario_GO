package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"inventario/internal/models"
	"inventario/internal/repository"
	"inventario/internal/services"
)

// fakeCatalogoService registra lo que recibe y devuelve err. Los métodos no
// sobrescritos llaman a la interfaz nil embebida (panic si un test los usa).
type fakeCatalogoService struct {
	services.CatalogoService
	err      error
	usuario  int
	catalogo models.Catalogo
	tabla    models.TablaCatalogo
	activos  bool
	filtro   models.FiltroPersonas
}

func (f *fakeCatalogoService) ListarCatalogo(_ context.Context, t models.TablaCatalogo, activos bool) ([]models.Catalogo, error) {
	f.tabla, f.activos = t, activos
	return nil, f.err
}

func (f *fakeCatalogoService) CrearCatalogo(ctx context.Context, t models.TablaCatalogo, c models.Catalogo) (models.Catalogo, error) {
	f.tabla, f.catalogo, f.usuario = t, c, services.UsuarioDe(ctx)
	c.ID = 7
	return c, f.err
}

func (f *fakeCatalogoService) ActualizarCatalogo(ctx context.Context, t models.TablaCatalogo, c models.Catalogo) (models.Catalogo, error) {
	f.tabla, f.catalogo, f.usuario = t, c, services.UsuarioDe(ctx)
	return c, f.err
}

func (f *fakeCatalogoService) ListarPersonas(_ context.Context, filtro models.FiltroPersonas) ([]models.Persona, error) {
	f.filtro = filtro
	return nil, f.err
}

func catalogoServer(f *fakeCatalogoService) *http.ServeMux {
	mux := http.NewServeMux()
	NewCatalogoHandler(f).RegisterRoutes(mux)
	return mux
}

func pedir(mux *http.ServeMux, method, url, body string, headers ...string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, url, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func codigoError(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var body errorResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("respuesta no es JSON: %s", rec.Body)
	}
	return body.Error.Code
}

func TestCatalogos_ListadoConFiltro(t *testing.T) {
	f := &fakeCatalogoService{}
	rec := pedir(catalogoServer(f), http.MethodGet, "/api/oficinas?activos=1", "")
	if rec.Code != http.StatusOK || f.tabla != models.CatalogoOficinas || !f.activos {
		t.Fatalf("status %d, tabla %q, activos %v", rec.Code, f.tabla, f.activos)
	}
	if !strings.Contains(rec.Body.String(), `"data":[]`) {
		t.Errorf("una lista vacía debe ser []: %s", rec.Body)
	}
	if rec := pedir(catalogoServer(f), http.MethodGet, "/api/puestos?activos=talvez", ""); rec.Code != http.StatusBadRequest {
		t.Errorf("activos inválido: status %d", rec.Code)
	}
}

func TestCatalogos_AltaConUsuario(t *testing.T) {
	f := &fakeCatalogoService{}
	rec := pedir(catalogoServer(f), http.MethodPost, "/api/puestos", `{"nombre":"Técnico"}`, "X-Usuario-ID", "3")
	if rec.Code != http.StatusCreated {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	if f.tabla != models.CatalogoPuestos || f.catalogo.Nombre != "Técnico" || !f.catalogo.Activo || f.usuario != 3 {
		t.Errorf("el servicio recibió %+v (tabla %q, usuario %d)", f.catalogo, f.tabla, f.usuario)
	}
}

func TestCatalogos_EdicionConVersion(t *testing.T) {
	f := &fakeCatalogoService{}
	rec := pedir(catalogoServer(f), http.MethodPut, "/api/usuarios/5", `{"nombre":"Franco","activo":false,"version":"00000000000007d1"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	if f.catalogo.ID != 5 || f.catalogo.Activo || f.catalogo.Version != "00000000000007d1" {
		t.Errorf("el servicio recibió %+v", f.catalogo)
	}
}

func TestCatalogos_PeticionesInvalidas(t *testing.T) {
	mux := catalogoServer(&fakeCatalogoService{})
	for _, c := range []struct {
		nombre, method, url, body string
		headers                   []string
		status                    int
		code                      string
	}{
		{"campo desconocido", http.MethodPost, "/api/oficinas", `{"nombre":"X","id":3}`, nil, 400, CodeInvalidJSON},
		{"usuario no numérico", http.MethodPost, "/api/oficinas", `{"nombre":"X"}`, []string{"X-Usuario-ID", "franco"}, 400, CodeInvalidUser},
		{"ID no numérico", http.MethodPut, "/api/oficinas/abc", `{"nombre":"X"}`, nil, 400, CodeInvalidID},
		{"no se borra", http.MethodDelete, "/api/oficinas/1", "", nil, 405, CodeMethodNotAllowed},
		{"oficina_id inválida", http.MethodGet, "/api/personas?oficina_id=-1", "", nil, 400, CodeInvalidQuery},
	} {
		rec := pedir(mux, c.method, c.url, c.body, c.headers...)
		if rec.Code != c.status || codigoError(t, rec) != c.code {
			t.Errorf("%s: status %d, código %q (se esperaba %d %s)", c.nombre, rec.Code, codigoError(t, rec), c.status, c.code)
		}
	}
}

func TestCatalogos_ErroresDelServicio(t *testing.T) {
	for _, c := range []struct {
		err    error
		status int
		code   string
	}{
		{fmt.Errorf("%w: el nombre es obligatorio", services.ErrDatoInvalido), 400, CodeInvalidData},
		{fmt.Errorf("%w: 9", services.ErrUsuarioInvalido), 400, CodeInvalidUser},
		{fmt.Errorf("%w: la oficina 9", repository.ErrNoEncontrado), 404, CodeNotFound},
		{fmt.Errorf("%w: ya existe una oficina llamada %q", repository.ErrDuplicado, "X"), 409, CodeAlreadyExists},
		{fmt.Errorf("%w: la oficina tiene personas", repository.ErrEnUso), 409, CodeInUse},
		{fmt.Errorf("%w: la oficina 1", repository.ErrConflict), 409, CodeConflict},
	} {
		rec := pedir(catalogoServer(&fakeCatalogoService{err: c.err}), http.MethodPost, "/api/oficinas", `{"nombre":"X"}`)
		if rec.Code != c.status || codigoError(t, rec) != c.code {
			t.Errorf("%v: status %d, código %q (se esperaba %d %s)", c.err, rec.Code, codigoError(t, rec), c.status, c.code)
		}
	}
}

func TestCatalogos_FiltroDePersonas(t *testing.T) {
	f := &fakeCatalogoService{}
	rec := pedir(catalogoServer(f), http.MethodGet, "/api/personas?oficina_id=2&q=ana&activos=1", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	if f.filtro != (models.FiltroPersonas{OficinaID: 2, Texto: "ana", SoloActivas: true}) {
		t.Errorf("filtro = %+v", f.filtro)
	}
}
