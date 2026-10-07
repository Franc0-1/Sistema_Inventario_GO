package repository

import (
	"errors"
	"fmt"
	"time"

	"github.com/xuri/excelize/v2"

	"inventario/internal/models"
	"inventario/internal/utils"
)

// Formato del Excel que se descarga desde la interfaz: en español y solo con
// los datos que usa una persona (sin fechas internas ni columnas técnicas).
// La importación acepta este mismo archivo, así se puede descargar, editar en
// Excel y volver a subir. El ID se conserva para no perder el historial de
// cada ítem; una fila sin ID es un ítem nuevo.
var exportSchema = sheetSchema{
	name: SheetInventory,
	header: []string{
		"ID", "N° de inventario", "Tipo de dispositivo", "Marca", "Modelo", "N° de serie",
		"Cantidad", "Área", "Estado", "Disponibilidad", "Asignado a", "Observación", "ID del equipo",
	},
}

const (
	expID = iota
	expInventoryNumber
	expDeviceType
	expBrand
	expModel
	expSerialNumber
	expQuantity
	expLocation
	expStatus
	expAvailability
	expAssignedTo
	expNotes
	expEquipmentID
	exportColumns
)

// Anchos de columna del archivo descargado, en el orden de exportSchema.
var exportWidths = []float64{7, 18, 22, 16, 24, 20, 10, 20, 16, 15, 22, 40, 16}

// Textos en español de los estados (los mismos que muestra la interfaz).
var (
	statusLabels = map[models.ItemStatus]string{
		models.StatusOperational:  "Utilizable",
		models.StatusInRepair:     "En reparación",
		models.StatusOutOfService: "No utilizable",
		models.StatusRetired:      "Dado de baja",
	}
	availabilityLabels = map[models.Availability]string{
		models.Available: "Disponible",
		models.Loaned:    "Prestado",
	}
)

// isTechnicalHeader distingue un archivo con el formato interno del libro
// (encabezados en inglés, como exportaba la versión anterior), que la
// importación sigue aceptando, del formato en español.
func isTechnicalHeader(header []string) bool {
	return len(header) > colInventoryNumber && header[colInventoryNumber] == inventorySchema.header[colInventoryNumber]
}

// ---- Exportación ----

func writeExportWorkbook(items []models.Item) ([]byte, error) {
	f := excelize.NewFile()
	defer f.Close()
	sheet := exportSchema.name
	if err := f.SetSheetName(f.GetSheetName(0), sheet); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrWriteFailed, err)
	}
	if err := writeHeader(f, exportSchema, nil); err != nil {
		return nil, err
	}
	for i, item := range items {
		if err := setRow(f, sheet, headerRow+1+i, itemToExportRow(item)); err != nil {
			return nil, err
		}
	}
	if err := formatExportSheet(f, len(items)); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrWriteFailed, err)
	}
	return writeBytes(f)
}

func itemToExportRow(item models.Item) []any {
	row := make([]any, exportColumns)
	row[expID] = item.ID
	row[expInventoryNumber] = item.InventoryNumber
	row[expDeviceType] = item.DeviceType
	row[expBrand] = item.Brand
	row[expModel] = item.Model
	row[expSerialNumber] = item.SerialNumber
	row[expQuantity] = item.Quantity
	row[expLocation] = item.Location
	row[expStatus] = labelOr(statusLabels, item.Status)
	row[expAvailability] = labelOr(availabilityLabels, item.Availability)
	row[expAssignedTo] = item.AssignedTo
	row[expNotes] = item.Notes
	if item.EquipmentID != 0 {
		row[expEquipmentID] = item.EquipmentID
	}
	return row
}

func labelOr[K ~string](labels map[K]string, value K) string {
	if label, ok := labels[value]; ok {
		return label
	}
	return string(value)
}

