package repository

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/xuri/excelize/v2"

	"inventario/internal/models"
	"inventario/internal/utils"
)

// ExcelRepository implementa Repository sobre un único archivo .xlsx.
//
//   - Lecturas: bloqueo compartido (RLock), abrir, validar la estructura, leer,
//     cerrar. Sin caché: el archivo es la única fuente de verdad.
//   - Escrituras: ver write(). Nunca se escribe directamente sobre el original.
//
// Métodos públicos: toman el mutex. Helpers internos (validate*, idSequence,
// append*, backup, commit): trabajan sobre un libro ya abierto y NO lo toman.
type ExcelRepository struct {
	path      string
	backupDir string // data/backups junto al Excel
	mu        sync.RWMutex
	now       func() time.Time // reloj inyectable para tests

	// replaceFile reemplaza el original por el temporal ya validado (os.Rename).
	// Es un campo solo para que los tests puedan simular un fallo de reemplazo.
	replaceFile func(src, dst string) error
}

var _ Repository = (*ExcelRepository)(nil)

// NewExcelRepository prepara el libro en path: lo crea si no existe, o
// agrega hojas y encabezados faltantes sin mover datos. Si un libro con datos
// tiene encabezados incompatibles devuelve ErrInvalidWorkbook y no lo toca.
// Los respaldos se guardan en la carpeta "backups" junto al libro.
func NewExcelRepository(path string) (*ExcelRepository, error) {
	r := &ExcelRepository{
		path:        path,
		backupDir:   filepath.Join(filepath.Dir(path), "backups"),
		now:         time.Now,
		replaceFile: os.Rename,
	}
	if err := r.prepareWorkbook(); err != nil {
		return nil, err
	}
	return r, nil
}

func (r *ExcelRepository) Close() error { return nil }

// ============================================================
// Apertura segura y ciclo de vida del libro
// ============================================================

// openWorkbook abre el libro y verifica las hojas requeridas. Si devuelve
// error ya liberó los recursos; si no, el llamador debe llamar a Close.
func openWorkbook(path string) (*excelize.File, error) {
	f, err := excelize.OpenFile(path)
	if err != nil {
		return nil, fmt.Errorf("%w: %s: %v", ErrInvalidWorkbook, path, err)
	}
	if err := requireSheets(f, requiredSheets...); err != nil {
		f.Close()
		return nil, err
	}
	return f, nil
}

func requireSheets(f *excelize.File, names ...string) error {
	for _, name := range names {
		if idx, err := f.GetSheetIndex(name); err != nil || idx == -1 {
			return fmt.Errorf("%w: %q", ErrSheetMissing, name)
		}
	}
	return nil
}

// read ejecuta fn con el libro abierto bajo bloqueo compartido y lo cierra al
// terminar. Valida la estructura (hojas y encabezados); la validación completa
// de datos corre antes de cada escritura y en Check, para que un dato
// inválido no impida consultar el inventario para corregirlo.
func (r *ExcelRepository) read(ctx context.Context, fn func(f *excelize.File) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()

	f, err := openWorkbook(r.path)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := validateStructure(f); err != nil {
		return err
	}
	return fn(f)
}

// write es el ÚNICO camino de escritura. Todo ocurre bajo el mismo Lock:
//
//	abrir y validar el original (estructura + datos)
//	→ modificar en memoria (fn)
//	→ respaldar el original en backups/
//	→ guardar en un temporal único junto al original
//	→ reabrir y validar el temporal
//	→ reemplazar el original → eliminar el temporal
//
// Si cualquier paso falla, el original queda intacto. El respaldo se hace
// después de fn para no generar copias de operaciones rechazadas (ítem
// inexistente, conflicto de versión): igual se hace antes de tocar el disco.
func (r *ExcelRepository) write(ctx context.Context, fn func(f *excelize.File) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	f, err := openWorkbook(r.path)
	if err != nil {
		log.Printf("inventario: no se pudo abrir el libro para escribir: %v", err)
		return err
	}
	defer f.Close()

	if err := validateWorkbook(f); err != nil {
		log.Printf("inventario: el libro no es válido; se cancela la escritura: %v", err)
		return err
	}
	if err := fn(f); err != nil {
		return err
	}
	if err := r.backup(); err != nil {
		log.Printf("inventario: %v", err)
		return err
	}
	if err := r.commit(f, validateWorkbook); err != nil {
		log.Printf("inventario: %v", err)
		return err
	}
	return nil
}

// Check verifica que el libro sea accesible y que su estructura sea válida
// (hojas y encabezados). No escribe ni recorre todos los datos.
func (r *ExcelRepository) Check(ctx context.Context) error {
	return r.read(ctx, func(*excelize.File) error { return nil })
}

