package repository

import (
	"errors"
	"fmt"
	"inventario/internal/models"
)

func validateImportedEquipment(items []models.Item, rows []int, schema sheetSchema, equipmentCol, numberCol int) error {
	if err := models.ValidateEquipment(items); err != nil {
		var relation *models.EquipmentError
		if errors.As(err, &relation) {
			for i, item := range items {
				if item.ID == relation.ItemID {
					return dataError(schema, rows[i], equipmentCol, err)
				}
			}
		}
		return fmt.Errorf("%w: %v", ErrInvalidItem, err)
	}
	seen := map[string]int{}
	for i, item := range items {
		key := normalizedIdentifier(item.InventoryNumber)
		if key == "" {
			continue
		}
		if previous, ok := seen[key]; ok && !models.ShareEquipment(item, items[previous]) {
			return dataError(schema, rows[i], numberCol, fmt.Errorf("numero de inventario %q duplicado / repetido fuera del mismo equipo (fila %d)", item.InventoryNumber, rows[previous]))
		}
		seen[key] = i
	}
	return nil
}
