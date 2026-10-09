package models

import "time"

// Equipos y componentes del modelo nuevo. El estado usa los mismos valores
// que el sistema anterior (ItemStatus). Los campos marcados "solo lectura"
// los completa el repositorio.

// Equipo: PC, monitor, impresora, notebook… NumeroInventario vacío = pendiente
// de numerar. La oficina dice dónde está y la persona, quién lo tiene (pueden
// no coincidir con la oficina de la persona, p. ej. una notebook).
type Equipo struct {
	ID               int        `json:"id"`
	IDAnterior       int        `json:"id_anterior,omitempty"` // ID en el sistema anterior
	NumeroInventario string     `json:"numero_inventario"`
	TipoID           int        `json:"tipo_id"`
	Tipo             string     `json:"tipo"` // solo lectura
	Marca            string     `json:"marca"`
	Modelo           string     `json:"modelo"`
	NumeroSerie      string     `json:"numero_serie"`
	Estado           ItemStatus `json:"estado"`
	OficinaID        int        `json:"oficina_id"`
	Oficina          string     `json:"oficina"`    // solo lectura
	PersonaID        int        `json:"persona_id"` // 0 = sin asignar
	Persona          string     `json:"persona"`    // solo lectura
	Observacion      string     `json:"observacion"`
	FechaBaja        *time.Time `json:"fecha_baja,omitempty"`
	MotivoBaja       string     `json:"motivo_baja"`
	CreadoEn         time.Time  `json:"creado_en"`
	ActualizadoEn    time.Time  `json:"actualizado_en"`
	Version          string     `json:"version"`

	CantidadComponentes int          `json:"cantidad_componentes"`  // solo lectura
	PrestamoID          int          `json:"prestamo_id,omitempty"` // solo lectura: préstamo abierto
	PrestadoA           string       `json:"prestado_a,omitempty"`  // solo lectura
	Componentes         []Componente `json:"componentes,omitempty"` // solo en el detalle
}

// Componente: procesador, RAM, disco, fuente… Su N° de inventario es fijo
// (puede repetirse). Está en un equipo (EquipoID) o suelto en una oficina
// (OficinaID), nunca en los dos.
type Componente struct {
	ID               int        `json:"id"`
	IDAnterior       int        `json:"id_anterior,omitempty"`
	NumeroInventario string     `json:"numero_inventario"`
	TipoID           int        `json:"tipo_id"`
	Tipo             string     `json:"tipo"` // solo lectura
	Marca            string     `json:"marca"`
	Modelo           string     `json:"modelo"`
	NumeroSerie      string     `json:"numero_serie"`
	Estado           ItemStatus `json:"estado"`
	EquipoID         int        `json:"equipo_id"`  // 0 = fuera de un equipo
	Equipo           string     `json:"equipo"`     // solo lectura: N° de inventario del equipo
	OficinaID        int        `json:"oficina_id"` // solo si está fuera de un equipo
	Oficina          string     `json:"oficina"`    // solo lectura: la suya o la de su equipo
	Observacion      string     `json:"observacion"`
	FechaBaja        *time.Time `json:"fecha_baja,omitempty"`
	MotivoBaja       string     `json:"motivo_baja"`
	CreadoEn         time.Time  `json:"creado_en"`
	ActualizadoEn    time.Time  `json:"actualizado_en"`
	Version          string     `json:"version"`
}

// FiltroEquipos: los campos vacíos no filtran. Sin IncluirBajas (y sin pedir
// Estado BAJA) no aparecen los equipos dados de baja.
type FiltroEquipos struct {
	Texto        string // N° de inventario, serie, marca, modelo o tipo
	TipoID       int
	OficinaID    int
	PersonaID    int
	Estado       ItemStatus
	IncluirBajas bool
	Pendientes   bool // solo los que no tienen N° de inventario
}

// FiltroComponentes: OficinaID filtra por la oficina donde está (la suya o la
// de su equipo).
type FiltroComponentes struct {
	Texto        string
	TipoID       int
	EquipoID     int
	OficinaID    int
	Sueltos      bool // solo los que no están en un equipo
	IncluirBajas bool
}

// HistorialEquipo: un cambio de oficina, persona o estado. Anterior vacío = alta.
type HistorialEquipo struct {
	ID              int        `json:"id"`
	EquipoID        int        `json:"equipo_id"`
	OficinaAnterior string     `json:"oficina_anterior"`
	OficinaNueva    string     `json:"oficina_nueva"`
	PersonaAnterior string     `json:"persona_anterior"`
	PersonaNueva    string     `json:"persona_nueva"`
	Estado          ItemStatus `json:"estado"` // después del cambio
	Observacion     string     `json:"observacion"`
	Fecha           time.Time  `json:"fecha"`
	Usuario         string     `json:"usuario"`
}

// HistorialComponente: el componente pasó de un equipo a otro. EquipoID 0 =
// fuera de un equipo (en una oficina).
type HistorialComponente struct {
	ID               int       `json:"id"`
	ComponenteID     int       `json:"componente_id"`
	EquipoAnteriorID int       `json:"equipo_anterior_id"`
	EquipoAnterior   string    `json:"equipo_anterior"`
	EquipoNuevoID    int       `json:"equipo_nuevo_id"`
	EquipoNuevo      string    `json:"equipo_nuevo"`
	Observacion      string    `json:"observacion"`
	Fecha            time.Time `json:"fecha"`
	Usuario          string    `json:"usuario"`
}
