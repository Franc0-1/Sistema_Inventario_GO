package services

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"inventario/internal/models"
	"inventario/internal/repository"
)

// EquipoService administra equipos y componentes. Toda escritura exige el
// usuario (queda en el historial); nota es la observación opcional del cambio.
type EquipoService interface {
	ListarEquipos(ctx context.Context, f models.FiltroEquipos) ([]models.Equipo, error)
	ObtenerEquipo(ctx context.Context, id int) (models.Equipo, error) // con sus componentes
	CrearEquipo(ctx context.Context, e models.Equipo, nota string) (models.Equipo, error)
	ActualizarEquipo(ctx context.Context, e models.Equipo, nota string) (models.Equipo, error)
	HistorialEquipo(ctx context.Context, id int) ([]models.HistorialEquipo, error)

	ListarComponentes(ctx context.Context, f models.FiltroComponentes) ([]models.Componente, error)
	ObtenerComponente(ctx context.Context, id int) (models.Componente, error)
	CrearComponente(ctx context.Context, c models.Componente, nota string) (models.Componente, error)
	ActualizarComponente(ctx context.Context, c models.Componente, nota string) (models.Componente, error)
	HistorialComponente(ctx context.Context, id int) ([]models.HistorialComponente, error)
}

type equipoService struct {
	repo repository.EquipoRepository
	cat  *catalogoService
}

func NewEquipoService(repo repository.EquipoRepository, catalogos repository.CatalogoRepository) EquipoService {
	return &equipoService{repo: repo, cat: &catalogoService{repo: catalogos}}
}

// tipoDeClase verifica que el tipo exista, esté activo y sea de la clase pedida.
func (s *catalogoService) tipoDeClase(ctx context.Context, id int, clase models.Clase) error {
	t, err := s.repo.GetTipo(ctx, id)
	switch {
	case errors.Is(err, repository.ErrNoEncontrado):
		return fmt.Errorf("%w: el tipo %d no existe", ErrDatoInvalido, id)
	case err != nil:
		return err
	case t.Clase != clase:
		return fmt.Errorf("%w: el tipo %q es de %s, no de %s", ErrDatoInvalido, t.Nombre,
			strings.ToLower(string(t.Clase)), strings.ToLower(string(clase)))
	case !t.Activo:
		return fmt.Errorf("%w: el tipo %q está inactivo", ErrDatoInvalido, t.Nombre)
	}
	return nil
}

// datosComunes normaliza los textos, el estado y la baja de un equipo o componente.
type datosComunes struct {
	numero, marca, modelo, serie, observacion, motivo *string
	estado                                            *models.ItemStatus
}

func (d datosComunes) normalizar() error {
	for _, c := range []struct {
		v     *string
		campo string
		max   int
	}{
		{d.numero, "el N° de inventario", 100}, {d.marca, "la marca", 100}, {d.modelo, "el modelo", 100},
		{d.serie, "el N° de serie", 100}, {d.observacion, "la observación", 1000}, {d.motivo, "el motivo de baja", 500},
	} {
		var err error
		if *c.v, err = texto(*c.v, c.campo, c.max, false); err != nil {
			return err
		}
	}
	if *d.estado == "" {
		*d.estado = models.StatusOperational
	} else if e, ok := oneOf(string(*d.estado), models.ValidStatuses); ok {
		*d.estado = e
	} else {
		return fmt.Errorf("%w: estado %q desconocido", ErrDatoInvalido, *d.estado)
	}
	switch {
	case *d.estado == models.StatusRetired && *d.motivo == "":
		return fmt.Errorf("%w: indicá el motivo de la baja", ErrDatoInvalido)
	case *d.estado != models.StatusRetired:
		*d.motivo = ""
	}
	return nil
}

func normalizarNota(nota string) (string, error) {
	return texto(nota, "la nota", 1000, false)
}

// ---- Equipos ----

func (s *equipoService) ListarEquipos(ctx context.Context, f models.FiltroEquipos) ([]models.Equipo, error) {
	if f.Estado != "" {
		e, ok := oneOf(string(f.Estado), models.ValidStatuses)
		if !ok {
			return nil, fmt.Errorf("%w: estado %q desconocido", ErrDatoInvalido, f.Estado)
		}
		f.Estado = e
	}
	return s.repo.ListEquipos(ctx, f)
}

