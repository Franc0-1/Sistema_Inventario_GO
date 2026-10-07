package repository

import (
	"errors"
	"fmt"
	"slices"

	"github.com/xuri/excelize/v2"

	"inventario/internal/models"
)

// Validación centralizada del libro. Solo detecta problemas: nunca modifica
// ni repara datos. Trabaja sobre un libro ya abierto y NO toma el mutex.
//
//   validateStructure: hojas y encabezados (barata; lecturas y /api/health).
//   validateWorkbook:  estructura + reglas de datos (antes de cada escritura y
//                      sobre el temporal antes de reemplazar el original).

// validateStructure exige las hojas Inventario y Movimientos con sus
// encabezados exactos. En Inventario, A–M son obligatorias y N–P (préstamos)
// la única extensión admitida.
func validateStructure(f *excelize.File) error {
	if err := requireSheets(f, requiredSheets...); err != nil {
		return err
	}
	checks := []struct {
		schema     sheetSchema
		minColumns int
	}{
		{inventorySchema, requiredInventoryColumns},
		{movementSchema, movementColumns},
	}
	for _, c := range checks {
		header, _, err := readHeader(f, c.schema.name)
		if err != nil {
			return err
		}
		if err := checkHeader(c.schema, header, c.minColumns); err != nil {
			return err
		}
	}
	return nil
}

// validateWorkbook valida estructura y datos. Los problemas de datos se
// informan como *CellError (errors.Is ErrInvalidCell) con hoja, celda y motivo.
func validateWorkbook(f *excelize.File) error {
	if err := validateStructure(f); err != nil {
		return err
	}
	itemIDs, maxItemID, err := validateInventoryRows(f)
	if err != nil {
		return err
	}
	lastIssued, err := inventoryIDs.lastIssued(f)
	if err != nil {
		return err
	}
	return validateMovementRows(f, itemIDs, max(maxItemID, lastIssued))
}

func validateInventoryRows(f *excelize.File) (ids map[int]bool, maxID int, err error) {
	ids = map[int]bool{}
	err = scanRows(f, inventorySchema, requiredInventoryColumns, func(rowNum int, cells []string) (bool, error) {
		item, err := rowToItem(cells, rowNum)
		if err != nil {
			return true, err
		}
		if col, problem := itemProblem(item, ids); problem != nil {
			return true, dataError(inventorySchema, rowNum, col, problem)
		}
		ids[item.ID], maxID = true, max(maxID, item.ID)
		return false, nil
	})
	return ids, maxID, err
}

func itemProblem(item models.Item, seen map[int]bool) (col int, problem error) {
	switch {
	case seen[item.ID]:
		return colID, fmt.Errorf("ID %d duplicado", item.ID)
	case item.Quantity < 0:
		return colQuantity, fmt.Errorf("cantidad negativa (%d)", item.Quantity)
	case item.HasInventory && item.InventoryNumber == "":
		return colInventoryNumber, errors.New("falta el número de inventario (HasInventory = SI)")
	case !item.HasInventory && item.InventoryNumber != "":
		return colInventoryNumber, fmt.Errorf("número de inventario %q con HasInventory = NO", item.InventoryNumber)
	case item.CreatedAt.IsZero():
		return colCreatedAt, errors.New("falta la fecha de alta")
	case item.UpdatedAt.IsZero():
		return colUpdatedAt, errors.New("falta la fecha de modificación")
	}
	return 0, nil
}

// validateMovementRows valida cada movimiento. Un ItemID es válido si el ítem
// existe o existió: el historial de un ítem eliminado se conserva (Etapa 7),
// así que solo es una referencia rota un ID que nunca se asignó
// (mayor que highestItemID, el mayor ID de ítem asignado alguna vez).
func validateMovementRows(f *excelize.File, itemIDs map[int]bool, highestItemID int) error {
	seen := map[int]bool{}
	return scanRows(f, movementSchema, movementColumns, func(rowNum int, cells []string) (bool, error) {
		mov, err := rowToMovement(cells, rowNum)
		if err != nil {
			return true, err
		}
		if col, problem := movementProblem(mov, seen, itemIDs, highestItemID); problem != nil {
			return true, dataError(movementSchema, rowNum, col, problem)
		}
		seen[mov.ID] = true
		return false, nil
	})
}

func movementProblem(m models.Movement, seen, itemIDs map[int]bool, highestItemID int) (col int, problem error) {
	switch {
	case seen[m.ID]:
		return colMovID, fmt.Errorf("ID %d duplicado", m.ID)
	case m.ItemID <= 0:
		return colMovItemID, fmt.Errorf("ItemID inválido (%d)", m.ItemID)
	case !itemIDs[m.ItemID] && m.ItemID > highestItemID:
		return colMovItemID, fmt.Errorf("%w: el ítem %d nunca existió", ErrInvalidMovementReference, m.ItemID)
	case !slices.Contains(models.ValidMovementTypes, m.MovementType):
		return colMovType, fmt.Errorf("tipo de movimiento %q desconocido", m.MovementType)
	case m.Quantity < 0:
		return colMovQuantity, fmt.Errorf("cantidad negativa (%d)", m.Quantity)
	case m.PreviousQuantity < 0:
		return colMovPreviousQuantity, fmt.Errorf("stock anterior negativo (%d)", m.PreviousQuantity)
	case m.NewQuantity < 0:
		return colMovNewQuantity, fmt.Errorf("stock nuevo negativo (%d)", m.NewQuantity)
	case m.CreatedAt.IsZero():
		return colMovCreatedAt, errors.New("falta la fecha")
	}
	return 0, nil
}

func dataError(s sheetSchema, row, col int, problem error) error {
	return &CellError{Sheet: s.name, Row: row, Column: col + 1, Field: s.header[col], Err: problem}
}
