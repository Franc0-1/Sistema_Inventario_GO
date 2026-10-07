package handlers

import (
	"errors"
	"inventario/internal/services"
	"net/http"
	"strings"
	"testing"
)

func TestEquipmentHTTP(t *testing.T) {
	for _, tc := range []struct {
		method, path, body string
		status             int
	}{
		{"GET", "/api/equipment", "", 200},
		{"GET", "/api/equipment?includeRetired=true", "", 200},
		{"GET", "/api/equipment?includeRetired=yes", "", 400},
		{"GET", "/api/equipment?includeRetired=true&includeRetired=false", "", 400},
		{"GET", "/api/equipment?other=1", "", 400},
		{"PUT", "/api/inventory/2/equipment", `{"equipo_id":1}`, 200},
		{"PUT", "/api/inventory/2/equipment", `{"equipo_id":0}`, 200},
		{"PUT", "/api/inventory/2/equipment", `{}`, 400},
		{"PUT", "/api/inventory/2/equipment", `{"equipo_id":"1"}`, 400},
		{"PUT", "/api/inventory/2/equipment", `{"equipo_id":1,"extra":1}`, 400},
		{"PUT", "/api/inventory/no/equipment", `{"equipo_id":1}`, 400},
		{"POST", "/api/equipment", `{}`, 405},
	} {
		t.Run(tc.method+tc.path+tc.body, func(t *testing.T) {
			f := &fakeService{}
			r := do(newTestServer(f), tc.method, tc.path, tc.body, jsonType)
			if r.Code != tc.status {
				t.Fatalf("status=%d body=%s", r.Code, r.Body.String())
			}
		})
	}
	for _, problem := range []error{services.ErrInvalidItem, notFound, errors.New("secret storage path")} {
		f := &fakeService{err: problem}
		r := do(newTestServer(f), http.MethodPut, "/api/inventory/2/equipment", `{"equipo_id":1}`, jsonType)
		if r.Code == 200 || strings.Contains(r.Body.String(), "secret storage") {
			t.Fatal(r.Code, r.Body.String())
		}
	}
}
