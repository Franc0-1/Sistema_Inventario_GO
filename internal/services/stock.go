package services

import (
	"context"
	"fmt"
	"math"
	"strings"

	"inventario/internal/models"
	"inventario/internal/utils"
)

// ApplyMovement registra un movimiento de inventario y aplica su efecto sobre
// el ítem en una única escritura del repositorio: nunca queda el ítem
// modificado sin su movimiento ni al revés.
//
// Las cantidades anterior y nueva salen del stock real leído aquí. Si otra
// operación escribe en el medio, el repositorio responde ErrConflict (la
// versión no coincide) y no guarda nada: dos salidas simultáneas no pueden
// dejar el stock en negativo.
func (s *inventoryService) ApplyMovement(ctx context.Context, id int, op models.StockOperation) (models.Item, error) {
	if err := validateID(id); err != nil {
		return models.Item{}, err
	}
	op.DestinationLocation = strings.TrimSpace(op.DestinationLocation)
	op.Notes = strings.TrimSpace(op.Notes)
	if err := validateOperation(op); err != nil {
		return models.Item{}, err
	}

	current, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return models.Item{}, err
	}
	mov, err := operationMovement(current, op)
	if err != nil {
		return models.Item{}, err
	}
	if err := validateMovement(mov); err != nil {
		return models.Item{}, err
	}

	if op.Type == models.MovementTransfer {
		moved := current
		moved.Location = op.DestinationLocation
		return s.repo.Update(ctx, moved, current.UpdatedAt, &mov)
	}
	return s.repo.UpdateStock(ctx, id, mov.NewQuantity, current.UpdatedAt, mov)
}

// validateOperation revisa lo que no depende del stock actual.
func validateOperation(op models.StockOperation) error {
	switch op.Type {
	case models.MovementStockIn, models.MovementStockOut:
		if err := validateQuantity(op.Quantity); err != nil {
			return err
		}
		if op.Quantity == 0 {
			return fmt.Errorf("%w: la cantidad de %s debe ser mayor a 0", ErrInvalidMovement, op.Type)
		}
	case models.MovementAdjustment, models.MovementStockUpdate:
		return validateQuantity(op.Quantity)
	case models.MovementTransfer:
		if op.Quantity != 0 {
			return fmt.Errorf("%w: una transferencia no cambia la cantidad; no envíe cantidad", ErrInvalidMovement)
		}
		if op.DestinationLocation == "" {
			return fmt.Errorf("%w: la transferencia requiere la ubicación de destino", ErrInvalidMovement)
		}
	default:
		return fmt.Errorf("%w: tipo %q desconocido (válidos: %v)", ErrInvalidMovement, op.Type, models.ValidMovementTypes)
	}
	return nil
}

// operationMovement calcula el movimiento a partir del estado real del ítem.
func operationMovement(current models.Item, op models.StockOperation) (models.Movement, error) {
	var mov models.Movement
	switch op.Type {
	case models.MovementStockIn:
		if op.Quantity > math.MaxInt-current.Quantity {
			return mov, fmt.Errorf("%w: la entrada de %d unidades excede el máximo", ErrInvalidQuantity, op.Quantity)
		}
		mov = stockMovement(current.ID, current.Quantity, current.Quantity+op.Quantity)
	case models.MovementStockOut:
		if op.Quantity > current.Quantity {
			return mov, fmt.Errorf("%w: se pidieron %d unidades y hay %d", ErrInsufficientStock, op.Quantity, current.Quantity)
		}
		mov = stockMovement(current.ID, current.Quantity, current.Quantity-op.Quantity)
	case models.MovementAdjustment:
		mov = adjustmentMovement(current.ID, current.Quantity, op.Quantity)
	case models.MovementStockUpdate:
		mov = stockMovement(current.ID, current.Quantity, op.Quantity)
	case models.MovementTransfer:
		if utils.FoldText(op.DestinationLocation) == utils.FoldText(current.Location) {
			return mov, fmt.Errorf("%w: el ítem ya está en %q", ErrInvalidMovement, current.Location)
		}
		mov = models.Movement{ItemID: current.ID, MovementType: models.MovementTransfer,
			Quantity: current.Quantity, PreviousQuantity: current.Quantity, NewQuantity: current.Quantity,
			OriginLocation: current.Location, DestinationLocation: op.DestinationLocation}
	}
	mov.Notes = op.Notes
	return mov, nil
}
