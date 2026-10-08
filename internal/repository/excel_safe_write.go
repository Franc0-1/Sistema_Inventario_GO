package repository

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/xuri/excelize/v2"
)

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
