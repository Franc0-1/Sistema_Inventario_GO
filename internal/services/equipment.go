package services

import (
	"context"
	"fmt"
	"sort"

	"inventario/internal/models"
)

func validateEquipmentChange(items []models.Item, changed models.Item) error {
	updated := make([]models.Item, 0, len(items)+1)
	for _, item := range items {
		if item.ID != changed.ID {
			updated = append(updated, item)
		}
	}
	updated = append(updated, changed)
	if err := models.ValidateEquipment(updated); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidItem, err)
	}
	return nil
}

func (s *inventoryService) Equipments(ctx context.Context, includeRetired bool) ([]models.Equipment, error) {
	items, err := s.repo.GetAll(ctx)
	if err != nil {
		return nil, err
	}
	components := map[int][]models.Item{}
	for _, item := range items {
		if item.EquipmentID != 0 {
			components[item.EquipmentID] = append(components[item.EquipmentID], item)
		}
	}
	result := []models.Equipment{}
	for _, item := range items {
		if !models.IsEquipment(item) || (!includeRetired && item.Status == models.StatusRetired) {
			continue
		}
		children := components[item.ID]
		if children == nil {
			children = []models.Item{}
		}
		sort.Slice(children, func(i, j int) bool { return children[i].ID < children[j].ID })
		result = append(result, models.Equipment{Cabinet: item, Components: children})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Cabinet.ID < result[j].Cabinet.ID })
	return result, nil
}

func (s *inventoryService) SetEquipment(ctx context.Context, id, equipmentID int) (models.Item, error) {
	if err := validateID(id); err != nil {
		return models.Item{}, err
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	item, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return models.Item{}, err
	}
	item.EquipmentID = equipmentID
	if err := s.checkUniqueness(ctx, item); err != nil {
		return models.Item{}, err
	}
	return s.repo.Update(ctx, item, item.UpdatedAt, nil)
}
