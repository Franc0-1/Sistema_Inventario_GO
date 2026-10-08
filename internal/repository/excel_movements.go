package repository

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/xuri/excelize/v2"

	"inventario/internal/models"
)

// ============================================================
// MovementRepository
// ============================================================

func (r *ExcelRepository) GetMovements(ctx context.Context) ([]models.Movement, error) {
	return r.readMovements(ctx, func(models.Movement) bool { return true })
}

func (r *ExcelRepository) GetMovementsByItemID(ctx context.Context, itemID int) ([]models.Movement, error) {
	return r.readMovements(ctx, func(m models.Movement) bool { return m.ItemID == itemID })
}

func (r *ExcelRepository) CreateMovement(ctx context.Context, mov models.Movement) (models.Movement, error) {
	if err := checkMovementStorable(mov); err != nil {
		return models.Movement{}, err
	}
	var created models.Movement
	err := r.write(ctx, func(f *excelize.File) error {
		mov.CreatedAt = r.timestamp(time.Time{})
		var err error
		created, err = appendMovement(f, mov)
		return err
	})
	if err != nil {
		return models.Movement{}, fmt.Errorf("registrar movimiento: %w", err)
	}
	return created, nil
}

// readMovements lee la hoja completa y devuelve los movimientos que cumplen
// keep, ordenados por ID ascendente.
func (r *ExcelRepository) readMovements(ctx context.Context, keep func(models.Movement) bool) ([]models.Movement, error) {
	movements := []models.Movement{}
	err := r.read(ctx, func(f *excelize.File) error {
		return scanRows(f, movementSchema, movementColumns, func(rowNum int, cells []string) (bool, error) {
			mov, err := rowToMovement(cells, rowNum)
			if err != nil {
				return true, err
			}
			if keep(mov) {
				movements = append(movements, mov)
			}
			return false, nil
		})
	})
	if err != nil {
		return nil, err
	}
	slices.SortFunc(movements, func(a, b models.Movement) int { return a.ID - b.ID })
	return movements, nil
}

// appendStockMovement completa el movimiento con lo que realmente se escribió
// en "Inventario" (ítem, cantidades y fecha) y lo agrega. Sin lock: se llama
// dentro de write().
func appendStockMovement(f *excelize.File, mov models.Movement, before, after models.Item) (models.Movement, error) {
	mov, err := stockMovementFor(mov, before, after)
	if err != nil {
		return models.Movement{}, err
	}
	return appendMovement(f, mov)
}

// stockMovementFor completa el movimiento con lo que realmente se escribe
// (ítem, cantidades anterior y nueva, fecha) y verifica que coincida con el
// cambio. Lo comparten los repositorios de Excel y de SQL Server.
func stockMovementFor(mov models.Movement, before, after models.Item) (models.Movement, error) {
	mov.ItemID = after.ID
	mov.PreviousQuantity, mov.NewQuantity = before.Quantity, after.Quantity
	mov.CreatedAt = after.UpdatedAt
	if err := checkMovementStorable(mov); err != nil {
		return models.Movement{}, err
	}
	if !matchesChange(mov) {
		return models.Movement{}, fmt.Errorf("%w: %s de %d no coincide con el cambio guardado (%d -> %d)",
			ErrInvalidMovement, mov.MovementType, mov.Quantity, mov.PreviousQuantity, mov.NewQuantity)
	}
	return mov, nil
}

// matchesChange verifica que el movimiento describa el cambio de cantidad que
// realmente se escribe en "Inventario", para que el historial nunca lo
// contradiga. Como el error cancela write(), tampoco se guarda el ítem.
func matchesChange(mov models.Movement) bool {
	diff := mov.NewQuantity - mov.PreviousQuantity
	switch mov.MovementType {
	case models.MovementStockIn:
		return diff > 0 && mov.Quantity == diff
	case models.MovementStockOut:
		return diff < 0 && mov.Quantity == -diff
	case models.MovementStockUpdate:
		return diff == 0 && mov.Quantity == 0
	case models.MovementAdjustment:
		return mov.Quantity == max(diff, -diff)
	case models.MovementTransfer:
		return diff == 0
	}
	return false
}

// appendMovement asigna el ID y agrega la fila al final de "Movimientos".
// Sin lock: se llama dentro de write().
func appendMovement(f *excelize.File, mov models.Movement) (models.Movement, error) {
	id, row, err := movementIDs.next(f)
	if err != nil {
		return models.Movement{}, err
	}
	mov.ID = id
	if err := setRow(f, SheetMovements, row, movementToRow(mov)); err != nil {
		return models.Movement{}, err
	}
	return mov, nil
}

// checkMovementStorable verifica invariantes de almacenamiento del historial.
func checkMovementStorable(mov models.Movement) error {
	switch {
	case mov.ItemID <= 0:
		return fmt.Errorf("%w: ItemID inválido (%d)", ErrInvalidMovement, mov.ItemID)
	case mov.MovementType == "":
		return fmt.Errorf("%w: falta el tipo de movimiento", ErrInvalidMovement)
	case mov.Quantity < 0 || mov.PreviousQuantity < 0 || mov.NewQuantity < 0:
		return fmt.Errorf("%w: cantidades negativas", ErrInvalidMovement)
	}
	return nil
}
