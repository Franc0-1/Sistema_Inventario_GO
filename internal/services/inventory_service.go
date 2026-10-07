package services

import (
	"context"
	"fmt"
	"io"
	"sync"
	"time"

	"inventario/internal/models"
)

type inventoryService struct {
	repo Repository

	// writeMu hace atómico "verificar unicidad + guardar" dentro del proceso:
	// sin él, dos altas simultáneas con el mismo número de inventario pasarían
	// ambas la verificación.
	writeMu sync.Mutex
}

// NewInventoryService recibe el repositorio por inyección: en producción el
// de Excel, en los tests un fake.
func NewInventoryService(repo Repository) InventoryService {
	return &inventoryService{repo: repo}
}

// ---- Consultas ----

func (s *inventoryService) GetAll(ctx context.Context) ([]models.Item, error) {
	return s.repo.GetAll(ctx)
}

func (s *inventoryService) GetByID(ctx context.Context, id int) (models.Item, error) {
	if err := validateID(id); err != nil {
		return models.Item{}, err
	}
	return s.repo.GetByID(ctx, id)
}

func (s *inventoryService) Search(ctx context.Context, filter models.ItemFilter) ([]models.Item, error) {
	return s.repo.Search(ctx, filter)
}

// ---- Comandos ----

func (s *inventoryService) Create(ctx context.Context, item models.Item) (models.Item, error) {
	item = asNewItem(normalizeItem(item))
	if err := validateItem(item); err != nil {
		return models.Item{}, err
	}

	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if err := s.checkUniqueness(ctx, item); err != nil {
		return models.Item{}, err
	}
	return s.repo.Create(ctx, item)
}

func (s *inventoryService) Update(ctx context.Context, id int, item models.Item) (models.Item, error) {
	if err := validateID(id); err != nil {
		return models.Item{}, err
	}
	if item.ID != 0 && item.ID != id {
		return models.Item{}, fmt.Errorf("%w: no se puede cambiar el ID %d por %d", ErrInvalidID, id, item.ID)
	}

	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	current, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return models.Item{}, err
	}

	item = keepManagedFields(normalizeItem(item), current)
	if err := validateItem(item); err != nil {
		return models.Item{}, err
	}
	if err := s.checkUniqueness(ctx, item); err != nil {
		return models.Item{}, err
	}

	// Un cambio de cantidad desde la edición también queda en el historial.
	var mov *models.Movement
	if item.Quantity != current.Quantity {
		adjustment := adjustmentMovement(id, current.Quantity, item.Quantity)
		if err := validateMovement(adjustment); err != nil {
			return models.Item{}, err
		}
		mov = &adjustment
	}
	return s.repo.Update(ctx, item, current.UpdatedAt, mov)
}

func (s *inventoryService) Delete(ctx context.Context, id int) error {
	if err := validateID(id); err != nil {
		return err
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	current, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return err
	}
	items, err := s.repo.GetAll(ctx)
	if err != nil {
		return err
	}
	for _, item := range items {
		if item.EquipmentID == id {
			return fmt.Errorf("%w: desvincule los componentes antes de eliminar el gabinete", ErrInvalidItem)
		}
	}
	return s.repo.Delete(ctx, id, current.UpdatedAt)
}

// UpdateStock fija la cantidad: es ApplyMovement con stock_update.
func (s *inventoryService) UpdateStock(ctx context.Context, id, quantity int) (models.Item, error) {
	return s.ApplyMovement(ctx, id, models.StockOperation{Type: models.MovementStockUpdate, Quantity: quantity})
}

func (s *inventoryService) CheckHealth(ctx context.Context) error {
	return s.repo.Check(ctx)
}

func (s *inventoryService) ExportInventory(ctx context.Context) ([]byte, error) {
	return s.repo.ExportInventory(ctx)
}

func (s *inventoryService) ImportInventory(ctx context.Context, src io.Reader) (int, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	return s.repo.ImportInventory(ctx, src)
}

// ---- Historial ----

func (s *inventoryService) GetMovements(ctx context.Context) ([]models.Movement, error) {
	return s.repo.GetMovements(ctx)
}

func (s *inventoryService) GetMovementsByItemID(ctx context.Context, itemID int) ([]models.Movement, error) {
	if err := validateID(itemID); err != nil {
		return nil, err
	}
	movs, err := s.repo.GetMovementsByItemID(ctx, itemID)
	if err != nil || len(movs) > 0 {
		return movs, err
	}
	// Sin historial: es un ítem sin movimientos (lista vacía) o un ID que no
	// existe. Un ítem eliminado con historial ya devolvió sus movimientos arriba.
	if _, err := s.repo.GetByID(ctx, itemID); err != nil {
		return nil, err
	}
	return movs, nil
}

// ---- Helpers ----

func (s *inventoryService) checkUniqueness(ctx context.Context, item models.Item) error {
	existing, err := s.repo.GetAll(ctx)
	if err != nil {
		return err
	}
	if err := checkUniqueness(existing, item); err != nil {
		return err
	}
	return validateEquipmentChange(existing, item)
}

// asNewItem descarta lo que el cliente no puede definir en un alta: el ID y
// las fechas los asigna el repositorio, y un ítem nuevo nunca está prestado.
func asNewItem(item models.Item) models.Item {
	item.ID = 0
	item.CreatedAt, item.UpdatedAt = time.Time{}, time.Time{}
	item.Availability, item.AssignedTo, item.LoanedAt = models.Available, "", nil
	return item
}

// keepManagedFields conserva lo que Update no puede cambiar: el ID, CreatedAt
// y los datos de préstamo (se modifican solo con las operaciones de préstamo;
// el formulario de edición no los envía y los borraría).
func keepManagedFields(item, current models.Item) models.Item {
	item.ID, item.CreatedAt = current.ID, current.CreatedAt
	item.Availability, item.AssignedTo, item.LoanedAt = current.Availability, current.AssignedTo, current.LoanedAt
	item.EquipmentID = current.EquipmentID
	return item
}
