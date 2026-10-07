package services

import (
	"fmt"
	"slices"
	"strings"

	"inventario/internal/models"
)

// normalizeItem quita espacios al principio y al final de los textos. No
// cambia mayúsculas ni espacios internos: se conserva el formato visual.
func normalizeItem(item models.Item) models.Item {
	for _, field := range []*string{
		&item.InventoryNumber, &item.DeviceType, &item.Brand, &item.Model,
		&item.SerialNumber, &item.Location, &item.Notes, &item.AssignedTo,
	} {
		*field = strings.TrimSpace(*field)
	}
	item.Status = models.ItemStatus(strings.TrimSpace(string(item.Status)))
	return item
}

// validateItem verifica un ítem ya normalizado.
func validateItem(item models.Item) error {
	required := []struct{ value, name string }{
		{item.DeviceType, "el tipo de dispositivo"},
		{item.Brand, "la marca"},
		{item.Model, "el modelo"},
		{item.Location, "la ubicación"},
		{string(item.Status), "el estado"},
	}
	for _, r := range required {
		if r.value == "" {
			return fmt.Errorf("%w: falta %s", ErrInvalidItem, r.name)
		}
	}
	if !slices.Contains(models.ValidStatuses, item.Status) {
		return fmt.Errorf("%w: estado %q desconocido (válidos: %v)", ErrInvalidItem, item.Status, models.ValidStatuses)
	}
	if err := validateQuantity(item.Quantity); err != nil {
		return err
	}
	return validateInventoryNumber(item)
}

// validateInventoryNumber exige coherencia entre HasInventory e InventoryNumber.
func validateInventoryNumber(item models.Item) error {
	switch {
	case item.HasInventory && item.InventoryNumber == "":
		return ErrInventoryNumberRequired
	case !item.HasInventory && item.InventoryNumber != "":
		return fmt.Errorf("%w: un ítem sin inventario no lleva número de inventario (%q)", ErrInvalidItem, item.InventoryNumber)
	}
	return nil
}

func validateQuantity(quantity int) error {
	if quantity < 0 {
		return fmt.Errorf("%w (%d)", ErrInvalidQuantity, quantity)
	}
	return nil
}

func validateID(id int) error {
	if id <= 0 {
		return fmt.Errorf("%w: %d", ErrInvalidID, id)
	}
	return nil
}

// stockMovement arma el movimiento de un cambio de stock: el tipo sale de la
// dirección del cambio y Quantity es su magnitud (10 → 15: stock_in de 5).
func stockMovement(itemID, previous, next int) models.Movement {
	mov := models.Movement{ItemID: itemID, PreviousQuantity: previous, NewQuantity: next}
	switch {
	case next > previous:
		mov.MovementType, mov.Quantity = models.MovementStockIn, next-previous
	case next < previous:
		mov.MovementType, mov.Quantity = models.MovementStockOut, previous-next
	default:
		mov.MovementType = models.MovementStockUpdate
	}
	return mov
}

// adjustmentMovement registra un cambio de cantidad hecho al editar el ítem.
func adjustmentMovement(itemID, previous, next int) models.Movement {
	return models.Movement{ItemID: itemID, MovementType: models.MovementAdjustment,
		Quantity: abs(next - previous), PreviousQuantity: previous, NewQuantity: next}
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// validateMovement exige un tipo conocido y cantidades coherentes con él.
// Una cantidad nueva negativa es stock negativo: ErrInvalidQuantity.
func validateMovement(mov models.Movement) error {
	if err := validateID(mov.ItemID); err != nil {
		return err
	}
	if !slices.Contains(models.ValidMovementTypes, mov.MovementType) {
		return fmt.Errorf("%w: tipo %q desconocido (válidos: %v)", ErrInvalidMovement, mov.MovementType, models.ValidMovementTypes)
	}
	for _, q := range []int{mov.Quantity, mov.PreviousQuantity, mov.NewQuantity} {
		if err := validateQuantity(q); err != nil {
			return err
		}
	}
	if !movementIsCoherent(mov) {
		return fmt.Errorf("%w: %s con cantidad %d no lleva de %d a %d",
			ErrInvalidMovement, mov.MovementType, mov.Quantity, mov.PreviousQuantity, mov.NewQuantity)
	}
	return nil
}

func movementIsCoherent(mov models.Movement) bool {
	diff := mov.NewQuantity - mov.PreviousQuantity
	switch mov.MovementType {
	case models.MovementStockIn:
		return diff > 0 && mov.Quantity == diff
	case models.MovementStockOut:
		return diff < 0 && mov.Quantity == -diff
	case models.MovementStockUpdate:
		return diff == 0 && mov.Quantity == 0
	case models.MovementAdjustment:
		return mov.Quantity == abs(diff)
	case models.MovementTransfer:
		origin, destination := strings.TrimSpace(mov.OriginLocation), strings.TrimSpace(mov.DestinationLocation)
		// Traslada todas las unidades del ítem sin cambiar la cantidad.
		return diff == 0 && mov.Quantity == mov.PreviousQuantity &&
			origin != "" && destination != "" && !strings.EqualFold(origin, destination)
	}
	return false
}

// checkUniqueness verifica que InventoryNumber y SerialNumber (si no están
// vacíos) no pertenezcan a otro ítem. Compara sin distinguir mayúsculas e
// ignora el propio ítem (item.ID), para que un Update pueda conservarlos.
// Incluye los ítems dados de baja: un número no se reasigna nunca.
func checkUniqueness(existing []models.Item, item models.Item) error {
	for _, other := range existing {
		if other.ID == item.ID {
			continue
		}
		if sameIdentifier(item.InventoryNumber, other.InventoryNumber) {
			return fmt.Errorf("%w: %q (ítem %d)", ErrInventoryNumberExists, item.InventoryNumber, other.ID)
		}
		if sameIdentifier(item.SerialNumber, other.SerialNumber) {
			return fmt.Errorf("%w: %q (ítem %d)", ErrSerialNumberExists, item.SerialNumber, other.ID)
		}
	}
	return nil
}

// sameIdentifier: dos identificadores vacíos nunca coinciden.
func sameIdentifier(a, b string) bool {
	a, b = strings.TrimSpace(a), strings.TrimSpace(b)
	return a != "" && strings.EqualFold(a, b)
}
