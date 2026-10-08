package repository

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	"github.com/xuri/excelize/v2"

	"inventario/internal/models"
)

// ExportInventory genera un libro nuevo con el inventario en español y solo
// las columnas útiles para una persona (ver exportSchema). No abre el camino
// de escritura ni toca el archivo real.
func (r *ExcelRepository) ExportInventory(ctx context.Context) ([]byte, error) {
	items, err := r.GetAll(ctx)
	if err != nil {
		return nil, err
	}
	return writeExportWorkbook(items)
}

func writeBytes(f *excelize.File) ([]byte, error) {
	var buf bytes.Buffer
	if _, err := f.WriteTo(&buf); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrWriteFailed, err)
	}
	return buf.Bytes(), nil
}

// importData es el archivo recibido ya validado, en uno de los dos formatos:
// el técnico (ítems completos) o el descargado en español (filas a completar
// contra el libro real).
type importData struct {
	items     []models.Item
	maxID     int
	spanish   []spanishRow
	isSpanish bool
}

// ImportInventory valida primero el archivo recibido completo y recién después
// reemplaza la hoja Inventario del libro real usando backup + commit.
func (r *ExcelRepository) ImportInventory(ctx context.Context, src io.Reader) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	data, err := readImportWorkbook(src)
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
	current, err := readInventoryItems(f)
	if err != nil {
		return 0, err
	}
	items, maxImportedID, err := prepareImport(data, current, lastIssued, r.timestamp)
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

// readInventoryItems lee todos los ítems de un libro ya abierto (sin lock: se
// llama dentro de una operación que ya lo tiene).
func readInventoryItems(f *excelize.File) ([]models.Item, error) {
	items := []models.Item{}
	err := scanRows(f, inventorySchema, requiredInventoryColumns, func(rowNum int, cells []string) (bool, error) {
		item, err := rowToItem(cells, rowNum)
		if err != nil {
			return true, err
		}
		items = append(items, item)
		return false, nil
	})
	return items, err
}

func readImportWorkbook(src io.Reader) (importData, error) {
	f, err := excelize.OpenReader(src)
	if err != nil {
		return importData{}, fmt.Errorf("%w: archivo Excel inválido", ErrInvalidWorkbook)
	}
	defer f.Close()
	if err := requireSheets(f, SheetInventory); err != nil {
		return importData{}, err
	}
	header, _, err := readHeader(f, SheetInventory)
	if err != nil {
		return importData{}, err
	}
	if !isTechnicalHeader(header) {
		rows, err := readSpanishRows(f)
		return importData{spanish: rows, isSpanish: true}, err
	}
	items, maxID, err := readTechnicalRows(f, header)
	return importData{items: items, maxID: maxID}, err
}

// readTechnicalRows lee el formato interno completo (encabezados en inglés).
func readTechnicalRows(f *excelize.File, header []string) ([]models.Item, int, error) {
	if err := checkHeader(inventorySchema, header, colLoanedAt+1); err != nil {
		return nil, 0, err
	}

	items := []models.Item{}
	seen := importSeen{
		ids:           map[int]bool{},
		serialNumbers: map[string]int{},
	}
	maxID := 0
	rows := []int{}
	err := scanRows(f, inventorySchema, colLoanedAt+1, func(rowNum int, cells []string) (bool, error) {
		item, err := rowToItem(cells, rowNum)
		if err != nil {
			return true, err
		}
		if col, problem := importItemProblem(item, seen); problem != nil {
			return true, dataError(inventorySchema, rowNum, col, problem)
		}
		seen.add(item, rowNum)
		items = append(items, item)
		rows = append(rows, rowNum)
		maxID = max(maxID, item.ID)
		return false, nil
	})
	if err != nil {
		return nil, 0, err
	}
	if err := validateImportedEquipment(items, rows, inventorySchema, colEquipmentID, colInventoryNumber); err != nil {
		return nil, 0, err
	}
	return items, maxID, nil
}

type importSeen struct {
	ids           map[int]bool
	serialNumbers map[string]int
}

func (s importSeen) add(item models.Item, row int) {
	s.ids[item.ID] = true
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
		if err := setRow(f, SheetInventory, headerRow+1+i, itemToRow(normalizeImported(item))); err != nil {
			return err
		}
	}
	return nil
}

// normalizeImported quita espacios de más y completa la disponibilidad.
func normalizeImported(item models.Item) models.Item {
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
	return item
}

// prepareImport deja los ítems importados listos para guardar, sin importar
// el almacenamiento: completa IDs y fechas del formato en español contra el
// inventario actual, valida los vínculos de equipos y normaliza los textos.
// Lo usan los repositorios de Excel y de SQL Server.
func prepareImport(data importData, current []models.Item, lastIssued int, timestamp func(time.Time) time.Time) ([]models.Item, int, error) {
	items, maxImportedID := data.items, data.maxID
	if data.isSpanish {
		var err error
		if items, maxImportedID, err = resolveSpanishRows(data.spanish, current, lastIssued, timestamp); err != nil {
			return nil, 0, err
		}
	}
	schema, equipmentCol, numberCol := inventorySchema, colEquipmentID, colInventoryNumber
	rows := make([]int, len(items))
	for i := range rows {
		rows[i] = i + headerRow + 1
	}
	if data.isSpanish {
		schema, equipmentCol, numberCol = exportSchema, expEquipmentID, expInventoryNumber
		for i := range rows {
			rows[i] = data.spanish[i].row
		}
	}
	if err := validateImportedEquipment(items, rows, schema, equipmentCol, numberCol); err != nil {
		return nil, 0, err
	}
	for i := range items {
		items[i] = normalizeImported(items[i])
	}
	return items, maxImportedID, nil
}
