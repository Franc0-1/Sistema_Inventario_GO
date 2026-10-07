package repository

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/xuri/excelize/v2"

	"inventario/internal/models"
)

// ExportInventory genera un libro nuevo con la hoja Inventario completa. No
// abre el camino de escritura ni toca el archivo real.
func (r *ExcelRepository) ExportInventory(ctx context.Context) ([]byte, error) {
	items, err := r.GetAll(ctx)
	if err != nil {
		return nil, err
	}
	f := excelize.NewFile()
	defer f.Close()
	if err := f.SetSheetName(f.GetSheetName(0), SheetInventory); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrWriteFailed, err)
	}
	if err := writeHeader(f, inventorySchema, nil); err != nil {
		return nil, err
	}
	for i, item := range items {
		if err := setRow(f, SheetInventory, headerRow+1+i, itemToRow(item)); err != nil {
			return nil, err
		}
	}
	var buf bytes.Buffer
	if _, err := f.WriteTo(&buf); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrWriteFailed, err)
	}
	return buf.Bytes(), nil
}

// ImportInventory valida primero el archivo recibido completo y recién después
// reemplaza la hoja Inventario del libro real usando backup + commit.
func (r *ExcelRepository) ImportInventory(ctx context.Context, src io.Reader) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	items, maxImportedID, err := readImportWorkbook(src)
	if err != nil {
		return 0, err
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	f, err := openWorkbook(r.path)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	if err := validateWorkbook(f); err != nil {
		return 0, err
	}
	lastIssued, err := inventoryIDs.lastIssued(f)
	if err != nil {
		return 0, err
	}
	if err := replaceInventoryRows(f, items); err != nil {
		return 0, err
	}
	if err := inventoryIDs.setLastIssued(f, max(maxImportedID, lastIssued)); err != nil {
		return 0, err
	}
	if err := r.backup(); err != nil {
		return 0, err
	}
	if err := r.commit(f, validateWorkbook); err != nil {
		return 0, err
	}
	return len(items), nil
}

func readImportWorkbook(src io.Reader) ([]models.Item, int, error) {
	f, err := excelize.OpenReader(src)
	if err != nil {
		return nil, 0, fmt.Errorf("%w: archivo Excel inválido", ErrInvalidWorkbook)
	}
	defer f.Close()
	if err := requireSheets(f, SheetInventory); err != nil {
		return nil, 0, err
	}
	header, _, err := readHeader(f, SheetInventory)
	if err != nil {
		return nil, 0, err
	}
	if err := checkHeader(inventorySchema, header, inventoryColumns); err != nil {
		return nil, 0, err
	}

	items := []models.Item{}
	seen := importSeen{
		ids:              map[int]bool{},
		inventoryNumbers: map[string]int{},
		serialNumbers:    map[string]int{},
	}
	maxID := 0
	err = scanRows(f, inventorySchema, inventoryColumns, func(rowNum int, cells []string) (bool, error) {
		item, err := rowToItem(cells, rowNum)
		if err != nil {
			return true, err
		}
		if col, problem := importItemProblem(item, seen); problem != nil {
			return true, dataError(inventorySchema, rowNum, col, problem)
		}
		seen.add(item, rowNum)
		items = append(items, item)
		maxID = max(maxID, item.ID)
		return false, nil
	})
	if err != nil {
		return nil, 0, err
	}
	return items, maxID, nil
}

type importSeen struct {
	ids              map[int]bool
	inventoryNumbers map[string]int
	serialNumbers    map[string]int
}

func (s importSeen) add(item models.Item, row int) {
	s.ids[item.ID] = true
	if key := normalizedIdentifier(item.InventoryNumber); key != "" {
		s.inventoryNumbers[key] = row
	}
	if key := normalizedIdentifier(item.SerialNumber); key != "" {
		s.serialNumbers[key] = row
	}
}

func importItemProblem(item models.Item, seen importSeen) (int, error) {
	if col, problem := itemProblem(item, seen.ids); problem != nil {
		return col, problem
	}
	switch {
	case !slices.Contains(models.ValidStatuses, item.Status):
		return colStatus, fmt.Errorf("estado %q desconocido", item.Status)
	case item.Availability != models.Available && item.Availability != models.Loaned:
		return colAvailability, fmt.Errorf("disponibilidad %q desconocida", item.Availability)
	}
	if key := normalizedIdentifier(item.InventoryNumber); key != "" {
		if row := seen.inventoryNumbers[key]; row != 0 {
			return colInventoryNumber, fmt.Errorf("número de inventario %q duplicado (fila %d)", item.InventoryNumber, row)
		}
	}
	if key := normalizedIdentifier(item.SerialNumber); key != "" {
		if row := seen.serialNumbers[key]; row != 0 {
			return colSerialNumber, fmt.Errorf("número de serie %q duplicado (fila %d)", item.SerialNumber, row)
		}
	}
	return 0, nil
}

func normalizedIdentifier(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func replaceInventoryRows(f *excelize.File, items []models.Item) error {
	_, lastRow, err := inventoryIDs.scan(f)
	if err != nil {
		return err
	}
	for row := lastRow; row > headerRow; row-- {
		if err := f.RemoveRow(SheetInventory, row); err != nil {
			return fmt.Errorf("%w: %v", ErrWriteFailed, err)
		}
	}
	if err := writeHeader(f, inventorySchema, nil); err != nil {
		return err
	}
	for i, item := range items {
		item.InventoryNumber = strings.TrimSpace(item.InventoryNumber)
		item.DeviceType = strings.TrimSpace(item.DeviceType)
		item.Brand = strings.TrimSpace(item.Brand)
		item.Model = strings.TrimSpace(item.Model)
		item.SerialNumber = strings.TrimSpace(item.SerialNumber)
		item.Location = strings.TrimSpace(item.Location)
		item.Status = models.ItemStatus(strings.TrimSpace(string(item.Status)))
		item.Notes = strings.TrimSpace(item.Notes)
		item.AssignedTo = strings.TrimSpace(item.AssignedTo)
		if item.Availability == "" {
			item.Availability = models.Available
		}
		if err := setRow(f, SheetInventory, headerRow+1+i, itemToRow(item)); err != nil {
			return err
		}
	}
	return nil
}
