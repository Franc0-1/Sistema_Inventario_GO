package services

import (
	"errors"
	"fmt"
)

// Errores de negocio. Los errores del repositorio (ErrItemNotFound,
// ErrConflict, ErrInvalidWorkbook…) no se duplican: se devuelven tal cual.
var (
	// ErrInvalidItem agrupa los datos inválidos; ErrInventoryNumberRequired y
	// ErrInvalidQuantity lo envuelven, así errors.Is(err, ErrInvalidItem)
	// basta para responder "datos inválidos".
	ErrInvalidItem             = errors.New("ítem inválido")
	ErrInventoryNumberRequired = fmt.Errorf("%w: el número de inventario es obligatorio si el ítem tiene inventario", ErrInvalidItem)
	ErrInvalidQuantity         = fmt.Errorf("%w: la cantidad no puede ser negativa", ErrInvalidItem)

	// ErrInvalidMovement: tipo desconocido o cantidades incoherentes con el tipo.
	ErrInvalidMovement = errors.New("movimiento inválido")

	ErrInvalidID             = errors.New("ID inválido")
	ErrInventoryNumberExists = errors.New("el número de inventario ya existe")
	ErrSerialNumberExists    = errors.New("el número de serie ya existe")
)

// ErrInvalidQuery: filtro, orden o paginación fuera de las listas blancas o
// de los rangos permitidos.
var ErrInvalidQuery = errors.New("consulta inválida")

// ErrInsufficientStock: una salida mayor al stock disponible.
var ErrInsufficientStock = errors.New("stock insuficiente")
