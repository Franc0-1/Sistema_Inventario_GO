package handlers

import "net/http"

func (h *InventoryHandler) summary(w http.ResponseWriter, r *http.Request) {
	if len(r.URL.Query()) != 0 {
		writeRequestError(w, badRequest(CodeInvalidQuery, "el resumen no acepta parámetros"))
		return
	}
	data, err := h.reports.Summary(r.Context())
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, data)
}

func (h *InventoryHandler) report(w http.ResponseWriter, r *http.Request) {
	values := r.URL.Query()
	for name := range values {
		if name != "location" && name != "deviceType" && name != "includeRetired" {
			writeRequestError(w, badRequest(CodeInvalidQuery, "parámetro desconocido: %q", name))
			return
		}
	}
	query, problem := parseItemQuery(values)
	if problem != nil {
		writeRequestError(w, problem)
		return
	}
	data, err := h.reports.Report(r.Context(), query.Filter)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, data)
}
