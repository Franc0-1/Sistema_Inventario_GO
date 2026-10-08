package excelcell

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/xuri/excelize/v2"
)

// Conversiones seguras de celdas de Excel. Reciben el valor crudo de la celda
// (excelize.Options{RawCellValue: true}), nunca hacen panic y tratan la celda
// vacía como el valor cero del tipo.

// ParseInt acepta enteros ("12") y números con parte decimal nula ("12.0"),
// que es como Excel puede guardar una cantidad.
func ParseInt(valor string) (int, error) {
	valor = strings.TrimSpace(valor)
	if valor == "" {
		return 0, nil
	}
	if n, err := strconv.Atoi(valor); err == nil {
		return n, nil
	}
	f, err := strconv.ParseFloat(valor, 64)
	if err != nil || f != math.Trunc(f) || math.Abs(f) > math.MaxInt32 {
		return 0, fmt.Errorf("%q no es un número entero", valor)
	}
	return int(f), nil
}

var (
	valoresVerdaderos = map[string]bool{"si": true, "sí": true, "s": true, "yes": true, "y": true, "true": true, "verdadero": true, "1": true, "x": true}
	valoresFalsos     = map[string]bool{"no": true, "n": true, "false": true, "falso": true, "0": true}
)

// ParseBool acepta SI/NO (con o sin tilde), TRUE/FALSE, VERDADERO/FALSO,
// 1/0 (celdas booleanas de Excel) y X, sin distinguir mayúsculas.
func ParseBool(valor string) (bool, error) {
	v := strings.ToLower(strings.TrimSpace(valor))
	switch {
	case v == "":
		return false, nil
	case valoresVerdaderos[v]:
		return true, nil
	case valoresFalsos[v]:
		return false, nil
	}
	return false, fmt.Errorf("%q no es un valor booleano (SI/NO)", valor)
}

// Formatos de fecha aceptados como texto. Los que no tienen zona horaria se
// interpretan en la hora local del servidor.
var formatosFecha = []string{
	time.RFC3339Nano, // formato que escribe el sistema
	"2006-01-02 15:04:05",
	"2006-01-02T15:04:05",
	"2006-01-02",
	"02/01/2006 15:04",
	"02/01/2006",
}

// ParseTime acepta los formatos de texto de formatosFecha y fechas
// numéricas de Excel (número de serie, p. ej. "46275.5").
func ParseTime(valor string) (time.Time, error) {
	valor = strings.TrimSpace(valor)
	if valor == "" {
		return time.Time{}, nil
	}
	for _, formato := range formatosFecha {
		if t, err := time.ParseInLocation(formato, valor, time.Local); err == nil {
			return t, nil
		}
	}
	if serie, err := strconv.ParseFloat(valor, 64); err == nil && serie > 0 {
		return fechaDeSerieExcel(serie)
	}
	return time.Time{}, fmt.Errorf("%q no es una fecha válida", valor)
}

// fechaDeSerieExcel convierte un número de serie de Excel a hora local,
// redondeado al segundo (el número de serie arrastra error de coma flotante).
func fechaDeSerieExcel(serie float64) (time.Time, error) {
	t, err := excelize.ExcelDateToTime(serie, false)
	if err != nil {
		return time.Time{}, fmt.Errorf("%v no es una fecha de Excel válida", serie)
	}
	t = t.Round(time.Second)
	return time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), t.Second(), 0, time.Local), nil
}

// FormatTime es el formato con el que el sistema escribe fechas: RFC 3339
// con nanosegundos, para que UpdatedAt distinga dos escrituras en el mismo segundo.
func FormatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(time.RFC3339Nano)
}

// FormatBool escribe booleanos como SI/NO, legibles para quien abra el archivo.
func FormatBool(b bool) string {
	if b {
		return "SI"
	}
	return "NO"
}
