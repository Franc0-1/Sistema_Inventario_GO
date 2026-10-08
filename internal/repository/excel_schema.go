package repository

import (
	"errors"
	"strings"
	"time"

	"inventario/internal/excelcell"
	"inventario/internal/models"
)

// Esquema del libro inventario.xlsx: ÚNICA fuente de verdad sobre nombres de
// hojas, orden de columnas y formato de celdas. Fila 1 = encabezado.

const (
	SheetInventory  = "Inventario"
	SheetMovements  = "Movimientos"
	SheetCategories = "Categorias"

	headerRow = 1
)

// Columnas de la hoja Inventario (índice 0 = columna A). A–M son las de la
// especificacion; N-P, prestamos; Q, el gabinete padre. Las nuevas van al final.
const (
	colID = iota
	colInventoryNumber
	colHasInventory
	colDeviceType
	colBrand
	colModel
	colSerialNumber
	colQuantity
	colLocation
	colStatus
	colNotes
	colCreatedAt
	colUpdatedAt
	colAvailability
	colAssignedTo
	colLoanedAt
	colEquipmentID
	inventoryColumns
)

// Columnas de la hoja Movimientos.
const (
	colMovID = iota
	colMovItemID
	colMovType
	colMovQuantity
	colMovPreviousQuantity
	colMovNewQuantity
	colMovOrigin
	colMovDestination
	colMovNotes
	colMovCreatedAt
	movementColumns
)

// Columnas de la hoja Categorias.
const (
	colCatID = iota
	colCatName
	colCatActive
	colCatLoanable
	categoryColumns
)

// sheetSchema describe una hoja. legacyHeaders son encabezados anteriores
// cuyas columnas significan lo mismo en el mismo orden (solo cambió el
// nombre): un libro con esos encabezados puede actualizarse sin mover datos.
type sheetSchema struct {
	name          string
	header        []string
	legacyHeaders [][]string
}

var (
	inventorySchema = sheetSchema{
		name: SheetInventory,
		header: []string{
			"ID", "InventoryNumber", "HasInventory", "DeviceType", "Brand", "Model",
			"SerialNumber", "Quantity", "Location", "Status", "Notes", "CreatedAt", "UpdatedAt",
			"Availability", "AssignedTo", "LoanedAt", "EquipmentID",
		},
	}
	// Las versiones anteriores de esta hoja tenían otras columnas en otro
	// orden: no son equivalentes, así que un libro viejo solo se actualiza si
	// la hoja está vacía (nunca se escribieron movimientos antes de la Etapa 7).
	movementSchema = sheetSchema{
		name: SheetMovements,
		header: []string{
			"ID", "ItemID", "MovementType", "Quantity", "PreviousQuantity", "NewQuantity",
			"OriginLocation", "DestinationLocation", "Notes", "CreatedAt",
		},
	}
	categorySchema = sheetSchema{
		name:          SheetCategories,
		header:        []string{"ID", "Name", "Active", "Loanable"},
		legacyHeaders: [][]string{{"ID", "Nombre", "Activa", "Prestable"}},
	}

	allSchemas = []sheetSchema{inventorySchema, movementSchema, categorySchema}

	// requiredSheets son las hojas sin las cuales no se puede abrir el libro.
	requiredSheets = []string{SheetInventory, SheetMovements}
)

// requiredInventoryColumns: A–M deben estar presentes; N–P son opcionales al
// leer (un libro sin la extensión se lee con valores por defecto).
const requiredInventoryColumns = colUpdatedAt + 1

// initialCategories se cargan solo al crear un libro nuevo.
var initialCategories = []models.Category{
	{Name: "PC"},
	{Name: "Notebook", Loanable: true},
	{Name: "Celular", Loanable: true},
	{Name: "Monitor", Loanable: true},
	{Name: "Impresora"},
	{Name: "Teclado", Loanable: true},
	{Name: "Mouse", Loanable: true},
	{Name: "Accesorio", Loanable: true},
	{Name: "Switch"},
	{Name: "Router"},
	{Name: "Access Point"},
	{Name: "UPS"},
	{Name: "Teléfono IP"},
	{Name: models.DeviceTypeAirConditioner},
	{Name: "Otro", Loanable: true},
}