// formatExportSheet: encabezado resaltado y fijo, filtros y anchos legibles.
func formatExportSheet(f *excelize.File, rows int) error {
	sheet := exportSchema.name
	last, _ := excelize.ColumnNumberToName(exportColumns)
	style, err := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Bold: true, Color: "FFFFFF"},
		Fill:      excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{"1F4E78"}},
		Alignment: &excelize.Alignment{Vertical: "center"},
	})
	if err != nil {
		return err
	}
	if err := f.SetCellStyle(sheet, "A1", last+"1", style); err != nil {
		return err
	}
	for i, width := range exportWidths {
		col, _ := excelize.ColumnNumberToName(i + 1)
		if err := f.SetColWidth(sheet, col, col, width); err != nil {
			return err
		}
	}
	if err := f.SetPanes(sheet, &excelize.Panes{Freeze: true, YSplit: 1, TopLeftCell: "A2", ActivePane: "bottomLeft"}); err != nil {
		return err
	}
	return f.AutoFilter(sheet, fmt.Sprintf("A1:%s%d", last, rows+1), nil)
}

// ---- Importación ----

// spanishRow es una fila del formato en español ya validada. Le faltan las
// fechas y, si es nueva (ID 0), el ID: se completan contra el libro real.
type spanishRow struct {
	row               int
	item              models.Item
	equipmentProvided bool
}

// readSpanishRows lee y valida las filas del archivo en español. Las
// reglas son las mismas que al cargar un ítem desde la interfaz.
func readSpanishRows(f *excelize.File) ([]spanishRow, error) {
	header, _, err := readHeader(f, SheetInventory)
	if err != nil {
		return nil, err
	}
	var (
		rows          []spanishRow
		ids           = map[int]int{}
		serialNumbers = map[string]int{}
	)
	err = scanRows(f, exportSchema, expEquipmentID, func(rowNum int, cells []string) (bool, error) {
		item, col, problem := spanishRowToItem(cells, rowNum)
		if problem == nil {
			col, problem = spanishDuplicate(item, rowNum, ids, serialNumbers)
		}
		if problem != nil {
			var cellErr *CellError
			if errors.As(problem, &cellErr) {
				return true, problem
			}
			return true, dataError(exportSchema, rowNum, col, problem)
		}
		rows = append(rows, spanishRow{row: rowNum, item: item, equipmentProvided: len(header) > expEquipmentID})
		return false, nil
	})
	return rows, err
}

func spanishRowToItem(cells []string, rowNum int) (models.Item, int, error) {
	r := rowReader{schema: exportSchema, row: rowNum, cells: cells}
	item := models.Item{
		ID:              r.readInt(expID),
		InventoryNumber: r.readText(expInventoryNumber),
		DeviceType:      r.readText(expDeviceType),
		Brand:           r.readText(expBrand),
		Model:           r.readText(expModel),
		SerialNumber:    r.readText(expSerialNumber),
		Location:        r.readText(expLocation),
		AssignedTo:      r.readText(expAssignedTo),
		Notes:           r.readText(expNotes),
		EquipmentID:     r.readInt(expEquipmentID),
	}
	item.HasInventory = item.InventoryNumber != ""
	if quantity := r.readText(expQuantity); quantity == "" && item.HasInventory {
		item.Quantity = 1 // un equipo con N° de inventario es unitario
	} else if quantity == "" {
		return item, expQuantity, errors.New("falta la cantidad")
	} else {
		item.Quantity = r.readInt(expQuantity)
	}
	if r.err != nil {
		return item, 0, r.err
	}

	var ok bool
	if item.Status, ok = parseLabel(r.readText(expStatus), statusLabels, models.StatusOperational); !ok {
		return item, expStatus, fmt.Errorf("estado %q desconocido (válidos: Utilizable, En reparación, No utilizable, Dado de baja)", r.readText(expStatus))
	}
	if item.Availability, ok = parseLabel(r.readText(expAvailability), availabilityLabels, models.Available); !ok {
		return item, expAvailability, fmt.Errorf("disponibilidad %q desconocida (válidas: Disponible, Prestado)", r.readText(expAvailability))
	}

	required := []struct {
		col   int
		value string
	}{{expDeviceType, item.DeviceType}, {expBrand, item.Brand}, {expModel, item.Model}, {expLocation, item.Location}}
	for _, field := range required {
		if field.value == "" {
			return item, field.col, errors.New("es obligatorio")
		}
	}
	switch {
	case item.ID < 0:
		return item, expID, errors.New("el ID debe ser un entero positivo, o quedar vacío para un ítem nuevo")
	case item.Quantity < 0:
		return item, expQuantity, fmt.Errorf("cantidad negativa (%d)", item.Quantity)
	case item.Availability == models.Available && item.AssignedTo != "":
		return item, expAssignedTo, errors.New("debe quedar vacío si el ítem está Disponible")
	}
	return item, 0, nil
}

