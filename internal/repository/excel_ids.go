package repository

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/xuri/excelize/v2"

	"inventario/internal/excelcell"
)

// ---- Secuencias de IDs ----
//
// "Mayor ID existente + 1" reutilizaría el ID si se borra la última fila.
// Por eso el libro guarda también el último ID asignado de cada hoja, como
// nombre definido (visible en Fórmulas > Administrador de nombres) para no
// tocar ninguna hoja. Próximo ID = max(mayor ID existente, último asignado) + 1.
//
// Estos métodos trabajan sobre un libro ya abierto dentro de write(): NO toman
// el mutex (tomarlo de nuevo sería un deadlock).

type idSequence struct {
	schema      sheetSchema
	minColumns  int
	definedName string
	comment     string
}

var (
	inventoryIDs = idSequence{inventorySchema, requiredInventoryColumns, "UltimoIDInventario",
		"Último ID asignado en la hoja Inventario. Evita reutilizar IDs eliminados. No modificar."}
	movementIDs = idSequence{movementSchema, movementColumns, "UltimoIDMovimiento",
		"Último ID asignado en la hoja Movimientos. No modificar."}
)

// next reserva el próximo ID y devuelve la fila donde escribir el registro
// (la siguiente a la última con datos).
func (s idSequence) next(f *excelize.File) (id, row int, err error) {
	maxID, lastRow, err := s.scan(f)
	if err != nil {
		return 0, 0, err
	}
	last, err := s.lastIssued(f)
	if err != nil {
		return 0, 0, err
	}
	id = max(maxID, last) + 1
	return id, lastRow + 1, s.setLastIssued(f, id)
}

// scan devuelve el mayor ID y la última fila con datos (o la del encabezado
// si la hoja está vacía). Un ID inválido es un error: calcular el próximo ID
// sobre datos dudosos podría duplicarlo.
func (s idSequence) scan(f *excelize.File) (maxID, lastRow int, err error) {
	lastRow = headerRow
	err = scanRows(f, s.schema, s.minColumns, func(rowNum int, cells []string) (bool, error) {
		id, err := rowID(s.schema, cells, rowNum)
		if err != nil {
			return true, err
		}
		maxID, lastRow = max(maxID, id), rowNum
		return false, nil
	})
	return maxID, lastRow, err
}

func (s idSequence) lastIssued(f *excelize.File) (int, error) {
	for _, dn := range f.GetDefinedName() {
		if dn.Name != s.definedName || dn.Scope != "Workbook" {
			continue
		}
		id, err := excelcell.ParseInt(strings.TrimPrefix(dn.RefersTo, "="))
		if err != nil || id < 0 {
			return 0, fmt.Errorf("%w: el nombre definido %s tiene un valor inválido (%q)", ErrInvalidWorkbook, s.definedName, dn.RefersTo)
		}
		return id, nil
	}
	return 0, nil
}

func (s idSequence) setLastIssued(f *excelize.File, id int) error {
	err := f.DeleteDefinedName(&excelize.DefinedName{Name: s.definedName})
	if err != nil && !errors.Is(err, excelize.ErrDefinedNameScope) { // ErrDefinedNameScope = todavía no existe
		return fmt.Errorf("%w: %v", ErrWriteFailed, err)
	}
	err = f.SetDefinedName(&excelize.DefinedName{Name: s.definedName, RefersTo: strconv.Itoa(id), Comment: s.comment})
	if err != nil {
		return fmt.Errorf("%w: %v", ErrWriteFailed, err)
	}
	return nil
}

// raise asegura que un ID ya usado (p. ej. uno que se va a borrar) no se reasigne.
func (s idSequence) raise(f *excelize.File, id int) error {
	last, err := s.lastIssued(f)
	if err != nil || id <= last {
		return err
	}
	return s.setLastIssued(f, id)
}
