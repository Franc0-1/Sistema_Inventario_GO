package repository

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"github.com/xuri/excelize/v2"
)

// ============================================================
// Creación y actualización del esquema
// ============================================================

func (r *ExcelRepository) prepareWorkbook() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, err := os.Stat(r.path); errors.Is(err, os.ErrNotExist) {
		return r.createWorkbook()
	} else if err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidWorkbook, err)
	}

	// Sin requireSheets: ensureSchema crea las hojas que falten.
	f, err := excelize.OpenFile(r.path)
	if err != nil {
		return fmt.Errorf("%w: %s: %v", ErrInvalidWorkbook, r.path, err)
	}
	defer f.Close()

	changed, err := ensureSchema(f)
	if err != nil || !changed {
		return err
	}
	// Mismo camino seguro que write(). Se valida solo la estructura: un dato
	// inválido no debe impedir arrancar (lo informan /api/health y las escrituras).
	if err := r.backup(); err != nil {
		return err
	}
	return r.commit(f, validateStructure)
}

func (r *ExcelRepository) createWorkbook() error {
	if err := os.MkdirAll(filepath.Dir(r.path), 0o755); err != nil {
		return fmt.Errorf("%w: %v", ErrWriteFailed, err)
	}
	f := excelize.NewFile()
	defer f.Close()

	if err := f.SetSheetName(f.GetSheetName(0), SheetInventory); err != nil {
		return fmt.Errorf("%w: %v", ErrWriteFailed, err)
	}
	if _, err := ensureSchema(f); err != nil {
		return err
	}
	for i, cat := range initialCategories {
		cat.ID, cat.Active = i+1, true
		if err := setRow(f, SheetCategories, headerRow+1+i, categoryToRow(cat)); err != nil {
			return err
		}
	}
	return r.commit(f, validateWorkbook) // libro nuevo: no hay original que respaldar
}

// ensureSchema lleva cada hoja al esquema actual. Informa si cambió algo.
func ensureSchema(f *excelize.File) (bool, error) {
	changed := false
	for _, s := range allSchemas {
		c, err := ensureSheet(f, s)
		if err != nil {
			return false, err
		}
		changed = changed || c
	}
	return changed, nil
}

// ensureSheet crea la hoja si falta y reescribe su encabezado solo cuando es
// seguro: la hoja no tiene datos, o el encabezado actual es un prefijo del
// esquema (faltan columnas al final) o de un encabezado anterior equivalente.
func ensureSheet(f *excelize.File, s sheetSchema) (bool, error) {
	idx, err := f.GetSheetIndex(s.name)
	if err != nil {
		return false, fmt.Errorf("%w: %v", ErrInvalidWorkbook, err)
	}
	if idx == -1 {
		if _, err := f.NewSheet(s.name); err != nil {
			return false, fmt.Errorf("%w: %v", ErrWriteFailed, err)
		}
		return true, writeHeader(f, s, nil)
	}

	current, hasData, err := readHeader(f, s.name)
	if err != nil {
		return false, err
	}
	if slices.Equal(trimTrailingEmpty(current), s.header) {
		return false, nil
	}
	// La asociacion es opcional: abrir un libro anterior no lo modifica.
	if s.name == SheetInventory && len(trimTrailingEmpty(current)) == colEquipmentID && isPrefixOf(current, s.header) {
		return false, nil
	}
	if !hasData || isPrefixOf(current, s.header) || matchesLegacyHeader(current, s) {
		return true, writeHeader(f, s, current)
	}
	col := firstHeaderMismatch(current, s.header)
	name, _ := excelize.ColumnNumberToName(col)
	return false, fmt.Errorf("%w: la hoja %q tiene datos y un encabezado incompatible (columna %s: se esperaba %q)",
		ErrInvalidWorkbook, s.name, name, s.header[col-1])
}

func matchesLegacyHeader(current []string, s sheetSchema) bool {
	if len(trimTrailingEmpty(current)) == 0 {
		return false
	}
	for _, legacy := range s.legacyHeaders {
		if isPrefixOf(current, legacy) {
			return true
		}
	}
	return false
}

