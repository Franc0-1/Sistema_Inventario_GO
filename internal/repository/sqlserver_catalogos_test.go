package repository

import (
	"context"
	"errors"
	"testing"

	"inventario/internal/models"
)

func TestSQLServer_CatalogoOficinas(t *testing.T) {
	repo := testModeloNuevo(t)
	ctx := context.Background()

	o, err := repo.CreateCatalogo(ctx, models.CatalogoOficinas, models.Catalogo{Nombre: "Piso 6 / Sistemas", Activo: true}, 1)
	must(t, err)
	if o.ID == 0 || o.Version == "" || !o.Activo {
		t.Fatalf("oficina creada = %+v", o)
	}
	var por int
	must(t, repo.db.QueryRowContext(ctx, `SELECT ActualizadoPor FROM dbo.Oficinas WHERE ID = @p1`, o.ID).Scan(&por))
	if por != 1 {
		t.Errorf("ActualizadoPor = %d, se esperaba 1", por)
	}

	// La base compara sin mayúsculas ni tildes.
	if _, err := repo.CreateCatalogo(ctx, models.CatalogoOficinas, models.Catalogo{Nombre: "PISO 6 / SISTEMAS", Activo: true}, 0); !errors.Is(err, ErrDuplicado) {
		t.Errorf("nombre repetido: err = %v", err)
	}

	o.Nombre, o.Activo = "Piso 6 / Soporte", false
	editada, err := repo.UpdateCatalogo(ctx, models.CatalogoOficinas, o, 0)
	must(t, err)
	if editada.Nombre != "Piso 6 / Soporte" || editada.Activo || editada.Version == o.Version {
		t.Errorf("oficina editada = %+v (versión anterior %s)", editada, o.Version)
	}
	// o.Version quedó vieja: otro la modificó.
	if _, err := repo.UpdateCatalogo(ctx, models.CatalogoOficinas, o, 0); !errors.Is(err, ErrConflict) {
		t.Errorf("versión vieja: err = %v", err)
	}
	o.ID = 999
	if _, err := repo.UpdateCatalogo(ctx, models.CatalogoOficinas, o, 0); !errors.Is(err, ErrNoEncontrado) {
		t.Errorf("inexistente: err = %v", err)
	}

	activas, err := repo.ListCatalogo(ctx, models.CatalogoOficinas, true)
	must(t, err)
	todas, err := repo.ListCatalogo(ctx, models.CatalogoOficinas, false)
	must(t, err)
	if len(activas) != 0 || len(todas) != 1 {
		t.Errorf("activas %d, todas %d", len(activas), len(todas))
	}
	if _, err := repo.GetCatalogo(ctx, models.CatalogoPuestos, 1); !errors.Is(err, ErrNoEncontrado) {
		t.Errorf("puesto inexistente: err = %v", err)
	}
}

