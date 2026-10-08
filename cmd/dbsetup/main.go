// Comando dbsetup: se conecta a SQL Server con INVENTARIO_DB_DSN, aplica las
// migraciones pendientes y muestra el estado de la base. No toca el Excel.
//
//	$env:INVENTARIO_DB_DSN = "server=...;database=Inventario;user id=inventario_app;password=...;encrypt=true;TrustServerCertificate=true"
//	go run ./cmd/dbsetup
package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"inventario/internal/config"
	"inventario/internal/repository"
)

func main() {
	cfg := config.Cargar()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	repo, err := repository.NewSQLServerRepository(ctx, cfg.DBDSN)
	if err != nil {
		log.Fatalf("no se pudo preparar la base: %v", err)
	}
	defer repo.Close()

	if err := repo.Check(ctx); err != nil {
		log.Fatalf("la base no está lista: %v", err)
	}
	versions, err := repo.MigrationVersions(ctx)
	if err != nil {
		log.Fatalf("leer migraciones: %v", err)
	}
	fmt.Printf("Conexión correcta. Migraciones aplicadas: %v\n", versions)
}
