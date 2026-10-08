package repository

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/xuri/excelize/v2"
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
