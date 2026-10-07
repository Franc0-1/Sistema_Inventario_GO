package handlers

import (
	"errors"
	"net/http"
	"strings"
	"testing"
)

func TestAnalytics_EndpointsYParametros(t *testing.T) {
	svc := &fakeService{}
	server := newTestServer(svc)
	if rec := do(server, "GET", "/api/inventory/summary", "", ""); rec.Code != http.StatusOK || !svc.called("Summary") {
		t.Fatal(rec.Body.String())
	}
	if rec := do(server, "GET", "/api/inventory/reports?location=Sistemas&deviceType=Mouse&includeRetired=true", "", ""); rec.Code != http.StatusOK || svc.gotFilter.Location != "Sistemas" || svc.gotFilter.DeviceType != "Mouse" || !svc.gotFilter.IncludeRetired {
		t.Fatal(rec.Body.String())
	}
	for _, url := range []string{"/api/inventory/summary?page=1", "/api/inventory/reports?page=1", "/api/inventory/reports?includeRetired=si", "/api/inventory/reports?location=A&location=B"} {
		if rec := do(server, "GET", url, "", ""); rec.Code != http.StatusBadRequest {
			t.Fatalf("%s: %d", url, rec.Code)
		}
	}
	for _, url := range []string{"/api/inventory/summary", "/api/inventory/reports"} {
		if rec := do(server, "POST", url, "", ""); rec.Code != http.StatusMethodNotAllowed {
			t.Fatal(rec.Code)
		}
		svc.err = errors.New("detalle secreto")
		rec := do(server, "GET", url, "", "")
		if rec.Code != http.StatusInternalServerError || strings.Contains(rec.Body.String(), "detalle secreto") {
			t.Fatal(rec.Body.String())
		}
	}
}
