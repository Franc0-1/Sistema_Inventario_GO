// Package services contiene las reglas de negocio del inventario. Depende
// solo de models y de las interfaces de repository: desconoce Excel, archivos
// y HTTP.
package services

import (
	"context"
	"io"

	"inventario/internal/models"
	"inventario/internal/repository"
)

// Repository son las capacidades de persistencia que usa el servicio.
type Repository interface {
	repository.InventoryRepository
	repository.MovementRepository
	repository.HealthChecker
}

// InventoryService son las operaciones que usarán los handlers.
//
// Errores: los de negocio de este paquete (ErrInvalidItem, ErrInvalidID,
// ErrInventoryNumberExists…) y los del repositorio sin reemplazar
// (repository.ErrItemNotFound, repository.ErrConflict…). Todos se
// identifican con errors.Is.
type InventoryService interface {
	Equipments(context.Context, bool) ([]models.Equipment, error)
	SetEquipment(context.Context, int, int) (models.Item, error)
	Summary(ctx context.Context) (models.InventorySummary, error)
	Report(ctx context.Context, filter models.ItemFilter) (models.InventoryReport, error)
	GetAll(ctx context.Context) ([]models.Item, error)
	GetByID(ctx context.Context, id int) (models.Item, error)
	Search(ctx context.Context, filter models.ItemFilter) ([]models.Item, error)

	// Query filtra, ordena y pagina el inventario. Devuelve ErrInvalidQuery
	// si el estado, la disponibilidad, el campo o el sentido de orden no están
	// en su lista blanca, o si la página o su tamaño están fuera de rango.
	Query(ctx context.Context, q models.ItemQuery) (models.ItemPage, error)

	// Create ignora ID, CreatedAt y UpdatedAt (los asigna el repositorio).
	// Un ítem nuevo nunca está prestado.
	Create(ctx context.Context, item models.Item) (models.Item, error)

	// Update reemplaza los datos del ítem id. item.ID debe ser 0 o igual a id.
	// Conserva CreatedAt y los datos de préstamo. Si cambia la cantidad,
	// registra un movimiento "adjustment" en la misma escritura.
	Update(ctx context.Context, id int, item models.Item) (models.Item, error)

	// Delete elimina el ítem definitivamente. Su historial se conserva.
	Delete(ctx context.Context, id int) error

	// UpdateStock fija la cantidad del ítem (no modifica ningún otro campo) y
	// registra en la misma escritura el movimiento stock_in, stock_out o
	// stock_update según la cantidad suba, baje o no cambie.
	UpdateStock(ctx context.Context, id, quantity int) (models.Item, error)

	// ApplyMovement registra una entrada, salida, ajuste, actualización o
	// transferencia (ver models.StockOperation) y aplica su efecto sobre el
	// ítem en la misma escritura. Errores: ErrInvalidMovement (tipo, cantidad
	// o destino inválidos), ErrInvalidQuantity, ErrInsufficientStock (salida
	// mayor al stock), repository.ErrItemNotFound y repository.ErrConflict.
	ApplyMovement(ctx context.Context, id int, op models.StockOperation) (models.Item, error)

	// GetMovements devuelve todo el historial, ordenado por ID.
	GetMovements(ctx context.Context) ([]models.Movement, error)

	// GetMovementsByItemID devuelve el historial de un ítem, aunque el ítem ya
	// se haya eliminado. ErrItemNotFound solo si el ítem no existe y no tiene historial.
	GetMovementsByItemID(ctx context.Context, itemID int) ([]models.Movement, error)

	// CheckHealth informa si el almacenamiento es accesible y su estructura es
	// válida. No escribe.
	CheckHealth(ctx context.Context) error

	// ExportInventory genera un Excel descargable con el inventario actual.
	ExportInventory(ctx context.Context) ([]byte, error)

	// ImportInventory reemplaza el inventario completo desde un .xlsx validado.
	ImportInventory(ctx context.Context, src io.Reader) (int, error)
}
