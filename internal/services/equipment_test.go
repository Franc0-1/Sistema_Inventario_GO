package services

import (
	"errors"
	"inventario/internal/models"
	"testing"
)

func equipmentSeed() []models.Item {
	items := seed()
	items[0].DeviceType = "Gabinete"
	items[0].Availability, items[0].AssignedTo, items[0].LoanedAt = models.Available, "", nil
	return items
}

func TestEquipment_LinkUnlinkPreservesInventoryAndHistory(t *testing.T) {
	svc, repo := newTestService(equipmentSeed()...)
	linked, err := svc.SetEquipment(ctx, 2, 1)
	if err != nil {
		t.Fatal(err)
	}
	if linked.EquipmentID != 1 || linked.InventoryNumber != "INV-002" {
		t.Fatalf("unexpected component: %+v", linked)
	}
	list, err := svc.Equipments(ctx, false)
	if err != nil || len(list) != 1 || len(list[0].Components) != 1 {
		t.Fatalf("list=%+v err=%v", list, err)
	}
	updated := linked
	updated.Model = "Otro modelo"
	updated.EquipmentID = 0
	updated, err = svc.Update(ctx, 2, updated)
	if err != nil || updated.EquipmentID != 1 {
		t.Fatalf("edit lost association: %+v %v", updated, err)
	}
	unlinked, err := svc.SetEquipment(ctx, 2, 0)
	if err != nil || unlinked.EquipmentID != 0 || unlinked.InventoryNumber != "INV-002" {
		t.Fatalf("unlink: %+v %v", unlinked, err)
	}
	if len(repo.movements) != 0 {
		t.Fatal("linking created stock movements")
	}
}

func TestEquipment_SharedNumbersOnlyWithinSamePC(t *testing.T) {
	items := equipmentSeed()
	svc, _ := newTestService(items...)
	component := validItem()
	component.DeviceType, component.InventoryNumber, component.SerialNumber = "RAM", items[0].InventoryNumber, "RAM-1"
	component.EquipmentID = 1
	created, err := svc.Create(ctx, component)
	if err != nil {
		t.Fatal(err)
	}
	component.EquipmentID = 0
	component.SerialNumber = "RAM-2"
	if _, err := svc.Create(ctx, component); !errors.Is(err, ErrInventoryNumberExists) {
		t.Fatalf("outside group: %v", err)
	}
	if _, err := svc.SetEquipment(ctx, created.ID, 0); !errors.Is(err, ErrInventoryNumberExists) {
		t.Fatalf("unlink shared number: %v", err)
	}
	component.EquipmentID, component.InventoryNumber, component.SerialNumber = 1, "DIFFERENT", "RAM-1"
	if _, err := svc.Create(ctx, component); !errors.Is(err, ErrSerialNumberExists) {
		t.Fatalf("serial duplicated: %v", err)
	}
}

func TestEquipment_InvalidReferencesAndStock(t *testing.T) {
	for _, parent := range []int{-1, 2, 3, 999} {
		svc, _ := newTestService(equipmentSeed()...)
		if _, err := svc.SetEquipment(ctx, 2, parent); !errors.Is(err, ErrInvalidItem) {
			t.Fatalf("parent %d: %v", parent, err)
		}
	}
	svc, _ := newTestService(equipmentSeed()...)
	if _, err := svc.SetEquipment(ctx, 1, 1); !errors.Is(err, ErrInvalidItem) {
		t.Fatal(err)
	}
	if _, err := svc.SetEquipment(ctx, 3, 1); !errors.Is(err, ErrInvalidItem) {
		t.Fatalf("bulk stock linked: %v", err)
	}
	if _, err := svc.SetEquipment(ctx, 2, 1); err != nil {
		t.Fatal(err)
	}
	if err := svc.Delete(ctx, 1); !errors.Is(err, ErrInvalidItem) {
		t.Fatalf("deleted root: %v", err)
	}
	if _, err := svc.UpdateStock(ctx, 2, 0); !errors.Is(err, ErrInvalidItem) {
		t.Fatalf("changed linked quantity: %v", err)
	}
	if _, err := svc.UpdateStock(ctx, 1, 0); !errors.Is(err, ErrInvalidItem) {
		t.Fatalf("changed root quantity: %v", err)
	}
	root := equipmentSeed()[0]
	root.DeviceType = "Monitor"
	if _, err := svc.Update(ctx, 1, root); !errors.Is(err, ErrInvalidItem) {
		t.Fatalf("changed root type: %v", err)
	}
}

func TestEquipment_EmptyRetiredAndStableOrder(t *testing.T) {
	svc, _ := newTestService()
	list, err := svc.Equipments(ctx, false)
	if err != nil || list == nil || len(list) != 0 {
		t.Fatalf("empty: %v %v", list, err)
	}
	items := equipmentSeed()
	items[0].Status = models.StatusRetired
	svc, _ = newTestService(items...)
	list, err = svc.Equipments(ctx, false)
	if err != nil || len(list) != 0 {
		t.Fatal(list, err)
	}
	list, err = svc.Equipments(ctx, true)
	if err != nil || len(list) != 1 {
		t.Fatal(list, err)
	}
}
