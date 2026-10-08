package repository

import (
	"context"
	"fmt"

	"github.com/xuri/excelize/v2"

	"inventario/internal/excelcell"
	"inventario/internal/models"
)

// ============================================================
// InventoryRepository: lectura
// ============================================================

func (r *ExcelRepository) GetAll(ctx context.Context) ([]models.Item, error) {
	items := []models.Item{}
	err := r.read(ctx, func(f *excelize.File) error {
		return scanRows(f, inventorySchema, requiredInventoryColumns, func(rowNum int, cells []string) (bool, error) {
			item, err := rowToItem(cells, rowNum)
			if err != nil {
				return true, err
			}
			items = append(items, item)
			return false, nil
		})
	})
	if err != nil {
		return nil, err
	}
	return items, nil
}

func (r *ExcelRepository) GetByID(ctx context.Context, id int) (models.Item, error) {
	var item models.Item
	err := r.read(ctx, func(f *excelize.File) error {
		var err error
		_, item, err = findItemRow(f, id)
		return err
	})
	return item, err
}

// findItemRow devuelve el número de fila de Excel y el ítem con ese ID.
// Convierte solo la fila buscada y deja de leer al encontrarla.
func findItemRow(f *excelize.File, id int) (int, models.Item, error) {
	if id <= 0 {
		return 0, models.Item{}, fmt.Errorf("%w: id %d", ErrItemNotFound, id)
	}
	var (
		foundRow int
		found    models.Item
	)
	err := scanRows(f, inventorySchema, requiredInventoryColumns, func(rowNum int, cells []string) (bool, error) {
		if rowID, err := excelcell.ParseInt(cellAt(cells, colID)); err != nil || rowID != id {
			return false, nil
		}
		item, err := rowToItem(cells, rowNum)
		if err != nil {
			return true, err
		}
		foundRow, found = rowNum, item
		return true, nil
	})
	if err != nil {
		return 0, models.Item{}, err
	}
	if foundRow == 0 {
		return 0, models.Item{}, fmt.Errorf("%w: id %d", ErrItemNotFound, id)
	}
	return foundRow, found, nil
}

func (r *ExcelRepository) Search(ctx context.Context, filter models.ItemFilter) ([]models.Item, error) {
	all, err := r.GetAll(ctx)
	if err != nil {
		return nil, err
	}
	matcher := newItemMatcher(filter)
	result := []models.Item{}
	for _, item := range all {
		if matcher.matches(item) {
			result = append(result, item)
		}
	}
	return result, nil
}

func cellAt(cells []string, col int) string {
	if col < len(cells) {
		return cells[col]
	}
	return ""
}

// ============================================================
// Pendiente: categorías
// ============================================================

func (r *ExcelRepository) ListCategories(ctx context.Context) ([]models.Category, error) {
	return nil, ErrNotImplemented
}
