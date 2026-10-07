package repository

import (
	"bytes"
	"context"
	"github.com/xuri/excelize/v2"
	"os"
	"reflect"
	"testing"
)

func TestEquipment_PersistenceAndExcelRoundTrip(t *testing.T) {
	repo, path := newWriteRepo(t)
	ctx := context.Background()
	root, err := repo.GetByID(ctx, 1)
	must(t, err)
	root.DeviceType = "Gabinete"
	root, err = repo.Update(ctx, root, root.UpdatedAt, nil)
	must(t, err)
	child, err := repo.GetByID(ctx, 2)
	must(t, err)
	child.EquipmentID, child.InventoryNumber = root.ID, root.InventoryNumber
	child, err = repo.Update(ctx, child, child.UpdatedAt, nil)
	must(t, err)
	before, err := repo.GetAll(ctx)
	must(t, err)
	movements, err := repo.GetMovements(ctx)
	must(t, err)
	data := exportar(t, repo)
	_, err = repo.ImportInventory(ctx, bytes.NewReader(data))
	must(t, err)
	after, err := repo.GetAll(ctx)
	must(t, err)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("round trip changed records")
	}
	gotMovements, err := repo.GetMovements(ctx)
	must(t, err)
	if !reflect.DeepEqual(movements, gotMovements) {
		t.Fatal("history changed")
	}
	legacy := editarExportado(t, data, func(f *excelize.File) { must(t, f.RemoveCol(SheetInventory, "M")) })
	_, err = repo.ImportInventory(ctx, bytes.NewReader(legacy))
	must(t, err)
	legacyItems, err := repo.GetAll(ctx)
	must(t, err)
	if !reflect.DeepEqual(before, legacyItems) {
		t.Fatal("legacy export lost associations")
	}
	reopened, err := NewExcelRepository(path)
	must(t, err)
	defer reopened.Close()
	got, err := reopened.GetByID(ctx, child.ID)
	must(t, err)
	if got.EquipmentID != root.ID || got.InventoryNumber != root.InventoryNumber {
		t.Fatal(got)
	}
	for _, parent := range []int{child.ID, 999} {
		invalid := editarExportado(t, data, func(f *excelize.File) { set(t, f, "M3", parent) })
		fileBefore, err := os.ReadFile(path)
		must(t, err)
		if _, err := repo.ImportInventory(ctx, bytes.NewReader(invalid)); err == nil {
			t.Fatal("invalid reference accepted")
		}
		fileAfter, err := os.ReadFile(path)
		must(t, err)
		if !bytes.Equal(fileBefore, fileAfter) {
			t.Fatal("failed import modified file")
		}
	}
	missingRoot := editarExportado(t, data, func(f *excelize.File) { must(t, f.RemoveRow(SheetInventory, 2)) })
	fileBefore, err := os.ReadFile(path)
	must(t, err)
	if _, err := repo.ImportInventory(ctx, bytes.NewReader(missingRoot)); err == nil {
		t.Fatal("import removed referenced root")
	}
	fileAfter, err := os.ReadFile(path)
	must(t, err)
	if !bytes.Equal(fileBefore, fileAfter) {
		t.Fatal("failed root removal changed workbook")
	}
}

func TestEquipment_RejectsDanglingWrites(t *testing.T) {
	repo, path := newWriteRepo(t)
	ctx := context.Background()
	child, err := repo.GetByID(ctx, 2)
	must(t, err)
	child.EquipmentID = 999
	before, err := os.ReadFile(path)
	must(t, err)
	if _, err := repo.Update(ctx, child, child.UpdatedAt, nil); err == nil {
		t.Fatal("invalid relationship accepted")
	}
	after, err := os.ReadFile(path)
	must(t, err)
	if !bytes.Equal(before, after) {
		t.Fatal("invalid write modified workbook")
	}
}
