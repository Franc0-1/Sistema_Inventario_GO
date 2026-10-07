// Package models contiene las entidades del dominio. No depende de ningún
// otro paquete interno ni de la tecnología de almacenamiento.
//
// Los nombres de campo están en inglés (especificación de la Etapa 2); los
// tags JSON se mantienen en español porque son el contrato con el frontend.
package models

import "time"

// ItemStatus es la condición física/operativa del ítem.
type ItemStatus string

const (
	StatusOperational  ItemStatus = "OPERATIVO"
	StatusInRepair     ItemStatus = "EN_REPARACION"
	StatusOutOfService ItemStatus = "FUERA_DE_SERVICIO"
	StatusRetired      ItemStatus = "BAJA" // baja lógica: la fila nunca se elimina
)

// ValidStatuses lista los estados aceptados por la capa de servicios.
var ValidStatuses = []ItemStatus{
	StatusOperational, StatusInRepair, StatusOutOfService, StatusRetired,
}

// Availability indica si un ítem individual está en depósito o prestado.
// Es independiente de ItemStatus: un ítem OPERATIVO puede estar PRESTADO.
// Los consumibles (HasInventory = false) siempre están DISPONIBLE.
//
// Los cuatro estados de negocio se expresan combinando ambos campos:
//
//	Disponible = Status OPERATIVO     + Availability DISPONIBLE
//	Prestado   = Availability PRESTADO
//	Reparación = Status EN_REPARACION (o FUERA_DE_SERVICIO si no tiene arreglo)
//	Baja       = Status BAJA
type Availability string

const (
	Available Availability = "DISPONIBLE"
	Loaned    Availability = "PRESTADO"
)

// Item es una fila de la hoja "Inventario".
//
// HasInventory distingue los dos tipos de ítem:
//   - true:  equipo individual (N° de inventario, serie, Quantity = 1, se presta).
//   - false: consumible/material en stock (sin N°, Quantity variable, se entrega).
//
// UpdatedAt funciona además como versión para el control de concurrencia
// optimista de las escrituras.
type Item struct {
	ID              int        `json:"id"`
	InventoryNumber string     `json:"numero_inventario"`
	HasInventory    bool       `json:"tiene_inventario"`
	DeviceType      string     `json:"tipo_dispositivo"` // referencia a Category.Name
	Brand           string     `json:"marca"`
	Model           string     `json:"modelo"`
	SerialNumber    string     `json:"numero_serie"`
	Quantity        int        `json:"cantidad"`
	Location        string     `json:"ubicacion"` // la interfaz la muestra como "Área"
	Status          ItemStatus `json:"estado"`
	Notes           string     `json:"observacion"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`

	// Extensión de préstamos (columnas N–P de la hoja).
	Availability Availability `json:"disponibilidad"`
	AssignedTo   string       `json:"asignado_a"`               // vacío si está DISPONIBLE
	LoanedAt     *time.Time   `json:"fecha_prestamo,omitempty"` // nil si está DISPONIBLE
}

// ItemFilter agrupa los criterios de búsqueda. Los campos vacíos no filtran
// y los que tienen valor se combinan (Y lógico).
//
// Los textos se comparan sin distinguir mayúsculas ni tildes y sin importar
// los espacios de más:
//   - DeviceType, ExcludeDeviceType y Location: valor exacto (son listas cerradas).
//   - Brand, Model, InventoryNumber y SerialNumber: coincidencia parcial.
//
// Sin Status ni IncludeRetired se excluyen las bajas (Status BAJA); filtrar
// por Status BAJA las muestra.
type ItemFilter struct {
	// Text busca en InventoryNumber, Brand, Model, DeviceType y SerialNumber.
	Text              string
	DeviceType        string
	ExcludeDeviceType string // p. ej. la lista general sin los aires acondicionados
	Brand             string
	Model             string
	InventoryNumber   string
	SerialNumber      string
	Location          string
	Status            ItemStatus
	Availability      Availability
	HasInventory      *bool // nil: ambos tipos de ítem
	OnlyConsumables   bool  // equivale a HasInventory = false
	IncludeRetired    bool
}