// ============================================================
// Respaldo y reemplazo seguro (sin lock: se llaman con el Lock tomado)
// ============================================================

// backup copia el original a backups/<nombre>_AAAA-MM-DD_HH-MM-SS.xlsx. Nunca
// sobrescribe: si ya hay uno con esa fecha (dos escrituras en el mismo
// segundo), agrega _2, _3… Si falla, la escritura se cancela.
func (r *ExcelRepository) backup() error {
	if err := os.MkdirAll(r.backupDir, 0o755); err != nil {
		return fmt.Errorf("%w: %v", ErrBackupFailed, err)
	}
	src, err := os.Open(r.path)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrBackupFailed, err)
	}
	defer src.Close()

	ext := filepath.Ext(r.path)
	base := fmt.Sprintf("%s_%s", strings.TrimSuffix(filepath.Base(r.path), ext), r.now().Format("2006-01-02_15-04-05"))
	for n := 1; n <= 999; n++ {
		name := base + ext
		if n > 1 {
			name = fmt.Sprintf("%s_%d%s", base, n, ext)
		}
		dst, err := os.OpenFile(filepath.Join(r.backupDir, name), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if errors.Is(err, fs.ErrExist) {
			continue
		}
		if err != nil {
			return fmt.Errorf("%w: %v", ErrBackupFailed, err)
		}
		if err := copyAndSync(dst, src); err != nil {
			os.Remove(dst.Name())
			return fmt.Errorf("%w: %v", ErrBackupFailed, err)
		}
		return nil
	}
	return fmt.Errorf("%w: demasiados respaldos con la misma fecha y hora", ErrBackupFailed)
}

func copyAndSync(dst *os.File, src io.Reader) error {
	_, err := io.Copy(dst, src)
	if err == nil {
		err = dst.Sync()
	}
	return errors.Join(err, dst.Close())
}

