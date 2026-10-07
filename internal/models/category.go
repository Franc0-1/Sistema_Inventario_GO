package models

// DeviceTypeAirConditioner tiene su propia sección en la interfaz, agrupada por área.
const DeviceTypeAirConditioner = "Aire acondicionado"

// Category es un tipo de dispositivo de la hoja "Categorias".
type Category struct {
	ID     int    `json:"id"`
	Name   string `json:"nombre"`
	Active bool   `json:"activa"`
	// Loanable indica si los ítems individuales de este tipo pueden prestarse.
	// Los equipos instalados (aires acondicionados, impresoras, switches…) no.
	Loanable bool `json:"prestable"`
}