// parseLabel acepta el texto en español o el código interno, sin distinguir
// mayúsculas ni tildes. Vacío es el valor por defecto.
func parseLabel[K ~string](text string, labels map[K]string, empty K) (K, bool) {
	if text == "" {
		return empty, true
	}
	folded := utils.FoldText(text)
	for value, label := range labels {
		if folded == utils.FoldText(label) || folded == utils.FoldText(string(value)) {
			return value, true
		}
	}
	return "", false
}

func spanishDuplicate(item models.Item, rowNum int, ids map[int]int, serialNumbers map[string]int) (int, error) {
	if item.ID != 0 {
		if row := ids[item.ID]; row != 0 {
			return expID, fmt.Errorf("ID %d repetido (fila %d)", item.ID, row)
		}
		ids[item.ID] = rowNum
	}
	if key := normalizedIdentifier(item.SerialNumber); key != "" {
		if row := serialNumbers[key]; row != 0 {
			return expSerialNumber, fmt.Errorf("número de serie %q repetido (fila %d)", item.SerialNumber, row)
		}
		serialNumbers[key] = rowNum
	}
	return 0, nil
}

// resolveSpanishRows completa lo que el archivo en español no trae, a partir
// del inventario actual:
//   - una fila con ID debe corresponder a un ítem existente: conserva su fecha
//     de alta (y la de préstamo si sigue prestado), y su fecha de
//     modificación solo cambia si cambió algún dato;
//   - una fila sin ID es un ítem nuevo y recibe el próximo ID libre (nunca
//     uno ya usado, aunque su ítem se haya eliminado).
//
// Devuelve los ítems completos y el mayor ID asignado.
func resolveSpanishRows(rows []spanishRow, current []models.Item, lastIssued int, timestamp func(time.Time) time.Time) ([]models.Item, int, error) {
	byID := make(map[int]models.Item, len(current))
	nextID := lastIssued
	for _, it := range current {
		byID[it.ID] = it
		nextID = max(nextID, it.ID)
	}
	items := make([]models.Item, 0, len(rows))
	for _, r := range rows {
		item := r.item
		if item.ID == 0 {
			nextID++
			item.ID = nextID
			item.CreatedAt = timestamp(time.Time{})
			item.UpdatedAt = item.CreatedAt
			if item.Availability == models.Loaned {
				loanedAt := item.CreatedAt
				item.LoanedAt = &loanedAt
			}
			items = append(items, item)
			continue
		}
		existing, ok := byID[item.ID]
		if !ok {
			return nil, 0, dataError(exportSchema, r.row, expID,
				fmt.Errorf("el ID %d no existe en el inventario actual; déjelo vacío para agregar un ítem nuevo", item.ID))
		}
		item.CreatedAt = existing.CreatedAt
		if !r.equipmentProvided {
			item.EquipmentID = existing.EquipmentID
		}
		item.LoanedAt = nil
		if item.Availability == models.Loaned {
			item.LoanedAt = existing.LoanedAt
		}
		if sameEditableData(item, existing) {
			item.UpdatedAt = existing.UpdatedAt
		} else {
			item.UpdatedAt = timestamp(existing.UpdatedAt)
		}
		if item.Availability == models.Loaned && item.LoanedAt == nil {
			loanedAt := item.UpdatedAt
			item.LoanedAt = &loanedAt
		}
		items = append(items, item)
	}
	return items, nextID, nil
}

// sameEditableData compara los campos que trae el archivo en español.
func sameEditableData(a, b models.Item) bool {
	return a.InventoryNumber == b.InventoryNumber && a.HasInventory == b.HasInventory &&
		a.DeviceType == b.DeviceType && a.Brand == b.Brand && a.Model == b.Model &&
		a.SerialNumber == b.SerialNumber && a.Quantity == b.Quantity && a.Location == b.Location &&
		a.Status == b.Status && a.Availability == b.Availability && a.AssignedTo == b.AssignedTo &&
		a.Notes == b.Notes && a.EquipmentID == b.EquipmentID
}
