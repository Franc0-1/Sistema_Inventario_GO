// Comando migrate: copia el inventario del Excel a SQL Server (una sola vez).
//
// Nunca modifica el Excel: trabaja sobre una copia temporal. La base de
// destino (INVENTARIO_DB_DSN) tiene que estar vacía.
//
//	go run ./cmd/migrate -probar                    # solo valida, no escribe nada
//	go run ./cmd/migrate                            # migra data/inventario.xlsx
//	go run ./cmd/migrate -excel C:\ruta\otro.xlsx
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"time"

	"inventario/internal/models"
	"inventario/internal/repository"
	"inventario/internal/utils"
)

func main() {
	cfg := utils.CargarConfig()
	excel := flag.String("excel", cfg.RutaExcel, "Excel de origen")
	probar := flag.Bool("probar", false, "solo validar los datos, sin escribir en la base")
	script := flag.String("script", "", "en vez de escribir en la base, generar este archivo .sql para ejecutarlo en SSMS")
	flag.Parse()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	copia, limpiar, err := copiarExcel(*excel)
	if err != nil {
		log.Fatalf("copiar el Excel: %v", err)
	}
	defer limpiar()
	origen, err := repository.NewExcelRepository(copia)
	if err != nil {
		log.Fatalf("abrir el Excel: %v", err)
	}
	data, err := origen.ReadMigrationData(ctx)
	origen.Close()
	if err != nil {
		log.Fatalf("leer el Excel: %v", err)
	}
	fmt.Printf("Excel %s: %d ítems, %d movimientos (último ID de ítem asignado: %d)\n",
		*excel, len(data.Items), len(data.Movements), data.LastItemID)

	if problemas := repository.CheckMigrationData(data); len(problemas) > 0 {
		fmt.Printf("\nHay %d datos que la base rechazaría. Corríjalos en el Excel y repita:\n", len(problemas))
		for _, p := range problemas {
			fmt.Println("  -", p)
		}
		os.Exit(1)
	}
	fmt.Println("Validación: sin problemas.")
	if *script != "" {
		if err := os.WriteFile(*script, []byte(scriptSQL(data)), 0o644); err != nil {
			log.Fatalf("escribir el script: %v", err)
		}
		fmt.Printf("Script generado: %s (abrirlo en SSMS sobre la base vacía y ejecutar con F5).\n", *script)
		return
	}
	if *probar {
		fmt.Println("Modo prueba: no se escribió nada en la base.")
		return
	}

	destino, err := repository.NewSQLServerRepository(ctx, cfg.DBDSN)
	if err != nil {
		log.Fatalf("conectar con SQL Server: %v", err)
	}
	defer destino.Close()
	if err := destino.LoadMigrationData(ctx, data); err != nil {
		log.Fatalf("migración cancelada, la base quedó sin cambios: %v", err)
	}
	if err := verificar(ctx, destino, data); err != nil {
		log.Fatalf("la verificación falló: %v", err)
	}
	fmt.Printf("Migración completa y verificada: %d ítems y %d movimientos.\n", len(data.Items), len(data.Movements))
}

// verificar relee la base y la compara con el Excel ítem por ítem.
func verificar(ctx context.Context, repo *repository.SQLServerRepository, data repository.MigrationData) error {
	items, err := repo.GetAll(ctx)
	if err != nil {
		return err
	}
	movs, err := repo.GetMovements(ctx)
	if err != nil {
		return err
	}
	if len(items) != len(data.Items) || len(movs) != len(data.Movements) {
		return fmt.Errorf("la base tiene %d ítems y %d movimientos; el Excel %d y %d",
			len(items), len(movs), len(data.Items), len(data.Movements))
	}
	porID := map[int]models.Item{}
	for _, it := range items {
		porID[it.ID] = it
	}
	for _, want := range data.Items {
		got, ok := porID[want.ID]
		if !ok || !mismoItem(got, want) {
			return fmt.Errorf("el ítem %d no coincide:\nExcel %+v\nbase  %+v", want.ID, want, got)
		}
	}
	return nil
}

func mismoItem(a, b models.Item) bool {
	return a.InventoryNumber == b.InventoryNumber && a.DeviceType == b.DeviceType && a.Brand == b.Brand &&
		a.Model == b.Model && a.SerialNumber == b.SerialNumber && a.Quantity == b.Quantity &&
		a.Location == b.Location && a.Status == b.Status && a.EquipmentID == b.EquipmentID &&
		a.Availability == b.Availability && a.CreatedAt.Truncate(100*time.Nanosecond).Equal(b.CreatedAt.Truncate(100*time.Nanosecond))
}

// copiarExcel copia el origen a una carpeta temporal: abrir el repositorio de
// Excel puede actualizar el esquema del libro, y el original no se toca.
func copiarExcel(ruta string) (string, func(), error) {
	dir, err := os.MkdirTemp("", "inventario-migracion-*")
	if err != nil {
		return "", nil, err
	}
	limpiar := func() { os.RemoveAll(dir) }
	src, err := os.Open(ruta)
	if err != nil {
		limpiar()
		return "", nil, err
	}
	defer src.Close()
	destino := filepath.Join(dir, "inventario.xlsx")
	dst, err := os.Create(destino)
	if err == nil {
		_, err = io.Copy(dst, src)
		if cerr := dst.Close(); err == nil {
			err = cerr
		}
	}
	if err != nil {
		limpiar()
		return "", nil, err
	}
	return destino, limpiar, nil
}