func (s *equipoService) ObtenerEquipo(ctx context.Context, id int) (models.Equipo, error) {
	if err := validarID(id); err != nil {
		return models.Equipo{}, err
	}
	e, err := s.repo.GetEquipo(ctx, id)
	if err != nil {
		return e, err
	}
	e.Componentes, err = s.repo.ListComponentes(ctx, models.FiltroComponentes{EquipoID: id, IncluirBajas: true})
	return e, err
}

// validarEquipo controla tipo, persona y oficina solo si cambian respecto de
// anterior (en el alta, anterior está vacío). Con persona y sin oficina, el
// equipo queda en la oficina de la persona.
func (s *equipoService) validarEquipo(ctx context.Context, e, anterior models.Equipo) (models.Equipo, error) {
	d := datosComunes{&e.NumeroInventario, &e.Marca, &e.Modelo, &e.NumeroSerie, &e.Observacion, &e.MotivoBaja, &e.Estado}
	if err := d.normalizar(); err != nil {
		return e, err
	}
	if e.TipoID <= 0 {
		return e, fmt.Errorf("%w: el tipo es obligatorio", ErrDatoInvalido)
	}
	if e.TipoID != anterior.TipoID {
		if err := s.cat.tipoDeClase(ctx, e.TipoID, models.ClaseEquipo); err != nil {
			return e, err
		}
	}
	if e.PersonaID < 0 || e.OficinaID < 0 {
		return e, fmt.Errorf("%w: persona u oficina inválida", ErrDatoInvalido)
	}
	if e.PersonaID != 0 && e.PersonaID != anterior.PersonaID {
		p, err := s.cat.repo.GetPersona(ctx, e.PersonaID)
		switch {
		case errors.Is(err, repository.ErrNoEncontrado):
			return e, fmt.Errorf("%w: la persona %d no existe", ErrDatoInvalido, e.PersonaID)
		case err != nil:
			return e, err
		case !p.Activo:
			return e, fmt.Errorf("%w: %s está inactiva", ErrDatoInvalido, p.NombreCompleto())
		}
		if e.OficinaID == 0 {
			e.OficinaID = p.OficinaID
		}
	}
	if e.OficinaID == 0 {
		return e, fmt.Errorf("%w: la oficina es obligatoria", ErrDatoInvalido)
	}
	if e.OficinaID != anterior.OficinaID {
		if err := s.cat.referenciaActiva(ctx, models.CatalogoOficinas, "la oficina", "inactiva", e.OficinaID); err != nil {
			return e, err
		}
	}
	return e, nil
}

func (s *equipoService) CrearEquipo(ctx context.Context, e models.Equipo, nota string) (models.Equipo, error) {
	usuario, err := actorDe(ctx, s.cat.repo, true)
	if err != nil {
		return e, err
	}
	if nota, err = normalizarNota(nota); err != nil {
		return e, err
	}
	if e, err = s.validarEquipo(ctx, e, models.Equipo{}); err != nil {
		return e, err
	}
	return s.repo.CreateEquipo(ctx, e, usuario, nota)
}

func (s *equipoService) ActualizarEquipo(ctx context.Context, e models.Equipo, nota string) (models.Equipo, error) {
	if err := validarEdicion(e.ID, e.Version); err != nil {
		return e, err
	}
	usuario, err := actorDe(ctx, s.cat.repo, true)
	if err != nil {
		return e, err
	}
	if nota, err = normalizarNota(nota); err != nil {
		return e, err
	}
	anterior, err := s.repo.GetEquipo(ctx, e.ID)
	if err != nil {
		return e, err
	}
	if e, err = s.validarEquipo(ctx, e, anterior); err != nil {
		return e, err
	}
	return s.repo.UpdateEquipo(ctx, e, usuario, nota)
}

func (s *equipoService) HistorialEquipo(ctx context.Context, id int) ([]models.HistorialEquipo, error) {
	if err := validarID(id); err != nil {
		return nil, err
	}
	if _, err := s.repo.GetEquipo(ctx, id); err != nil {
		return nil, err
	}
	return s.repo.HistorialEquipo(ctx, id)
}

// ---- Componentes ----

