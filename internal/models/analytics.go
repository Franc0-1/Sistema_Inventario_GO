package models

type InventoryGroup struct {
	Value   string `json:"valor"`
	Records int    `json:"registros"`
	Units   int    `json:"unidades"`
}

type InventoryReport struct {
	Records     int              `json:"registros"`
	Units       int              `json:"unidades"`
	Locations   []InventoryGroup `json:"ubicaciones"`
	DeviceTypes []InventoryGroup `json:"tipos"`
	Statuses    []InventoryGroup `json:"condiciones"`
}

type InventorySummary struct {
	ActiveRecords      int `json:"registros_activos"`
	RetiredRecords     int `json:"registros_baja"`
	ActiveUnits        int `json:"unidades_activas"`
	AvailableEquipment int `json:"equipos_disponibles"`
	LoanedEquipment    int `json:"equipos_prestados"`
	InventoryReport
	RecentMovements []Movement `json:"movimientos"`
}
