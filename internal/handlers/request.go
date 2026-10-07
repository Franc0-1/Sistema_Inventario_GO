package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"inventario/internal/models"
)

// maxBodyBytes limita el cuerpo JSON: un ítem ocupa menos de 1 KiB.
const maxBodyBytes = 64 << 10

// ============================================================
// DTOs de entrada
// ============================================================
//
// Solo exponen los campos que el cliente puede definir. ID, CreatedAt,
// UpdatedAt y los datos de préstamo no forman parte del DTO y, como el
// decodificador rechaza campos desconocidos, enviarlos es un error 400.
// Los nombres JSON son los mismos que los de las respuestas (models.Item).

// ItemRequest es el cuerpo de POST /api/inventory y PUT /api/inventory/{id}.
// Crear y actualizar reciben los mismos campos, por eso comparten el DTO.
type ItemRequest struct {
	InventoryNumber string            `json:"numero_inventario"`
	HasInventory    bool              `json:"tiene_inventario"`
	DeviceType      string            `json:"tipo_dispositivo"`
	Brand           string            `json:"marca"`
	Model           string            `json:"modelo"`
	SerialNumber    string            `json:"numero_serie"`
	Quantity        int               `json:"cantidad"`
	Location        string            `json:"ubicacion"`
	Status          models.ItemStatus `json:"estado"`
	Notes           string            `json:"observacion"`
}

type (
	CreateItemRequest = ItemRequest
	UpdateItemRequest = ItemRequest
)

// toItem copia los campos tal cual: normalizar y validar es tarea del servicio.
func (r ItemRequest) toItem() models.Item {
	return models.Item{
		InventoryNumber: r.InventoryNumber,
		HasInventory:    r.HasInventory,
		DeviceType:      r.DeviceType,
		Brand:           r.Brand,
		Model:           r.Model,
		SerialNumber:    r.SerialNumber,
		Quantity:        r.Quantity,
		Location:        r.Location,
		Status:          r.Status,
		Notes:           r.Notes,
	}
}

// UpdateStockRequest es el cuerpo de PATCH /api/inventory/{id}/stock.
// Quantity es puntero para distinguir "no enviado" de 0 (un stock válido).
//
// Sin Type fija la cantidad ({"cantidad": 10}, como desde la Etapa 5). Con Type
// registra ese movimiento (ver models.StockOperation):
//
//	{"tipo": "stock_in",  "cantidad": 5}             suma 5
//	{"tipo": "stock_out", "cantidad": 3}             resta 3 (no más que el stock)
//	{"tipo": "adjustment", "cantidad": 12}           corrige la cantidad a 12
//	{"tipo": "stock_update", "cantidad": 10}         igual que sin tipo
//	{"tipo": "transfer", "ubicacion_destino": "X"}   cambia la ubicación, sin cantidad
//
// "observacion" es opcional en todos los casos.
type UpdateStockRequest struct {
	Quantity            *int                `json:"cantidad"`
	Type                models.MovementType `json:"tipo"`
	DestinationLocation string              `json:"ubicacion_destino"`
	Notes               string              `json:"observacion"`
}

// toOperation exige cantidad salvo en una transferencia, donde la prohíbe.
// Que el tipo exista y los valores sean válidos lo decide el servicio.
func (r UpdateStockRequest) toOperation() (models.StockOperation, *requestError) {
	op := models.StockOperation{Type: r.Type, DestinationLocation: r.DestinationLocation, Notes: r.Notes}
	if op.Type == "" {
		op.Type = models.MovementStockUpdate
	}
	switch {
	case op.Type == models.MovementTransfer && r.Quantity != nil:
		return op, badRequest(CodeInvalidRequest, "una transferencia no lleva cantidad")
	case op.Type != models.MovementTransfer && r.Quantity == nil:
		return op, badRequest(CodeInvalidRequest, "falta el campo cantidad")
	case r.Quantity != nil:
		op.Quantity = *r.Quantity
	}
	return op, nil
}

// ============================================================
// Lectura de la petición
// ============================================================

// requestError es un problema de formato de la petición (no de negocio).
type requestError struct {
	status  int
	code    string
	message string
}

func badRequest(code, format string, args ...any) *requestError {
	return &requestError{status: http.StatusBadRequest, code: code, message: fmt.Sprintf(format, args...)}
}

func writeRequestError(w http.ResponseWriter, e *requestError) {
	writeError(w, e.status, e.code, e.message)
}

// pathID lee {id} de la URL. Solo verifica que sea un entero; si es positivo
// lo decide el servicio (ErrInvalidID).
func pathID(r *http.Request) (int, *requestError) {
	raw := r.PathValue("id")
	id, err := strconv.Atoi(raw)
	if err != nil {
		return 0, badRequest(CodeInvalidID, "el ID debe ser un número entero (%q)", raw)
	}
	return id, nil
}