func TestSQLServer_Personas(t *testing.T) {
	repo := testModeloNuevo(t)
	ctx := context.Background()
	oficina, err := repo.CreateCatalogo(ctx, models.CatalogoOficinas, models.Catalogo{Nombre: "Piso EP / Prensa", Activo: true}, 0)
	must(t, err)
	puesto, err := repo.CreateCatalogo(ctx, models.CatalogoPuestos, models.Catalogo{Nombre: "Periodista", Activo: true}, 0)
	must(t, err)

	p, err := repo.CreatePersona(ctx, models.Persona{Nombre: "María", Apellido: "Gómez", PuestoID: puesto.ID, OficinaID: oficina.ID, Activo: true}, 0)
	must(t, err)
	if p.Oficina != "Piso EP / Prensa" || p.Puesto != "Periodista" {
		t.Errorf("persona creada = %+v", p)
	}
	sinPuesto, err := repo.CreatePersona(ctx, models.Persona{Nombre: "Juan", OficinaID: oficina.ID, Activo: true}, 0)
	must(t, err)
	if sinPuesto.PuestoID != 0 || sinPuesto.Puesto != "" {
		t.Errorf("persona sin puesto = %+v", sinPuesto)
	}

	for _, c := range []struct {
		filtro models.FiltroPersonas
		want   int
	}{
		{models.FiltroPersonas{Texto: "gomez maria"}, 1}, // sin tildes y en cualquier orden
		{models.FiltroPersonas{Texto: "100%"}, 0},        // los comodines de LIKE se buscan literalmente
		{models.FiltroPersonas{OficinaID: oficina.ID}, 2},
		{models.FiltroPersonas{OficinaID: oficina.ID + 1}, 0},
	} {
		got, err := repo.ListPersonas(ctx, c.filtro)
		must(t, err)
		if len(got) != c.want {
			t.Errorf("%+v: %d personas, se esperaban %d", c.filtro, len(got), c.want)
		}
	}

	// No se desactiva una oficina con personas activas.
	oficina.Activo = false
	if _, err := repo.UpdateCatalogo(ctx, models.CatalogoOficinas, oficina, 0); !errors.Is(err, ErrEnUso) {
		t.Errorf("oficina con personas: err = %v", err)
	}

	// No se desactiva una persona con un equipo asignado.
	_, err = repo.db.ExecContext(ctx, `INSERT INTO dbo.Tipos (Nombre, Clase) VALUES (N'PC', 'EQUIPO');
		INSERT INTO dbo.Equipos (NumeroInventario, TipoID, OficinaID, PersonaID) VALUES (N'1001', SCOPE_IDENTITY(), @p1, @p2)`,
		oficina.ID, p.ID)
	must(t, err)
	p.Activo = false
	if _, err := repo.UpdatePersona(ctx, p, 0); !errors.Is(err, ErrEnUso) {
		t.Errorf("persona con equipo: err = %v", err)
	}
	sinPuesto.Activo, sinPuesto.Apellido = false, "Pérez"
	editada, err := repo.UpdatePersona(ctx, sinPuesto, 0)
	must(t, err)
	if editada.Activo || editada.Apellido != "Pérez" || editada.Oficina != "Piso EP / Prensa" {
		t.Errorf("persona editada = %+v", editada)
	}
}

func TestSQLServer_Tipos(t *testing.T) {
	repo := testModeloNuevo(t)
	ctx := context.Background()
	pc, err := repo.CreateTipo(ctx, models.Tipo{Nombre: "PC", Clase: models.ClaseEquipo, Activo: true}, 0)
	must(t, err)
	_, err = repo.CreateTipo(ctx, models.Tipo{Nombre: "Mouse", Clase: models.ClaseInsumo, Activo: true}, 0)
	must(t, err)
	if _, err := repo.CreateTipo(ctx, models.Tipo{Nombre: "pc", Clase: models.ClaseEquipo, Activo: true}, 0); !errors.Is(err, ErrDuplicado) {
		t.Errorf("tipo repetido: err = %v", err)
	}

	equipos, err := repo.ListTipos(ctx, models.ClaseEquipo, false)
	must(t, err)
	if len(equipos) != 1 || equipos[0].Nombre != "PC" {
		t.Errorf("tipos de equipo = %+v", equipos)
	}

	// Sin ítems cargados la clase todavía se puede cambiar.
	pc.Clase = models.ClaseComponente
	pc, err = repo.UpdateTipo(ctx, pc, 0)
	must(t, err)
	pc.Clase = models.ClaseEquipo
	pc, err = repo.UpdateTipo(ctx, pc, 0)
	must(t, err)

	_, err = repo.db.ExecContext(ctx, `INSERT INTO dbo.Oficinas (Nombre) VALUES (N'Piso 6');
		INSERT INTO dbo.Equipos (NumeroInventario, TipoID, OficinaID) VALUES (N'1001', @p1, SCOPE_IDENTITY())`, pc.ID)
	must(t, err)
	pc.Clase = models.ClaseInsumo
	if _, err := repo.UpdateTipo(ctx, pc, 0); !errors.Is(err, ErrEnUso) {
		t.Errorf("cambiar la clase de un tipo con equipos: err = %v", err)
	}
	pc.Clase, pc.Nombre, pc.Prestable = models.ClaseEquipo, "Computadora", true
	renombrado, err := repo.UpdateTipo(ctx, pc, 0)
	must(t, err)
	if renombrado.Nombre != "Computadora" || !renombrado.Prestable {
		t.Errorf("tipo editado = %+v", renombrado)
	}
}
