package repository

import (
	"context"
	"errors"
	"strings"
	"testing"

	"inventario/internal/models"
)

// datosEquipos carga dos oficinas, una persona y los tipos PC y RAM.
// Devuelve sus IDs en ese orden.
func datosEquipos(t *testing.T, repo *SQLServerRepository) (sistemas, prensa, persona, pc, ram int) {
	t.Helper()
	ctx := context.Background()
	must(t, repo.db.QueryRowContext(ctx, `
		INSERT INTO dbo.Oficinas (Nombre) VALUES (N'Piso 6 / Sistemas'), (N'Piso EP / Prensa');
		INSERT INTO dbo.Personas (Nombre, Apellido, OficinaID) VALUES (N'María', N'Gómez', 2);
		INSERT INTO dbo.Tipos (Nombre, Clase) VALUES (N'PC', 'EQUIPO'), (N'RAM', 'COMPONENTE');
		SELECT 1, 2, 1, 1, 2`).Scan(&sistemas, &prensa, &persona, &pc, &ram))
	return
}

func TestSQLServer_EquiposEHistorial(t *testing.T) {
	repo := testModeloNuevo(t)
	ctx := context.Background()
	sistemas, prensa, persona, pc, _ := datosEquipos(t, repo)

	e, err := repo.CreateEquipo(ctx, models.Equipo{NumeroInventario: "1001", TipoID: pc, Marca: "Dell", NumeroSerie: "DX-1",
		Estado: models.StatusOperational, OficinaID: sistemas}, 1, "Alta")
	must(t, err)
	if e.Tipo != "PC" || e.Oficina != "Piso 6 / Sistemas" || e.Version == "" {
		t.Fatalf("equipo creado = %+v", e)
	}

	// Asignarlo a una persona de otra oficina deja una fila de historial.
	e.PersonaID, e.OficinaID = persona, prensa
	e, err = repo.UpdateEquipo(ctx, e, 1, "Pasa a Prensa")
	must(t, err)
	if e.Persona != "María Gómez" || e.Oficina != "Piso EP / Prensa" {
		t.Errorf("equipo asignado = %+v", e)
	}
	// Cambiar solo la marca no agrega historial.
	e.Marca = "Dell Inc."
	e, err = repo.UpdateEquipo(ctx, e, 1, "")
	must(t, err)
	// La baja guarda fecha y motivo.
	e.Estado, e.MotivoBaja = models.StatusRetired, "Placa quemada"
	e, err = repo.UpdateEquipo(ctx, e, 1, "")
	must(t, err)
	if e.FechaBaja == nil || e.MotivoBaja != "Placa quemada" {
		t.Errorf("baja = %v %q", e.FechaBaja, e.MotivoBaja)
	}

	h, err := repo.HistorialEquipo(ctx, e.ID)
	must(t, err)
	if len(h) != 3 {
		t.Fatalf("historial = %+v", h)
	}
	if h[1].OficinaAnterior != "Piso 6 / Sistemas" || h[1].PersonaNueva != "María Gómez" || h[1].Observacion != "Pasa a Prensa" ||
		h[1].Usuario != "Sistema" || h[0].Estado != models.StatusRetired || h[2].OficinaAnterior != "" {
		t.Errorf("historial = %+v", h)
	}

	// Duplicados con un mensaje según el campo, y versión vieja.
	_, err = repo.CreateEquipo(ctx, models.Equipo{NumeroInventario: "1001", TipoID: pc, Estado: models.StatusOperational, OficinaID: sistemas}, 1, "")
	if !errors.Is(err, ErrDuplicado) || !contiene(err, "N° de inventario") {
		t.Errorf("N° repetido: err = %v", err)
	}
	_, err = repo.CreateEquipo(ctx, models.Equipo{NumeroInventario: "1002", NumeroSerie: "dx-1", TipoID: pc, Estado: models.StatusOperational, OficinaID: sistemas}, 1, "")
	if !errors.Is(err, ErrDuplicado) || !contiene(err, "N° de serie") {
		t.Errorf("serie repetida: err = %v", err)
	}
	viejo := e
	viejo.Version = "0000000000000001"
	if _, err := repo.UpdateEquipo(ctx, viejo, 1, ""); !errors.Is(err, ErrConflict) {
		t.Errorf("versión vieja: err = %v", err)
	}

	// Pendientes de numerar y filtros.
	_, err = repo.CreateEquipo(ctx, models.Equipo{TipoID: pc, Estado: models.StatusOperational, OficinaID: sistemas}, 1, "")
	must(t, err)
	for _, c := range []struct {
		f    models.FiltroEquipos
		want int
	}{
		{models.FiltroEquipos{}, 1}, // sin bajas
		{models.FiltroEquipos{IncluirBajas: true}, 2},
		{models.FiltroEquipos{Estado: models.StatusRetired}, 1},
		{models.FiltroEquipos{Pendientes: true}, 1},
		{models.FiltroEquipos{Texto: "dell", IncluirBajas: true}, 1},
		{models.FiltroEquipos{PersonaID: persona, IncluirBajas: true}, 1},
	} {
		got, err := repo.ListEquipos(ctx, c.f)
		must(t, err)
		if len(got) != c.want {
			t.Errorf("%+v: %d equipos, se esperaban %d", c.f, len(got), c.want)
		}
	}
}