// ---- Encabezados ----

// trimTrailingEmpty quita celdas vacías al final (Excel las devuelve a veces).
func trimTrailingEmpty(cells []string) []string {
	n := len(cells)
	for n > 0 && strings.TrimSpace(cells[n-1]) == "" {
		n--
	}
	return cells[:n]
}

// isPrefixOf informa si got coincide con el comienzo de want.
func isPrefixOf(got, want []string) bool {
	got = trimTrailingEmpty(got)
	if len(got) > len(want) {
		return false
	}
	for i := range got {
		if strings.TrimSpace(got[i]) != want[i] {
			return false
		}
	}
	return true
}

// firstHeaderMismatch devuelve la primera columna distinta (1-based), o 0.
func firstHeaderMismatch(got, want []string) int {
	for i := range want {
		valor := ""
		if i < len(got) {
			valor = strings.TrimSpace(got[i])
		}
		if valor != want[i] {
			return i + 1
		}
	}
	return 0
}

// isBlankRow informa si una fila no tiene ningún valor (fila intermedia vacía).
func isBlankRow(cells []string) bool {
	return len(trimTrailingEmpty(cells)) == 0
}

// ---- Lectura de celdas ----

// rowReader convierte las celdas de una fila guardando el primer error como
// CellError, para no repetir el manejo de errores en cada campo.
type rowReader struct {
	schema sheetSchema
	row    int
	cells  []string
	err    error
}

func (r *rowReader) readText(col int) string {
	if col < len(r.cells) {
		return strings.TrimSpace(r.cells[col])
	}
	return ""
}

func (r *rowReader) fail(col int, err error) {
	if r.err == nil {
		r.err = &CellError{Sheet: r.schema.name, Row: r.row, Column: col + 1, Field: r.schema.header[col], Err: err}
	}
}

func (r *rowReader) readInt(col int) int {
	n, err := excelcell.ParseInt(r.readText(col))
	if err != nil {
		r.fail(col, err)
	}
	return n
}

// readID lee la columna A: toda fila con datos debe tener un ID entero positivo.
func (r *rowReader) readID() int {
	id := r.readInt(colID)
	if r.err == nil && id <= 0 {
		r.fail(colID, errors.New("el ID debe ser un entero positivo"))
	}
	return id
}

func (r *rowReader) readBool(col int) bool {
	b, err := excelcell.ParseBool(r.readText(col))
	if err != nil {
		r.fail(col, err)
	}
	return b
}

func (r *rowReader) readTime(col int) time.Time {
	t, err := excelcell.ParseTime(r.readText(col))
	if err != nil {
		r.fail(col, err)
	}
	return t
}

func (r *rowReader) readOptionalTime(col int) *time.Time {
	if t := r.readTime(col); !t.IsZero() {
		return &t
	}
	return nil
}

// ---- Conversión fila <-> struct (funciones puras, sin E/S) ----

// rowToItem convierte una fila de Inventario. row es el número de fila en
// Excel, solo para el mensaje de error.
func rowToItem(cells []string, row int) (models.Item, error) {
	r := rowReader{schema: inventorySchema, row: row, cells: cells}
	item := models.Item{
		ID:              r.readID(),
		InventoryNumber: r.readText(colInventoryNumber),
		HasInventory:    r.readBool(colHasInventory),
		DeviceType:      r.readText(colDeviceType),
		Brand:           r.readText(colBrand),
		Model:           r.readText(colModel),
		SerialNumber:    r.readText(colSerialNumber),
		Quantity:        r.readInt(colQuantity),
		Location:        r.readText(colLocation),
		Status:          models.ItemStatus(r.readText(colStatus)),
		Notes:           r.readText(colNotes),
		CreatedAt:       r.readTime(colCreatedAt),
		UpdatedAt:       r.readTime(colUpdatedAt),
		Availability:    models.Availability(r.readText(colAvailability)),
		AssignedTo:      r.readText(colAssignedTo),
		LoanedAt:        r.readOptionalTime(colLoanedAt),
		EquipmentID:     r.readInt(colEquipmentID),
	}
	if r.err != nil {
		return models.Item{}, r.err
	}
	if item.Availability == "" {
		item.Availability = models.Available // valor por defecto de la columna
	}
	return item, nil
}

