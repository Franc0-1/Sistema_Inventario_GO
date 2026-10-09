// Package config carga la configuración desde variables de entorno.
package config

import (
	"fmt"
	"os"
	"strings"
	"time"
)

// Config se carga desde variables de entorno (compatible con Docker/Proxmox).
type Config struct {
	Puerto        string // INVENTARIO_PUERTO     (default 8080)
	RutaExcel     string // INVENTARIO_EXCEL      (default data/inventario.xlsx)
	RutaTemplates string // INVENTARIO_TEMPLATES  (default templates/web)

	// Almacenamiento: "excel" (default) o "sqlserver". Con sqlserver, DBDSN es
	// la cadena de conexión, p. ej.
	// "server=192.168.106.84;port=1433;database=Inventario;user id=inventario_app;password=...;encrypt=true;TrustServerCertificate=true".
	// Contiene la contraseña: se pasa por variable de entorno y nunca se registra.
	Almacenamiento string // INVENTARIO_STORAGE
	DBDSN          string // INVENTARIO_DB_DSN

	// CORSOrigins son los orígenes externos autorizados a llamar a la API,
	// separados por coma en INVENTARIO_CORS_ORIGINS
	// (p. ej. "http://localhost:5173,http://192.168.1.50:3000").
	// Vacío (default): sin CORS, porque el frontend lo sirve este mismo servidor.
	CORSOrigins []string

	// HoraSalida (HH:MM) es la hora de fin de la jornada: los préstamos del
	// día vencen a esa hora salvo que se indique otra. INVENTARIO_HORA_SALIDA.
	HoraSalida string
}

func Cargar() Config {
	return Config{
		Puerto:         entorno("INVENTARIO_PUERTO", "8080"),
		RutaExcel:      entorno("INVENTARIO_EXCEL", "data/inventario.xlsx"),
		RutaTemplates:  entorno("INVENTARIO_TEMPLATES", "templates/web"),
		Almacenamiento: strings.ToLower(entorno("INVENTARIO_STORAGE", "excel")),
		DBDSN:          os.Getenv("INVENTARIO_DB_DSN"),
		CORSOrigins:    lista(os.Getenv("INVENTARIO_CORS_ORIGINS")),
		HoraSalida:     entorno("INVENTARIO_HORA_SALIDA", "13:00"),
	}
}

func entorno(clave, porDefecto string) string {
	if v := os.Getenv(clave); v != "" {
		return v
	}
	return porDefecto
}

func lista(valor string) []string {
	var out []string
	for _, v := range strings.Split(valor, ",") {
		if v = strings.TrimSpace(v); v != "" {
			out = append(out, v)
		}
	}
	return out
}

// HoraDeSalida devuelve HoraSalida como tiempo desde la medianoche.
func (c Config) HoraDeSalida() (time.Duration, error) {
	t, err := time.Parse("15:04", strings.TrimSpace(c.HoraSalida))
	if err != nil {
		return 0, fmt.Errorf("INVENTARIO_HORA_SALIDA=%q debe tener el formato HH:MM", c.HoraSalida)
	}
	return time.Duration(t.Hour())*time.Hour + time.Duration(t.Minute())*time.Minute, nil
}