func (s *equipoService) ListarComponentes(ctx context.Context, f models.FiltroComponentes) ([]models.Componente, error) {
	return s.repo.ListComponentes(ctx, f)
}

func (s *equipoService) ObtenerComponente(ctx context.Context, id int) (models.Componente, error) {
	if err := validarID(id); err != nil {
		return models.Componente{}, err
	}
	return s.repo.GetComponente(ctx, id)
}

// validarComponente: el componente está en un equipo (que no esté de baja) o
// suelto en una oficina activa. Si se instala nuevo en un equipo sin N° de
// inventario propio, toma el del equipo.
func (s *equipoService) validarComponente(ctx context.Context, c, anterior models.Componente, alta bool) (models.Componente, error) {
	d := datosComunes{&c.NumeroInventario, &c.Marca, &c.Modelo, &c.NumeroSerie, &c.Observacion, &c.MotivoBaja, &c.Estado}
	if err := d.normalizar(); err != nil {
		return c, err
	}
	if c.TipoID <= 0 {
		return c, fmt.Errorf("%w: el tipo es obligatorio", ErrDatoInvalido)
	}
	if c.TipoID != anterior.TipoID {
		if err := s.cat.tipoDeClase(ctx, c.TipoID, models.ClaseComponente); err != nil {
			return c, err
		}
	}
	if c.EquipoID < 0 || c.OficinaID < 0 {
		return c, fmt.Errorf("%w: equipo u oficina inválida", ErrDatoInvalido)
	}
	if c.EquipoID > 0 {
		c.OficinaID = 0
		if c.EquipoID == anterior.EquipoID {
			return c, nil
		}
		e, err := s.repo.GetEquipo(ctx, c.EquipoID)
		switch {
		case errors.Is(err, repository.ErrNoEncontrado):
			return c, fmt.Errorf("%w: el equipo %d no existe", ErrDatoInvalido, c.EquipoID)
		case err != nil:
			return c, err
		case e.Estado == models.StatusRetired:
			return c, fmt.Errorf("%w: el equipo %s está de baja", ErrDatoInvalido, e.NumeroInventario)
		}
		if alta && c.NumeroInventario == "" {
			c.NumeroInventario = e.NumeroInventario
		}
		return c, nil
	}
	if c.OficinaID == 0 {
		return c, fmt.Errorf("%w: indicá el equipo donde está instalado o la oficina donde está guardado", ErrDatoInvalido)
	}
	if c.OficinaID != anterior.OficinaID {
		if err := s.cat.referenciaActiva(ctx, models.CatalogoOficinas, "la oficina", "inactiva", c.OficinaID); err != nil {
			return c, err
		}
	}
	return c, nil
}

func (s *equipoService) CrearComponente(ctx context.Context, c models.Componente, nota string) (models.Componente, error) {
	usuario, err := actorDe(ctx, s.cat.repo, true)
	if err != nil {
		return c, err
	}
	if nota, err = normalizarNota(nota); err != nil {
		return c, err
	}
	if c, err = s.validarComponente(ctx, c, models.Componente{}, true); err != nil {
		return c, err
	}
	return s.repo.CreateComponente(ctx, c, usuario, nota)
}

func (s *equipoService) ActualizarComponente(ctx context.Context, c models.Componente, nota string) (models.Componente, error) {
	if err := validarEdicion(c.ID, c.Version); err != nil {
		return c, err
	}
	usuario, err := actorDe(ctx, s.cat.repo, true)
	if err != nil {
		return c, err
	}
	if nota, err = normalizarNota(nota); err != nil {
		return c, err
	}
	anterior, err := s.repo.GetComponente(ctx, c.ID)
	if err != nil {
		return c, err
	}
	if c, err = s.validarComponente(ctx, c, anterior, false); err != nil {
		return c, err
	}
	return s.repo.UpdateComponente(ctx, c, usuario, nota)
}

func (s *equipoService) HistorialComponente(ctx context.Context, id int) ([]models.HistorialComponente, error) {
	if err := validarID(id); err != nil {
		return nil, err
	}
	if _, err := s.repo.GetComponente(ctx, id); err != nil {
		return nil, err
	}
	return s.repo.HistorialComponente(ctx, id)
}