// rowID lee solo el ID (columna A) de una fila de la hoja, con la misma
// validación que rowToItem / rowToMovement.
func rowID(s sheetSchema, cells []string, row int) (int, error) {
	r := rowReader{schema: s, row: row, cells: cells}
	id := r.readID()
	return id, r.err
}

func itemToRow(item models.Item) []any {
	row := make([]any, inventoryColumns)
	row[colID] = item.ID
	row[colInventoryNumber] = item.InventoryNumber
	row[colHasInventory] = excelcell.FormatBool(item.HasInventory)
	row[colDeviceType] = item.DeviceType
	row[colBrand] = item.Brand
	row[colModel] = item.Model
	row[colSerialNumber] = item.SerialNumber
	row[colQuantity] = item.Quantity
	row[colLocation] = item.Location
	row[colStatus] = string(item.Status)
	row[colNotes] = item.Notes
	row[colCreatedAt] = excelcell.FormatTime(item.CreatedAt)
	row[colUpdatedAt] = excelcell.FormatTime(item.UpdatedAt)
	row[colAvailability] = string(item.Availability)
	row[colAssignedTo] = item.AssignedTo
	row[colLoanedAt] = ""
	row[colEquipmentID] = item.EquipmentID
	if item.LoanedAt != nil {
		row[colLoanedAt] = excelcell.FormatTime(*item.LoanedAt)
	}
	return row
}

func rowToMovement(cells []string, row int) (models.Movement, error) {
	r := rowReader{schema: movementSchema, row: row, cells: cells}
	mov := models.Movement{
		ID:                  r.readID(),
		ItemID:              r.readInt(colMovItemID),
		MovementType:        models.MovementType(r.readText(colMovType)),
		Quantity:            r.readInt(colMovQuantity),
		PreviousQuantity:    r.readInt(colMovPreviousQuantity),
		NewQuantity:         r.readInt(colMovNewQuantity),
		OriginLocation:      r.readText(colMovOrigin),
		DestinationLocation: r.readText(colMovDestination),
		Notes:               r.readText(colMovNotes),
		CreatedAt:           r.readTime(colMovCreatedAt),
	}
	if r.err != nil {
		return models.Movement{}, r.err
	}
	return mov, nil
}

func movementToRow(mov models.Movement) []any {
	row := make([]any, movementColumns)
	row[colMovID] = mov.ID
	row[colMovItemID] = mov.ItemID
	row[colMovType] = string(mov.MovementType)
	row[colMovQuantity] = mov.Quantity
	row[colMovPreviousQuantity] = mov.PreviousQuantity
	row[colMovNewQuantity] = mov.NewQuantity
	row[colMovOrigin] = mov.OriginLocation
	row[colMovDestination] = mov.DestinationLocation
	row[colMovNotes] = mov.Notes
	row[colMovCreatedAt] = excelcell.FormatTime(mov.CreatedAt)
	return row
}

func rowToCategory(cells []string, row int) (models.Category, error) {
	r := rowReader{schema: categorySchema, row: row, cells: cells}
	cat := models.Category{
		ID:     r.readInt(colCatID),
		Name:   r.readText(colCatName),
		Active: r.readBool(colCatActive),
		// Vacío = fila anterior a la columna: se mantiene el comportamiento previo (prestable).
		Loanable: r.readText(colCatLoanable) == "" || r.readBool(colCatLoanable),
	}
	return cat, r.err
}

func categoryToRow(cat models.Category) []any {
	row := make([]any, categoryColumns)
	row[colCatID] = cat.ID
	row[colCatName] = cat.Name
	row[colCatActive] = excelcell.FormatBool(cat.Active)
	row[colCatLoanable] = excelcell.FormatBool(cat.Loanable)
	return row
}
