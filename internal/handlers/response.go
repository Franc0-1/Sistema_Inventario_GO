package handlers

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"

	"inventario/internal/repository"
	"inventario/internal/services"
)

// Formato de respuesta de la API:
//
//	éxito: {"data": ...}
//	error: {"error": {"code": "ITEM_NOT_FOUND", "message": "..."}}

// Códigos de error de la API. Son estables: el frontend puede decidir por
// ellos; el mensaje es solo para mostrar.
const (
	CodeItemNotFound            = "ITEM_NOT_FOUND"
	CodeInvalidRequest          = "INVALID_REQUEST"
	CodeInvalidID               = "INVALID_ID"
	CodeInvalidItem             = "INVALID_ITEM"
	CodeInventoryNumberRequired = "INVENTORY_NUMBER_REQUIRED"
	CodeInventoryNumberExists   = "INVENTORY_NUMBER_EXISTS"
	CodeSerialNumberExists      = "SERIAL_NUMBER_EXISTS"
	CodeInvalidQuantity         = "INVALID_QUANTITY"
	CodeInsufficientStock       = "INSUFFICIENT_STOCK"
	CodeInvalidMovement         = "INVALID_MOVEMENT"
	CodeInvalidQuery            = "INVALID_QUERY"
	CodeInvalidJSON             = "INVALID_JSON"
	CodeUnsupportedMediaType    = "UNSUPPORTED_MEDIA_TYPE"
	CodeRequestTooLarge         = "REQUEST_TOO_LARGE"
	CodeConflict                = "CONFLICT"
	CodeMethodNotAllowed        = "METHOD_NOT_ALLOWED"
	CodeNotFound                = "NOT_FOUND"
	CodeInternal                = "INTERNAL_ERROR"
	CodeServiceUnavailable      = "SERVICE_UNAVAILABLE"

	// Modelo nuevo (catálogos, equipos…).
	CodeInvalidData   = "INVALID_DATA"
	CodeInvalidUser   = "INVALID_USER"
	CodeAlreadyExists = "ALREADY_EXISTS"
	CodeInUse         = "IN_USE"
	CodeInvalidState  = "INVALID_STATE"
)

type dataResponse struct {
	Data any `json:"data"`
}

type errorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type errorResponse struct {
	Error errorBody `json:"error"`
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		log.Printf("no se pudo escribir la respuesta: %v", err)
	}
}

func writeData(w http.ResponseWriter, status int, data any) {
	writeJSON(w, status, dataResponse{Data: data})
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, errorResponse{Error: errorBody{Code: code, Message: message}})
}

// errorMappings traduce errores de service/repository a HTTP. El orden
// importa: ErrInventoryNumberRequired y ErrInvalidQuantity envuelven
// ErrInvalidItem y deben evaluarse antes.
var errorMappings = []struct {
	err    error
	status int
	code   string
}{
	{services.ErrInvalidID, http.StatusBadRequest, CodeInvalidID},
	{services.ErrInventoryNumberRequired, http.StatusBadRequest, CodeInventoryNumberRequired},
	{services.ErrInvalidQuantity, http.StatusBadRequest, CodeInvalidQuantity},
	{services.ErrInvalidItem, http.StatusBadRequest, CodeInvalidItem},
	{services.ErrInvalidMovement, http.StatusBadRequest, CodeInvalidMovement},
	{services.ErrInvalidQuery, http.StatusBadRequest, CodeInvalidQuery},
	{repository.ErrInvalidMovement, http.StatusBadRequest, CodeInvalidMovement},
	{services.ErrInventoryNumberExists, http.StatusConflict, CodeInventoryNumberExists},
	{services.ErrSerialNumberExists, http.StatusConflict, CodeSerialNumberExists},
	{services.ErrInsufficientStock, http.StatusConflict, CodeInsufficientStock},
	{repository.ErrItemNotFound, http.StatusNotFound, CodeItemNotFound},
	{repository.ErrConflict, http.StatusConflict, CodeConflict},
	{repository.ErrInvalidItem, http.StatusBadRequest, CodeInvalidItem},
	{services.ErrDatoInvalido, http.StatusBadRequest, CodeInvalidData},
	{services.ErrUsuarioInvalido, http.StatusBadRequest, CodeInvalidUser},
	{repository.ErrNoEncontrado, http.StatusNotFound, CodeNotFound},
	{repository.ErrDuplicado, http.StatusConflict, CodeAlreadyExists},
	{repository.ErrEnUso, http.StatusConflict, CodeInUse},
	{repository.ErrStockInsuficiente, http.StatusConflict, CodeInsufficientStock},
	{repository.ErrEstadoInvalido, http.StatusConflict, CodeInvalidState},
}

// writeServiceError responde un error devuelto por el servicio. Los errores
// conocidos llevan su propio mensaje (son de negocio, sin detalles internos);
// cualquier otro es un 500 genérico y el detalle queda solo en el log.
func writeServiceError(w http.ResponseWriter, r *http.Request, err error) {
	for _, m := range errorMappings {
		if errors.Is(err, m.err) {
			writeError(w, m.status, m.code, err.Error())
			return
		}
	}
	log.Printf("error interno en %s %s: %v", r.Method, r.URL.Path, err)
	writeError(w, http.StatusInternalServerError, CodeInternal, "error interno del servidor")
}
