// Package utils reúne utilidades transversales sin lógica de negocio:
// configuración y conversión de celdas.
package utils

import (
	"os"
	"strings"
)

// Config se carga desde variables de entorno (compatible con Docker/Proxmox).
type Config struct {
	Puerto        string // INVENTARIO_PUERTO     (default 8080)
	RutaExcel     string // INVENTARIO_EXCEL      (default data/inventario.xlsx)
	RutaTemplates string // INVENTARIO_TEMPLATES  (default templates/web)

	// CORSOrigins son los orígenes externos autorizados a llamar a la API,
	// separados por coma en INVENTARIO_CORS_ORIGINS
	// (p. ej. "http://localhost:5173,http://192.168.1.50:3000").
	// Vacío (default): sin CORS, porque el frontend lo sirve este mismo servidor.
	CORSOrigins []string
}

func CargarConfig() Config {
	return Config{
		Puerto:        entorno("INVENTARIO_PUERTO", "8080"),
		RutaExcel:     entorno("INVENTARIO_EXCEL", "data/inventario.xlsx"),
		RutaTemplates: entorno("INVENTARIO_TEMPLATES", "templates/web"),
		CORSOrigins:   lista(os.Getenv("INVENTARIO_CORS_ORIGINS")),
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
