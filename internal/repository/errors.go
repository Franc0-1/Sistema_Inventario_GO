package repository

import (
	"errors"
	"fmt"

	"github.com/xuri/excelize/v2"
)

// Errores de almacenamiento. Se comparan con errors.Is; los servicios los
// reexportan para que los handlers no dependan de este paquete.
var (
	ErrItemNotFound    = errors.New("ítem no encontrado")
	ErrInvalidWorkbook = errors.New("el archivo de inventario no es un libro válido")
	ErrSheetMissing    = errors.New("falta una hoja requerida en el libro")
	ErrInvalidCell     = errors.New("celda con valor inválido")
	ErrInvalidItem     = errors.New("el ítem no puede guardarse")
	ErrInvalidMovement = errors.New("el movimiento no puede guardarse")
	ErrConflict        = errors.New("el ítem fue modificado por otra operación")
	ErrWriteFailed     = errors.New("no se pudo guardar el libro")
	ErrNotImplemented  = errors.New("operación no implementada")

	// Escritura segura (Etapa 8). En todos estos casos el original queda intacto.
	ErrBackupFailed  = errors.New("no se pudo crear el respaldo; no se modificó el inventario")
	ErrSaveFailed    = errors.New("no se pudo guardar o validar el archivo temporal; no se modificó el inventario")
	ErrReplaceFailed = errors.New("no se pudo reemplazar el archivo de inventario; no se modificó")

	// ErrInvalidMovementReference: un movimiento apunta a un ítem que nunca existió.
	ErrInvalidMovementReference = errors.New("el movimiento referencia un ítem inexistente")
)

// CellError indica exactamente qué celda no se pudo convertir, para poder
// corregirla a mano. errors.Is(err, ErrInvalidCell) es verdadero.
type CellError struct {
	Sheet  string
	Row    int    // 1-based, como en Excel
	Column int    // 1-based
	Field  string // nombre del encabezado
	Err    error  // causa de la conversión
}

func (e *CellError) Error() string {
	celda, _ := excelize.CoordinatesToCellName(e.Column, e.Row)
	return fmt.Sprintf("hoja %q, celda %s (%s): %v", e.Sheet, celda, e.Field, e.Err)
}

func (e *CellError) Unwrap() []error { return []error{ErrInvalidCell, e.Err} }
