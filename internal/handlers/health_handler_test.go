package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"inventario/internal/repository"
)

func TestHealth_LibroValido(t *testing.T) {
	svc := &fakeService{}
	rec := do(newTestServer(svc), "GET", "/api/health", "", "")

	var body map[string]string
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &body) != nil || body["status"] != "ok" {
		t.Errorf("status %d, cuerpo %s; se esperaba 200 {\"status\":\"ok\"}", rec.Code, rec.Body)
	}
	if !svc.called("CheckHealth") {
		t.Error("no verificó el almacenamiento")
	}
}

func TestHealth_LibroInvalidoOInaccesible(t *testing.T) {
	fallas := []error{
		fmt.Errorf("%w: \"Movimientos\"", repository.ErrSheetMissing),
		fmt.Errorf("%w: C:\\datos\\inventario.xlsx: open: acceso denegado", repository.ErrInvalidWorkbook),
	}
	for _, err := range fallas {
		rec := do(newTestServer(&fakeService{err: err}), "GET", "/api/health", "", "")
		assertError(t, rec, http.StatusServiceUnavailable, CodeServiceUnavailable)
		for _, filtrado := range []string{"inventario.xlsx", "Movimientos", "acceso denegado", `C:\`} {
			if strings.Contains(rec.Body.String(), filtrado) {
				t.Errorf("la respuesta expone %q: %s", filtrado, rec.Body)
			}
		}
	}
}

func TestHealth_MetodoIncorrecto(t *testing.T) {
	svc := &fakeService{}
	rec := do(newTestServer(svc), "POST", "/api/health", "", "")
	assertError(t, rec, http.StatusMethodNotAllowed, CodeMethodNotAllowed)
	assertNotCalled(t, svc)
}
