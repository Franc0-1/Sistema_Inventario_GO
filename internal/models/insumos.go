package models

import "time"

// Insumo: mouse, teclado, cables… Un solo stock, el de nuestra oficina. El
// stock solo cambia con movimientos (entrada, salida o ajuste).
type Insumo struct {
	ID            int       `json:"id"`
	IDAnterior    int       `json:"id_anterior,omitempty"`
	TipoID        int       `json:"tipo_id"`
	Tipo          string    `json:"tipo"` // solo lectura
	Marca         string    `json:"marca"`
	Modelo        string    `json:"modelo"`
	Stock         int       `json:"stock"` // solo se define en el alta (stock inicial)
	StockMinimo   int       `json:"stock_minimo"`
	BajoStock     bool      `json:"bajo_stock"` // solo lectura: hay un mínimo y el stock no lo supera
	Observacion   string    `json:"observacion"`
	Activo        bool      `json:"activo"`
	CreadoEn      time.Time `json:"creado_en"`
	ActualizadoEn time.Time `json:"actualizado_en"`
	Version       string    `json:"version"`
}

type TipoMovimientoInsumo string

const (
	MovimientoEntrada TipoMovimientoInsumo = "ENTRADA"
	MovimientoSalida  TipoMovimientoInsumo = "SALIDA" // entrega a una persona u oficina
	MovimientoAjuste  TipoMovimientoInsumo = "AJUSTE" // corrección después de contar
)

var TiposMovimientoInsumo = []TipoMovimientoInsumo{MovimientoEntrada, MovimientoSalida, MovimientoAjuste}

// MovimientoInsumo es inmutable. Cantidad: unidades que entran o salen; en un
// AJUSTE, la diferencia con signo respecto del stock anterior. Al registrar un
// ajuste se envía el stock contado (ver services.InsumoService).
type MovimientoInsumo struct {
	ID          int                  `json:"id"`
	InsumoID    int                  `json:"insumo_id"`
	Insumo      string               `json:"insumo"` // solo lectura: tipo, marca y modelo
	Tipo        TipoMovimientoInsumo `json:"tipo"`
	Cantidad    int                  `json:"cantidad"`
	PersonaID   int                  `json:"persona_id"`
	Persona     string               `json:"persona"` // solo lectura
	OficinaID   int                  `json:"oficina_id"`
	Oficina     string               `json:"oficina"` // solo lectura
	Observacion string               `json:"observacion"`
	Fecha       time.Time            `json:"fecha"`
	Usuario     string               `json:"usuario"` // solo lectura
}

type FiltroInsumos struct {
	Texto       string // tipo, marca o modelo
	TipoID      int
	SoloActivos bool
	BajoStock   bool
}

// FiltroMovimientosInsumo: Desde y Hasta son días completos (cero = sin
// límite). Limite acota la cantidad de filas, de la más nueva a la más vieja.
type FiltroMovimientosInsumo struct {
	InsumoID  int
	PersonaID int
	OficinaID int
	Tipo      TipoMovimientoInsumo
	Desde     time.Time
	Hasta     time.Time
	Limite    int
}