func TestSQLServer_ComponentesEHistorial(t *testing.T) {
	repo := testModeloNuevo(t)
	ctx := context.Background()
	sistemas, _, _, pc, ram := datosEquipos(t, repo)
	pc1, err := repo.CreateEquipo(ctx, models.Equipo{NumeroInventario: "1001", TipoID: pc, Estado: models.StatusOperational, OficinaID: sistemas}, 1, "")
	must(t, err)
	pc2, err := repo.CreateEquipo(ctx, models.Equipo{NumeroInventario: "1002", TipoID: pc, Estado: models.StatusOperational, OficinaID: sistemas}, 1, "")
	must(t, err)

	c, err := repo.CreateComponente(ctx, models.Componente{NumeroInventario: "1001", TipoID: ram, NumeroSerie: "RAM-1",
		Estado: models.StatusOperational, EquipoID: pc1.ID}, 1, "")
	must(t, err)
	if c.Equipo != "1001" || c.Oficina != "Piso 6 / Sistemas" || c.OficinaID != 0 {
		t.Fatalf("componente creado = %+v", c)
	}

	// Pasa a otra PC sin cambiar su N° de inventario, y después a un cajón.
	c.EquipoID = pc2.ID
	c, err = repo.UpdateComponente(ctx, c, 1, "Upgrade de la 1002")
	must(t, err)
	if c.NumeroInventario != "1001" || c.Equipo != "1002" {
		t.Errorf("componente movido = %+v", c)
	}
	c.EquipoID, c.OficinaID = 0, sistemas
	c, err = repo.UpdateComponente(ctx, c, 1, "")
	must(t, err)

	h, err := repo.HistorialComponente(ctx, c.ID)
	must(t, err)
	if len(h) != 3 || h[1].EquipoAnterior != "1001" || h[1].EquipoNuevo != "1002" || h[0].EquipoNuevoID != 0 {
		t.Errorf("historial = %+v", h)
	}

	if _, err := repo.CreateComponente(ctx, models.Componente{TipoID: ram, NumeroSerie: "ram-1", Estado: models.StatusOperational,
		OficinaID: sistemas}, 1, ""); !errors.Is(err, ErrDuplicado) {
		t.Errorf("serie repetida: err = %v", err)
	}

	sueltos, err := repo.ListComponentes(ctx, models.FiltroComponentes{Sueltos: true})
	must(t, err)
	enOficina, err := repo.ListComponentes(ctx, models.FiltroComponentes{OficinaID: sistemas})
	must(t, err)
	if len(sueltos) != 1 || len(enOficina) != 1 {
		t.Errorf("sueltos %d, en la oficina %d", len(sueltos), len(enOficina))
	}
	detalle, err := repo.GetEquipo(ctx, pc2.ID)
	must(t, err)
	if detalle.CantidadComponentes != 0 {
		t.Errorf("la PC 1002 quedó con %d componentes", detalle.CantidadComponentes)
	}
}

func contiene(err error, texto string) bool {
	return err != nil && strings.Contains(err.Error(), texto)
}
