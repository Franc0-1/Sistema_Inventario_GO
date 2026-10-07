package models

import "time"

// MovementType clasifica cada entrada del historial.
type MovementType string

const (
	MovementStockIn     MovementType = "stock_in"     // el stock aumentó
	MovementStockOut    MovementType = "stock_out"    // el stock disminuyó
	MovementStockUpdate MovementType = "stock_update" // se registró el stock sin cambio de cantidad
	MovementTransfer    MovementType = "transfer"     // cambio de ubicación
	MovementAdjustment  MovementType = "adjustment"   // corrección de cantidad al editar el ítem
)

// ValidMovementTypes lista los tipos aceptados por la capa de servicios.
var ValidMovementTypes = []MovementType{
	MovementStockIn, MovementStockOut, MovementStockUpdate, MovementTransfer, MovementAdjustment,
}

// Movement es una fila de la hoja "Movimientos". Es inmutable: el historial
// solo crece, y sobrevive a la eliminación del ítem.
//
// Quantity es la magnitud del cambio (siempre ≥ 0); la dirección la indica
// MovementType. Ejemplo: 10 → 15 es stock_in con Quantity 5.
type Movement struct {
	ID                  int          `json:"id"`
	ItemID              int          `json:"item_id"`
	MovementType        MovementType `json:"tipo"`
	Quantity            int          `json:"cantidad"`
	PreviousQuantity    int          `json:"cantidad_anterior"`
	NewQuantity         int          `json:"cantidad_nueva"`
	OriginLocation      string       `json:"ubicacion_origen"`
	DestinationLocation string       `json:"ubicacion_destino"`
	Notes               string       `json:"observacion"`
	CreatedAt           time.Time    `json:"created_at"`
}

// StockOperation es un movimiento pedido por el usuario sobre un ítem. El
// servicio calcula las cantidades anterior y nueva a partir del stock real.
//
// Significado de Quantity según Type:
//   - MovementStockIn / MovementStockOut: unidades que entran o salen (≥ 1).
//   - MovementAdjustment: cantidad correcta tras un conteo (≥ 0), se registra como ajuste.
//   - MovementStockUpdate: cantidad final (≥ 0); se registra como stock_in,
//     stock_out o stock_update según suba, baje o no cambie (equivale a UpdateStock).
//   - MovementTransfer: no se usa (debe ser 0); la cantidad del ítem no cambia y
//     el movimiento registra todas sus unidades como trasladadas.
type StockOperation struct {
	Type                MovementType
	Quantity            int
	DestinationLocation string // solo MovementTransfer
	Notes               string
}