// readHeader devuelve la fila 1 y si existe al menos una fila de datos, sin
// leer más filas de las necesarias.
func readHeader(f *excelize.File, sheet string) (header []string, hasData bool, err error) {
	rows, err := f.Rows(sheet)
	if err != nil {
		return nil, false, fmt.Errorf("%w: %v", ErrInvalidWorkbook, err)
	}
	defer rows.Close()

	for rowNum := 1; rows.Next(); rowNum++ {
		cells, err := rows.Columns(excelize.Options{RawCellValue: true})
		if err != nil {
			return nil, false, fmt.Errorf("%w: %v", ErrInvalidWorkbook, err)
		}
		if rowNum == headerRow {
			header = cells
			continue
		}
		if !isBlankRow(cells) {
			return header, true, nil
		}
	}
	return header, false, rows.Error()
}

// writeHeader escribe el encabezado del esquema, borra celdas sobrantes del
// encabezado anterior e inmoviliza la fila 1 para quien abra el archivo a mano.
func writeHeader(f *excelize.File, s sheetSchema, previous []string) error {
	values := make([]any, len(s.header))
	for i, v := range s.header {
		values[i] = v
	}
	if err := setRow(f, s.name, headerRow, values); err != nil {
		return err
	}
	for col := len(s.header) + 1; col <= len(previous); col++ {
		cell, _ := excelize.CoordinatesToCellName(col, headerRow)
		if err := f.SetCellValue(s.name, cell, nil); err != nil {
			return fmt.Errorf("%w: %v", ErrWriteFailed, err)
		}
	}
	err := f.SetPanes(s.name, &excelize.Panes{Freeze: true, YSplit: 1, TopLeftCell: "A2", ActivePane: "bottomLeft"})
	if err != nil {
		return fmt.Errorf("%w: %v", ErrWriteFailed, err)
	}
	return nil
}

func setRow(f *excelize.File, sheet string, rowNum int, values []any) error {
	if sheet == SheetInventory && rowNum > headerRow && len(values) > colEquipmentID {
		header, _, err := readHeader(f, sheet)
		if err != nil {
			return err
		}
		if len(header) < inventoryColumns {
			if err := writeHeader(f, inventorySchema, header); err != nil {
				return err
			}
		}
	}
	cell, err := excelize.CoordinatesToCellName(1, rowNum)
	if err == nil {
		err = f.SetSheetRow(sheet, cell, &values)
	}
	if err != nil {
		return fmt.Errorf("%w: %v", ErrWriteFailed, err)
	}
	return nil
}

// ============================================================
// Recorrido de filas
// ============================================================

// rowVisitor recibe cada fila de datos; devuelve stop=true para cortar la lectura.
type rowVisitor func(rowNum int, cells []string) (stop bool, err error)

// scanRows recorre las filas de datos de una hoja en orden, en streaming:
// valida el encabezado, saltea filas vacías y se detiene cuando visit lo pide.
func scanRows(f *excelize.File, s sheetSchema, minColumns int, visit rowVisitor) error {
	rows, err := f.Rows(s.name)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidWorkbook, err)
	}
	defer rows.Close()

	for rowNum := 1; rows.Next(); rowNum++ {
		cells, err := rows.Columns(excelize.Options{RawCellValue: true})
		if err != nil {
			return fmt.Errorf("%w: hoja %q, fila %d: %v", ErrInvalidWorkbook, s.name, rowNum, err)
		}
		if rowNum == headerRow {
			if err := checkHeader(s, cells, minColumns); err != nil {
				return err
			}
			continue
		}
		if isBlankRow(cells) {
			continue
		}
		if stop, err := visit(rowNum, cells); err != nil || stop {
			return err
		}
	}
	return rows.Error()
}

// checkHeader verifica que las primeras minColumns columnas coincidan con el
// esquema y que no haya columnas desconocidas. Leer con columnas corridas
// devolvería datos equivocados sin error.
func checkHeader(s sheetSchema, header []string, minColumns int) error {
	got := trimTrailingEmpty(header)
	if len(got) >= minColumns && isPrefixOf(got, s.header) {
		return nil
	}
	col := firstHeaderMismatch(got, s.header)
	if col == 0 { // todas las columnas conocidas coinciden: sobran columnas al final
		col = len(s.header) + 1
		name, _ := excelize.ColumnNumberToName(col)
		return fmt.Errorf("%w: encabezado de la hoja %q inválido: columna %s no esperada (%q)",
			ErrInvalidWorkbook, s.name, name, got[col-1])
	}
	name, _ := excelize.ColumnNumberToName(col)
	return fmt.Errorf("%w: encabezado de la hoja %q inválido en la columna %s (se esperaba %q)",
		ErrInvalidWorkbook, s.name, name, s.header[col-1])
}
