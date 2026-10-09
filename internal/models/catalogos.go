package models

import "strings"

// Catálogos del modelo nuevo (DER del 2026-10-09). Version es la RowVersion
// de la fila: cambia con cada modificación y se envía al actualizar, así dos
// personas no se pisan los cambios (concurrencia optimista).

// TablaCatalogo identifica los catálogos que solo tienen nombre y estado.
type TablaCatalogo string

const (
	CatalogoOficinas TablaCatalogo = "Oficinas"
	CatalogoPuestos  TablaCatalogo = "Puestos" // cargo de la persona
	CatalogoUsuarios TablaCatalogo = "Usuarios"
)

// Catalogo es una oficina, un puesto o un usuario del sistema. Nada se
// elimina: lo que ya no se usa se desactiva.
type Catalogo struct {
	ID      int    `json:"id"`
	Nombre  string `json:"nombre"`
	Activo  bool   `json:"activo"`
	Version string `json:"version"`
}

// Persona tiene su puesto en una oficina. Puesto y Oficina son de solo
// lectura: los completa el repositorio a partir de los IDs.
type Persona struct {
	ID        int    `json:"id"`
	Nombre    string `json:"nombre"`
	Apellido  string `json:"apellido"`
	PuestoID  int    `json:"puesto_id"` // 0 = sin puesto
	Puesto    string `json:"puesto"`
	OficinaID int    `json:"oficina_id"`
	Oficina   string `json:"oficina"`
	Activo    bool   `json:"activo"`
	Version   string `json:"version"`
}

func (p Persona) NombreCompleto() string {
	return strings.TrimSpace(p.Nombre + " " + p.Apellido)
}

// FiltroPersonas: los campos vacíos no filtran. Texto busca en nombre y
// apellido sin distinguir mayúsculas ni tildes.
type FiltroPersonas struct {
	OficinaID   int
	Texto       string
	SoloActivas bool
}

// Clase dice qué se registra con un tipo. No cambia una vez que el tipo tiene
// ítems cargados (lo garantiza la base).
type Clase string

const (
	ClaseEquipo     Clase = "EQUIPO"
	ClaseComponente Clase = "COMPONENTE"
	ClaseInsumo     Clase = "INSUMO"
)

var ClasesValidas = []Clase{ClaseEquipo, ClaseComponente, ClaseInsumo}

// Tipo de equipo, componente o insumo (PC, Monitor, RAM, Mouse…). Solo los
// tipos de equipo pueden ser prestables.
type Tipo struct {
	ID        int    `json:"id"`
	Nombre    string `json:"nombre"`
	Clase     Clase  `json:"clase"`
	Prestable bool   `json:"prestable"`
	Activo    bool   `json:"activo"`
	Version   string `json:"version"`
}
