package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/xuri/excelize/v2"

	"inventario/internal/excelcell"
	"inventario/internal/models"
)

// ============================================================
// InventoryRepository: escritura (todas pasan por write())
// ============================================================

func (r *ExcelRepository) Create(ctx context.Context, item models.Item) (models.Item, error) {
	if err := checkStorable(item); err != nil {
		return models.Item{}, err
	}
	err := r.write(ctx, func(f *excelize.File) error {
		id, row, err := inventoryIDs.next(f)
		if err != nil {
			return err
		}
		item.ID = id
		item.CreatedAt = r.timestamp(time.Time{})
		item.UpdatedAt = item.CreatedAt
		return setRow(f, SheetInventory, row, itemToRow(item))
	})
	if err != nil {
		return models.Item{}, fmt.Errorf("crear ítem: %w", err)
	}
	return item, nil
}

func (r *ExcelRepository) Update(ctx context.Context, item models.Item, expectedVersion time.Time, mov *models.Movement) (models.Item, error) {
	if err := checkStorable(item); err != nil {
		return models.Item{}, err
	}
	err := r.write(ctx, func(f *excelize.File) error {
		rowNum, current, err := findItemRow(f, item.ID)
		if err != nil {
			return err
		}
		if err := checkVersion(current, expectedVersion); err != nil {
			return err
		}
		item.CreatedAt = current.CreatedAt
		item.UpdatedAt = r.timestamp(current.UpdatedAt)
		if err := setRow(f, SheetInventory, rowNum, itemToRow(item)); err != nil {
			return err
		}
		if mov == nil {
			return nil
		}
		_, err = appendStockMovement(f, *mov, current, item)
		return err
	})
	if err != nil {
		return models.Item{}, fmt.Errorf("actualizar ítem %d: %w", item.ID, err)
	}
	return item, nil
}

func (r *ExcelRepository) Delete(ctx context.Context, id int, expectedVersion time.Time) error {
	err := r.write(ctx, func(f *excelize.File) error {
		rowNum, current, err := findItemRow(f, id)
		if err != nil {
			return err
		}
		if err := checkVersion(current, expectedVersion); err != nil {
			return err
		}
		// Se registra antes de borrar: si era el mayor ID, no debe volver a asignarse.
		// La hoja Movimientos no se toca: el historial del ítem se conserva.
		if err := inventoryIDs.raise(f, id); err != nil {
			return err
		}
		if err := f.RemoveRow(SheetInventory, rowNum); err != nil {
			return fmt.Errorf("%w: %v", ErrWriteFailed, err)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("eliminar ítem %d: %w", id, err)
	}
	return nil
}

// UpdateStock modifica "Inventario" y agrega el movimiento en memoria, y recién
// entonces write() guarda el libro una sola vez: si cualquier paso falla no se
// guarda nada, así que nunca queda stock sin movimiento ni movimiento sin stock.
func (r *ExcelRepository) UpdateStock(ctx context.Context, id, quantity int, expectedVersion time.Time, mov models.Movement) (models.Item, error) {
	if quantity < 0 {
		return models.Item{}, fmt.Errorf("%w: la cantidad no puede ser negativa (%d)", ErrInvalidItem, quantity)
	}
	var updated models.Item
	err := r.write(ctx, func(f *excelize.File) error {
		rowNum, current, err := findItemRow(f, id)
		if err != nil {
			return err
		}
		if err := checkVersion(current, expectedVersion); err != nil {
			return err
		}
		updated = current
		updated.Quantity = quantity
		updated.UpdatedAt = r.timestamp(current.UpdatedAt)
		// Solo estas dos celdas: el resto de la fila queda exactamente como estaba.
		if err := setInventoryCell(f, rowNum, colQuantity, updated.Quantity); err != nil {
			return err
		}
		if err := setInventoryCell(f, rowNum, colUpdatedAt, excelcell.FormatTime(updated.UpdatedAt)); err != nil {
			return err
		}
		_, err = appendStockMovement(f, mov, current, updated)
		return err
	})
	if err != nil {
		return models.Item{}, fmt.Errorf("actualizar stock del ítem %d: %w", id, err)
	}
	return updated, nil
}

// ---- Helpers de escritura ----

// checkStorable verifica invariantes de almacenamiento (no reglas de negocio).
func checkStorable(item models.Item) error {
	if item.Quantity < 0 {
		return fmt.Errorf("%w: la cantidad no puede ser negativa (%d)", ErrInvalidItem, item.Quantity)
	}
	return nil
}

func checkVersion(current models.Item, expected time.Time) error {
	if !current.UpdatedAt.Equal(expected) {
		return fmt.Errorf("%w: id %d (versión leída %s, guardada %s)", ErrConflict, current.ID,
			excelcell.FormatTime(expected), excelcell.FormatTime(current.UpdatedAt))
	}
	return nil
}

// timestamp devuelve la hora actual, siempre posterior a prev: UpdatedAt es la
// versión del ítem y dos escrituras seguidas nunca deben compartirla.
func (r *ExcelRepository) timestamp(prev time.Time) time.Time {
	t := r.now().Round(0) // sin lectura monotónica, igual a lo que se relee del Excel
	if !t.After(prev) {
		t = prev.Add(time.Nanosecond)
	}
	return t
}

func setInventoryCell(f *excelize.File, rowNum, col int, value any) error {
	cell, err := excelize.CoordinatesToCellName(col+1, rowNum)
	if err == nil {
		err = f.SetCellValue(SheetInventory, cell, value)
	}
	if err != nil {
		return fmt.Errorf("%w: %v", ErrWriteFailed, err)
	}
	return nil
}
