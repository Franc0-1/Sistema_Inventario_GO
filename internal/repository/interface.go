// Package repository define los contratos de persistencia y su
// implementación sobre Excel. Los servicios dependen SOLO de estas
// interfaces; nunca de excelize.
//
// El repositorio no contiene reglas de negocio: guarda y recupera datos.
// Solo garantiza integridad de almacenamiento, lo mismo que haría una base de
// datos: IDs únicos, escrituras atómicas y control de versión.
package repository

import (
	"context"
	"io"
	"time"

	"inventario/internal/models"
)

// InventoryRepository gestiona la hoja "Inventario".
//
// Reglas comunes a las escrituras:
//   - El repositorio completa ID, CreatedAt y UpdatedAt; los valores que
//     traiga el ítem en esos campos se ignoran.
//   - Los IDs nunca se reutilizan, aunque se borre el último.
//   - Update, Delete y UpdateStock reciben expectedVersion: el UpdatedAt que
//     el llamador leyó. Si lo guardado ya no coincide, otra operación escribió
//     en el medio y devuelven ErrConflict sin guardar (concurrencia optimista).
type InventoryRepository interface {
	// GetAll devuelve todos los ítems en el orden de la hoja, incluidas las bajas.
	GetAll(ctx context.Context) ([]models.Item, error)

	// GetByID devuelve ErrItemNotFound si el ID no existe.
	GetByID(ctx context.Context, id int) (models.Item, error)

	// Search aplica el filtro como una consulta (equivale a un WHERE).
	// Sin IncludeRetired ni Status, excluye los ítems con Status BAJA.
	// Devuelve los ítems en el orden de la hoja; ordenar y paginar es tarea
	// del servicio.
	Search(ctx context.Context, filter models.ItemFilter) ([]models.Item, error)

	// Create agrega el ítem al final de la hoja y lo devuelve con ID y fechas.
	Create(ctx context.Context, item models.Item) (models.Item, error)

	// Update reescribe en su misma fila todos los campos del ítem item.ID,
	// salvo ID y CreatedAt. Devuelve el ítem con el nuevo UpdatedAt.
	// Si mov no es nil, se agrega a "Movimientos" en el MISMO guardado.
	Update(ctx context.Context, item models.Item, expectedVersion time.Time, mov *models.Movement) (models.Item, error)

	// Delete elimina la fila del ítem. Sus movimientos se conservan.
	// La baja lógica (Status = BAJA) se hace con Update.
	Delete(ctx context.Context, id int, expectedVersion time.Time) error

	// UpdateStock cambia solo Quantity y UpdatedAt del ítem y agrega mov a
	// "Movimientos" en el MISMO guardado: nunca queda uno sin el otro.
	// Devuelve el ítem actualizado.
	UpdateStock(ctx context.Context, id, quantity int, expectedVersion time.Time, mov models.Movement) (models.Item, error)

	// ExportInventory genera un .xlsx con la hoja Inventario actual. No modifica
	// el archivo real.
	ExportInventory(ctx context.Context) ([]byte, error)

	// ImportInventory reemplaza atómicamente la hoja Inventario con los datos de
	// un .xlsx validado. La hoja Movimientos existente se conserva.
	ImportInventory(ctx context.Context, src io.Reader) (int, error)
}

// MovementRepository gestiona la hoja "Movimientos" (solo inserción y lectura).
//
// Al guardar un movimiento el repositorio completa ID (sin reutilizar) y
// CreatedAt. Cuando el movimiento acompaña a Update o UpdateStock, además
// completa ItemID, PreviousQuantity y NewQuantity con los valores realmente
// escritos, para que el historial nunca contradiga a "Inventario".
type MovementRepository interface {
	// GetMovements devuelve todos los movimientos ordenados por ID ascendente.
	GetMovements(ctx context.Context) ([]models.Movement, error)

	// GetMovementsByItemID devuelve los movimientos de un ítem (aunque el ítem
	// ya no exista), ordenados por ID ascendente.
	GetMovementsByItemID(ctx context.Context, itemID int) ([]models.Movement, error)

	// CreateMovement agrega un movimiento suelto y lo devuelve con ID y fecha.
	CreateMovement(ctx context.Context, mov models.Movement) (models.Movement, error)
}

// CategoryRepository lee los tipos de dispositivo de la hoja "Categorias".
type CategoryRepository interface {
	ListCategories(ctx context.Context) ([]models.Category, error)
}

// HealthChecker informa si el almacenamiento es accesible y su estructura es
// válida, sin escribir. Devuelve ErrInvalidWorkbook o ErrSheetMissing si no.
type HealthChecker interface {
	Check(ctx context.Context) error
}

// Repository agrupa todos los contratos. Es lo que recibe la capa de servicios.
type Repository interface {
	InventoryRepository
	MovementRepository
	CategoryRepository
	HealthChecker
	Close() error
}