// decodeJSON exige Content-Type application/json, limita el tamaño y
// decodifica un único objeto rechazando campos desconocidos.
func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) *requestError {
	if !isJSON(r.Header.Get("Content-Type")) {
		return &requestError{http.StatusUnsupportedMediaType, CodeUnsupportedMediaType, "se esperaba Content-Type: application/json"}
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()

	if err := dec.Decode(dst); err != nil {
		return jsonError(err)
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if tooLarge(err) {
			return jsonError(err)
		}
		return badRequest(CodeInvalidJSON, "el cuerpo debe contener un único objeto JSON")
	}
	return nil
}

func isJSON(contentType string) bool {
	mediaType, _, err := mime.ParseMediaType(contentType)
	return err == nil && mediaType == "application/json"
}

func tooLarge(err error) bool {
	var maxErr *http.MaxBytesError
	return errors.As(err, &maxErr)
}

// jsonError traduce los errores de encoding/json a mensajes útiles para el
// frontend, sin exponer detalles del servidor.
func jsonError(err error) *requestError {
	var (
		syntaxErr *json.SyntaxError
		typeErr   *json.UnmarshalTypeError
	)
	switch {
	case tooLarge(err):
		return &requestError{http.StatusRequestEntityTooLarge, CodeRequestTooLarge,
			fmt.Sprintf("el cuerpo supera el máximo de %d bytes", maxBodyBytes)}
	case errors.Is(err, io.EOF):
		return badRequest(CodeInvalidJSON, "el cuerpo está vacío")
	case errors.As(err, &syntaxErr):
		return badRequest(CodeInvalidJSON, "JSON mal formado en la posición %d", syntaxErr.Offset)
	case errors.Is(err, io.ErrUnexpectedEOF):
		return badRequest(CodeInvalidJSON, "JSON incompleto")
	case errors.As(err, &typeErr):
		return badRequest(CodeInvalidJSON, "el campo %q tiene un tipo inválido (se esperaba %s)", typeErr.Field, typeErr.Type)
	case strings.HasPrefix(err.Error(), "json: unknown field "):
		return badRequest(CodeInvalidJSON, "campo no permitido: %s", strings.TrimPrefix(err.Error(), "json: unknown field "))
	}
	return badRequest(CodeInvalidJSON, "JSON inválido")
}

// ============================================================
// Parámetros del listado (GET /api/inventory y /search)
// ============================================================

// queryParams son los únicos parámetros aceptados. Un parámetro desconocido
// o repetido es un 400: así un error de tipeo ("Brand=") no se ignora en
// silencio devolviendo resultados sin filtrar.
var queryParams = map[string]bool{
	"q": true, "deviceType": true, "excludeDeviceType": true, "brand": true, "model": true,
	"inventoryNumber": true, "serialNumber": true, "location": true, "status": true,
	"availability": true, "hasInventory": true, "includeRetired": true,
	"sort": true, "order": true, "page": true, "pageSize": true,
}

// parseItemQuery convierte la query string en models.ItemQuery. Solo valida
// el formato (enteros, booleanos); las listas blancas de estado y orden, los
// rangos y los valores por defecto son del servicio.
func parseItemQuery(values url.Values) (models.ItemQuery, *requestError) {
	for name, list := range values {
		if !queryParams[name] {
			return models.ItemQuery{}, badRequest(CodeInvalidQuery, "parámetro desconocido: %q", name)
		}
		if len(list) > 1 {
			return models.ItemQuery{}, badRequest(CodeInvalidQuery, "el parámetro %q está repetido", name)
		}
	}
	get := values.Get
	q := models.ItemQuery{
		Filter: models.ItemFilter{
			Text:              get("q"),
			DeviceType:        get("deviceType"),
			ExcludeDeviceType: get("excludeDeviceType"),
			Brand:             get("brand"),
			Model:             get("model"),
			InventoryNumber:   get("inventoryNumber"),
			SerialNumber:      get("serialNumber"),
			Location:          get("location"),
			Status:            models.ItemStatus(strings.TrimSpace(get("status"))),
			Availability:      models.Availability(strings.TrimSpace(get("availability"))),
		},
		Sort:  models.SortField(strings.TrimSpace(get("sort"))),
		Order: models.SortOrder(strings.TrimSpace(get("order"))),
	}

	var reqErr *requestError
	if q.Filter.HasInventory, reqErr = optionalBool(values, "hasInventory"); reqErr != nil {
		return q, reqErr
	}
	includeRetired, reqErr := optionalBool(values, "includeRetired")
	if reqErr != nil {
		return q, reqErr
	}
	q.Filter.IncludeRetired = includeRetired != nil && *includeRetired
	if q.Page, reqErr = positiveInt(values, "page"); reqErr != nil {
		return q, reqErr
	}
	if q.PageSize, reqErr = positiveInt(values, "pageSize"); reqErr != nil {
		return q, reqErr
	}
	return q, nil
}

// optionalBool acepta "true" o "false"; ausente o vacío es nil.
func optionalBool(values url.Values, name string) (*bool, *requestError) {
	switch raw := strings.ToLower(strings.TrimSpace(values.Get(name))); raw {
	case "":
		return nil, nil
	case "true", "false":
		b := raw == "true"
		return &b, nil
	}
	return nil, badRequest(CodeInvalidQuery, "%s debe ser true o false", name)
}

// positiveInt acepta un entero >= 1; ausente o vacío es 0 (valor por defecto).
func positiveInt(values url.Values, name string) (int, *requestError) {
	raw := strings.TrimSpace(values.Get(name))
	if raw == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 {
		return 0, badRequest(CodeInvalidQuery, "%s debe ser un número entero mayor o igual a 1", name)
	}
	return n, nil
}
