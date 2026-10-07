package models

import (
	"fmt"
	"strings"
)

type Equipment struct {
	Cabinet    Item   `json:"gabinete"`
	Components []Item `json:"componentes"`
}

func IsCabinet(item Item) bool {
	switch strings.ToUpper(strings.TrimSpace(item.DeviceType)) {
	case "CPU", "GABINETE", "PC":
		return true
	}
	return false
}

func IsEquipment(item Item) bool {
	return IsCabinet(item) && item.EquipmentID == 0 && item.HasInventory && item.Quantity == 1
}

func EquipmentRoot(item Item) int {
	if item.EquipmentID != 0 {
		return item.EquipmentID
	}
	if IsEquipment(item) {
		return item.ID
	}
	return 0
}

func ShareEquipment(a, b Item) bool {
	root := EquipmentRoot(a)
	return root > 0 && root == EquipmentRoot(b)
}

type EquipmentError struct {
	ItemID int
	Reason string
}

func (e *EquipmentError) Error() string { return fmt.Sprintf("elemento #%d: %s", e.ItemID, e.Reason) }

// Se valida el conjunto completo para rechazar referencias rotas y ciclos.
func ValidateEquipment(items []Item) error {
	byID := make(map[int]Item, len(items))
	for _, item := range items {
		byID[item.ID] = item
	}
	for _, item := range items {
		if item.EquipmentID == 0 {
			continue
		}
		problem := ""
		parent, exists := byID[item.EquipmentID]
		switch {
		case item.EquipmentID < 0:
			problem = "el ID del equipo no puede ser negativo"
		case item.EquipmentID == item.ID:
			problem = "un elemento no puede pertenecer a si mismo"
		case !exists:
			problem = "el gabinete indicado no existe"
		case !IsEquipment(parent):
			problem = "el destino debe ser un gabinete, CPU o PC individual sin otro equipo padre"
		case IsCabinet(item):
			problem = "un gabinete no puede ser componente de otra PC"
		case item.Quantity != 1:
			problem = "un componente vinculado debe representar una sola unidad"
		}
		if problem != "" {
			return &EquipmentError{item.ID, problem}
		}
	}
	return nil
}
