package models

// SortField es un campo por el que se puede ordenar el inventario. Es una
// lista blanca: el servicio rechaza cualquier otro valor.
type SortField string

const (
	SortByID              SortField = "id"
	SortByInventoryNumber SortField = "inventoryNumber"
	SortByDeviceType      SortField = "deviceType"
	SortByBrand           SortField = "brand"
	SortByModel           SortField = "model"
	SortByQuantity        SortField = "quantity"
	SortByLocation        SortField = "location"
	SortByStatus          SortField = "status"
	SortByCreatedAt       SortField = "createdAt"
	SortByUpdatedAt       SortField = "updatedAt"
)

// SortFields son los únicos campos de ordenamiento aceptados.
var SortFields = []SortField{
	SortByID, SortByInventoryNumber, SortByDeviceType, SortByBrand, SortByModel,
	SortByQuantity, SortByLocation, SortByStatus, SortByCreatedAt, SortByUpdatedAt,
}

type SortOrder string

const (
	Ascending  SortOrder = "asc"
	Descending SortOrder = "desc"
)

const (
	DefaultPageSize = 20
	MaxPageSize     = 100
)

// ItemQuery es una consulta del listado: filtro, orden y página.
type ItemQuery struct {
	Filter ItemFilter
	Sort   SortField // vacío: SortByID
	Order  SortOrder // vacío: Ascending

	// Page y PageSize en 0 devuelven todos los resultados en una sola página.
	// Si se indica uno solo, el otro toma su valor por defecto (1 y DefaultPageSize).
	Page     int
	PageSize int
}

// Pagination describe la página devuelta. TotalPages es 0 solo si no hay
// resultados. Una página posterior a la última no es un error: llega vacía.
type Pagination struct {
	Page       int `json:"page"`
	PageSize   int `json:"pageSize"`
	TotalItems int `json:"totalItems"`
	TotalPages int `json:"totalPages"`
}

// ItemPage es el resultado de una ItemQuery.
type ItemPage struct {
	Items      []Item     `json:"items"`
	Pagination Pagination `json:"pagination"`
}
