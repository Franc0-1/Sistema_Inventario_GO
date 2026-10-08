package handlers

import "net/http"

func (h *InventoryHandler) equipments(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	for key, values := range q {
		if key != "includeRetired" || len(values) != 1 || (values[0] != "true" && values[0] != "false") {
			writeError(w, http.StatusBadRequest, CodeInvalidRequest, "solo se admite includeRetired=true o false")
			return
		}
	}
	result, err := h.equipment.Equipments(r.Context(), q.Get("includeRetired") == "true")
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, nonNil(result))
}

func (h *InventoryHandler) setEquipment(w http.ResponseWriter, r *http.Request) {
	id, reqErr := pathID(r)
	if reqErr != nil {
		writeRequestError(w, reqErr)
		return
	}
	var req struct {
		EquipmentID *int `json:"equipo_id"`
	}
	if reqErr := decodeJSON(w, r, &req); reqErr != nil {
		writeRequestError(w, reqErr)
		return
	}
	if req.EquipmentID == nil {
		writeError(w, http.StatusBadRequest, CodeInvalidRequest, "falta equipo_id; use 0 para desvincular")
		return
	}
	item, err := h.equipment.SetEquipment(r.Context(), id, *req.EquipmentID)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, item)
}