// commit guarda f en un temporal único junto al original, lo reabre y lo
// valida con validate, y recién entonces reemplaza el original. El temporal
// se elimina siempre (tras un reemplazo exitoso ya no existe).
func (r *ExcelRepository) commit(f *excelize.File, validate func(*excelize.File) error) error {
	tmp, err := os.CreateTemp(filepath.Dir(r.path), "."+filepath.Base(r.path)+".*.tmp")
	if err != nil {
		return fmt.Errorf("%w: crear el temporal: %v", ErrSaveFailed, err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	// No se usa f.SaveAs: excelize valida la extensión y rechaza ".tmp".
	if err := writeAndSync(f, tmp); err != nil {
		return fmt.Errorf("%w: %v", ErrSaveFailed, err)
	}
	if err := validateFile(tmpPath, validate); err != nil {
		return fmt.Errorf("%w: el archivo guardado no es válido: %w", ErrSaveFailed, err)
	}
	if err := r.replaceFile(tmpPath, r.path); err != nil {
		return fmt.Errorf("%w: %v", ErrReplaceFailed, err)
	}
	return nil
}

func writeAndSync(f *excelize.File, file *os.File) error {
	_, err := f.WriteTo(file)
	if err == nil {
		err = file.Sync()
	}
	return errors.Join(err, file.Close())
}

// validateFile reabre desde disco lo que se acaba de guardar y lo valida.
func validateFile(path string, validate func(*excelize.File) error) error {
	f, err := excelize.OpenFile(path)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidWorkbook, err)
	}
	defer f.Close()
	return validate(f)
}

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
		if rowID, err := utils.ParseCellInt(cellAt(cells, colID)); err != nil || rowID != id {
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
// InventoryRepository: escritura (todas pasan por write())
// ============================================================

func (r *ExcelRepository) Create(ctx context.Context, item models.Item) (models.Item, error) {
	if err := checkStorable(item); err != nil {
		return models.Item{}, err
	}
	err := r.write(ctx, func(f *excelize.File) error {
		id, row, err := inventoryIDs.next(f)
		if err != nil {
			return err
		}
		item.ID = id
		item.CreatedAt = r.timestamp(time.Time{})
		item.UpdatedAt = item.CreatedAt
		return setRow(f, SheetInventory, row, itemToRow(item))
	})
	if err != nil {
		return models.Item{}, fmt.Errorf("crear ítem: %w", err)
	}
	return item, nil
}

func (r *ExcelRepository) Update(ctx context.Context, item models.Item, expectedVersion time.Time, mov *models.Movement) (models.Item, error) {
	if err := checkStorable(item); err != nil {
		return models.Item{}, err
	}
	err := r.write(ctx, func(f *excelize.File) error {
		rowNum, current, err := findItemRow(f, item.ID)
		if err != nil {
			return err
		}
		if err := checkVersion(current, expectedVersion); err != nil {
			return err
		}
		item.CreatedAt = current.CreatedAt
		item.UpdatedAt = r.timestamp(current.UpdatedAt)
		if err := setRow(f, SheetInventory, rowNum, itemToRow(item)); err != nil {
			return err
		}
		if mov == nil {
			return nil
		}
		_, err = appendStockMovement(f, *mov, current, item)
		return err
	})
	if err != nil {
		return models.Item{}, fmt.Errorf("actualizar ítem %d: %w", item.ID, err)
	}
	return item, nil
}

func (r *ExcelRepository) Delete(ctx context.Context, id int, expectedVersion time.Time) error {
	err := r.write(ctx, func(f *excelize.File) error {
		rowNum, current, err := findItemRow(f, id)
		if err != nil {
			return err
		}
		if err := checkVersion(current, expectedVersion); err != nil {
			return err
		}
		// Se registra antes de borrar: si era el mayor ID, no debe volver a asignarse.
		// La hoja Movimientos no se toca: el historial del ítem se conserva.
		if err := inventoryIDs.raise(f, id); err != nil {
			return err
		}
		if err := f.RemoveRow(SheetInventory, rowNum); err != nil {
			return fmt.Errorf("%w: %v", ErrWriteFailed, err)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("eliminar ítem %d: %w", id, err)
	}
	return nil
}

// UpdateStock modifica "Inventario" y agrega el movimiento en memoria, y recién
// entonces write() guarda el libro una sola vez: si cualquier paso falla no se
// guarda nada, así que nunca queda stock sin movimiento ni movimiento sin stock.
func (r *ExcelRepository) UpdateStock(ctx context.Context, id, quantity int, expectedVersion time.Time, mov models.Movement) (models.Item, error) {
	if quantity < 0 {
		return models.Item{}, fmt.Errorf("%w: la cantidad no puede ser negativa (%d)", ErrInvalidItem, quantity)
	}
	var updated models.Item
	err := r.write(ctx, func(f *excelize.File) error {
		rowNum, current, err := findItemRow(f, id)
		if err != nil {
			return err
		}
		if err := checkVersion(current, expectedVersion); err != nil {
			return err
		}
		updated = current
		updated.Quantity = quantity
		updated.UpdatedAt = r.timestamp(current.UpdatedAt)
		// Solo estas dos celdas: el resto de la fila queda exactamente como estaba.
		if err := setInventoryCell(f, rowNum, colQuantity, updated.Quantity); err != nil {
			return err
		}
		if err := setInventoryCell(f, rowNum, colUpdatedAt, utils.FormatCellTime(updated.UpdatedAt)); err != nil {
			return err
		}
		_, err = appendStockMovement(f, mov, current, updated)
		return err
	})
	if err != nil {
		return models.Item{}, fmt.Errorf("actualizar stock del ítem %d: %w", id, err)
	}
	return updated, nil
}

// ---- Helpers de escritura ----

// checkStorable verifica invariantes de almacenamiento (no reglas de negocio).
func checkStorable(item models.Item) error {
	if item.Quantity < 0 {
		return fmt.Errorf("%w: la cantidad no puede ser negativa (%d)", ErrInvalidItem, item.Quantity)
	}
	return nil
}

func checkVersion(current models.Item, expected time.Time) error {
	if !current.UpdatedAt.Equal(expected) {
		return fmt.Errorf("%w: id %d (versión leída %s, guardada %s)", ErrConflict, current.ID,
			utils.FormatCellTime(expected), utils.FormatCellTime(current.UpdatedAt))
	}
	return nil
}

// timestamp devuelve la hora actual, siempre posterior a prev: UpdatedAt es la
// versión del ítem y dos escrituras seguidas nunca deben compartirla.
func (r *ExcelRepository) timestamp(prev time.Time) time.Time {
	t := r.now().Round(0) // sin lectura monotónica, igual a lo que se relee del Excel
	if !t.After(prev) {
		t = prev.Add(time.Nanosecond)
	}
	return t
}

func setInventoryCell(f *excelize.File, rowNum, col int, value any) error {
	cell, err := excelize.CoordinatesToCellName(col+1, rowNum)
	if err == nil {
		err = f.SetCellValue(SheetInventory, cell, value)
	}
	if err != nil {
		return fmt.Errorf("%w: %v", ErrWriteFailed, err)
	}
	return nil
}

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
		id, err := utils.ParseCellInt(strings.TrimPrefix(dn.RefersTo, "="))
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

// ============================================================
// MovementRepository
// ============================================================

func (r *ExcelRepository) GetMovements(ctx context.Context) ([]models.Movement, error) {
	return r.readMovements(ctx, func(models.Movement) bool { return true })
}

func (r *ExcelRepository) GetMovementsByItemID(ctx context.Context, itemID int) ([]models.Movement, error) {
	return r.readMovements(ctx, func(m models.Movement) bool { return m.ItemID == itemID })
}

func (r *ExcelRepository) CreateMovement(ctx context.Context, mov models.Movement) (models.Movement, error) {
	if err := checkMovementStorable(mov); err != nil {
		return models.Movement{}, err
	}
	var created models.Movement
	err := r.write(ctx, func(f *excelize.File) error {
		mov.CreatedAt = r.timestamp(time.Time{})
		var err error
		created, err = appendMovement(f, mov)
		return err
	})
	if err != nil {
		return models.Movement{}, fmt.Errorf("registrar movimiento: %w", err)
	}
	return created, nil
}

// readMovements lee la hoja completa y devuelve los movimientos que cumplen
// keep, ordenados por ID ascendente.
func (r *ExcelRepository) readMovements(ctx context.Context, keep func(models.Movement) bool) ([]models.Movement, error) {
	movements := []models.Movement{}
	err := r.read(ctx, func(f *excelize.File) error {
		return scanRows(f, movementSchema, movementColumns, func(rowNum int, cells []string) (bool, error) {
			mov, err := rowToMovement(cells, rowNum)
			if err != nil {
				return true, err
			}
			if keep(mov) {
				movements = append(movements, mov)
			}
			return false, nil
		})
	})
	if err != nil {
		return nil, err
	}
	slices.SortFunc(movements, func(a, b models.Movement) int { return a.ID - b.ID })
	return movements, nil
}

// appendStockMovement completa el movimiento con lo que realmente se escribió
// en "Inventario" (ítem, cantidades y fecha) y lo agrega. Sin lock: se llama
// dentro de write().
func appendStockMovement(f *excelize.File, mov models.Movement, before, after models.Item) (models.Movement, error) {
	mov.ItemID = after.ID
	mov.PreviousQuantity, mov.NewQuantity = before.Quantity, after.Quantity
	mov.CreatedAt = after.UpdatedAt
	if err := checkMovementStorable(mov); err != nil {
		return models.Movement{}, err
	}
	if !matchesChange(mov) {
		return models.Movement{}, fmt.Errorf("%w: %s de %d no coincide con el cambio guardado (%d -> %d)",
			ErrInvalidMovement, mov.MovementType, mov.Quantity, mov.PreviousQuantity, mov.NewQuantity)
	}
	return appendMovement(f, mov)
}

// matchesChange verifica que el movimiento describa el cambio de cantidad que
// realmente se escribe en "Inventario", para que el historial nunca lo
// contradiga. Como el error cancela write(), tampoco se guarda el ítem.
func matchesChange(mov models.Movement) bool {
	diff := mov.NewQuantity - mov.PreviousQuantity
	switch mov.MovementType {
	case models.MovementStockIn:
		return diff > 0 && mov.Quantity == diff
	case models.MovementStockOut:
		return diff < 0 && mov.Quantity == -diff
	case models.MovementStockUpdate:
		return diff == 0 && mov.Quantity == 0
	case models.MovementAdjustment:
		return mov.Quantity == max(diff, -diff)
	case models.MovementTransfer:
		return diff == 0
	}
	return false
}

// appendMovement asigna el ID y agrega la fila al final de "Movimientos".
// Sin lock: se llama dentro de write().
func appendMovement(f *excelize.File, mov models.Movement) (models.Movement, error) {
	id, row, err := movementIDs.next(f)
	if err != nil {
		return models.Movement{}, err
	}
	mov.ID = id
	if err := setRow(f, SheetMovements, row, movementToRow(mov)); err != nil {
		return models.Movement{}, err
	}
	return mov, nil
}

// checkMovementStorable verifica invariantes de almacenamiento del historial.
func checkMovementStorable(mov models.Movement) error {
	switch {
	case mov.ItemID <= 0:
		return fmt.Errorf("%w: ItemID inválido (%d)", ErrInvalidMovement, mov.ItemID)
	case mov.MovementType == "":
		return fmt.Errorf("%w: falta el tipo de movimiento", ErrInvalidMovement)
	case mov.Quantity < 0 || mov.PreviousQuantity < 0 || mov.NewQuantity < 0:
		return fmt.Errorf("%w: cantidades negativas", ErrInvalidMovement)
	}
	return nil
}

// ============================================================
// Pendiente: categorías
// ============================================================

func (r *ExcelRepository) ListCategories(ctx context.Context) ([]models.Category, error) {
	return nil, ErrNotImplemented
}
