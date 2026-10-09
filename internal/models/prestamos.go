package models

import "time"

// Prestamo de un equipo en el día. No cambia la persona ni la oficina
// asignadas al equipo. FechaDevolucion nil = todavía no se devolvió.
type Prestamo struct {
	ID                 int        `json:"id"`
	EquipoID           int        `json:"equipo_id"`
	Equipo             string     `json:"equipo"`             // solo lectura: N° de inventario
	EquipoDescripcion  string     `json:"equipo_descripcion"` // solo lectura: tipo, marca y modelo
	PersonaID          int        `json:"persona_id"`
	Persona            string     `json:"persona"` // solo lectura
	Oficina            string     `json:"oficina"` // solo lectura: la de la persona
	FechaSalida        time.Time  `json:"fecha_salida"`
	DevolucionPrevista time.Time  `json:"devolucion_prevista"`
	FechaDevolucion    *time.Time `json:"fecha_devolucion,omitempty"`
	Vencido            bool       `json:"vencido"` // solo lectura: abierto y pasada la devolución prevista
	Observacion        string     `json:"observacion"`
	Usuario            string     `json:"usuario"` // solo lectura: quién lo registró
}

// FiltroPrestamos: Vencidos implica Abiertos. Limite acota las filas, de la
// más nueva a la más vieja.
type FiltroPrestamos struct {
	EquipoID  int
	PersonaID int
	Abiertos  bool
	Vencidos  bool
	Limite    int
}
