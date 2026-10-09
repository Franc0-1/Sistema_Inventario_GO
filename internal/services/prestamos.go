package services

import (
	"context"
	"errors"
	"fmt"
	"time"

	"inventario/internal/models"
	"inventario/internal/repository"
)

// PrestamoService registra préstamos en el día y sus devoluciones. Las
// escrituras exigen el usuario.
type PrestamoService interface {
	ListarPrestamos(ctx context.Context, f models.FiltroPrestamos) ([]models.Prestamo, error)
	ObtenerPrestamo(ctx context.Context, id int) (models.Prestamo, error)
	// Prestar: sin DevolucionPrevista, el equipo vuelve hoy a la hora de salida.
	Prestar(ctx context.Context, p models.Prestamo) (models.Prestamo, error)
	Devolver(ctx context.Context, id int, nota string) (models.Prestamo, error)
}

type prestamoService struct {
	repo       repository.PrestamoRepository
	equipos    repository.EquipoRepository
	cat        *catalogoService
	horaSalida time.Duration // desde la medianoche
	ahora      func() time.Time
}

func NewPrestamoService(repo repository.PrestamoRepository, equipos repository.EquipoRepository,
	catalogos repository.CatalogoRepository, horaSalida time.Duration) PrestamoService {
	return &prestamoService{repo: repo, equipos: equipos, cat: &catalogoService{repo: catalogos},
		horaSalida: horaSalida, ahora: time.Now}
}

func (s *prestamoService) ListarPrestamos(ctx context.Context, f models.FiltroPrestamos) ([]models.Prestamo, error) {
	switch {
	case f.Limite == 0:
		f.Limite = LimiteMovimientosPorDefecto
	case f.Limite < 0 || f.Limite > LimiteMovimientosMaximo:
		return nil, fmt.Errorf("%w: el límite debe estar entre 1 y %d", ErrDatoInvalido, LimiteMovimientosMaximo)
	}
	return s.repo.ListPrestamos(ctx, f)
}

func (s *prestamoService) ObtenerPrestamo(ctx context.Context, id int) (models.Prestamo, error) {
	if err := validarID(id); err != nil {
		return models.Prestamo{}, err
	}
	return s.repo.GetPrestamo(ctx, id)
}

// equipoPrestable verifica que el equipo exista, no esté de baja y su tipo se preste.
func (s *prestamoService) equipoPrestable(ctx context.Context, id int) error {
	e, err := s.equipos.GetEquipo(ctx, id)
	switch {
	case errors.Is(err, repository.ErrNoEncontrado):
		return fmt.Errorf("%w: el equipo %d no existe", ErrDatoInvalido, id)
	case err != nil:
		return err
	case e.Estado == models.StatusRetired:
		return fmt.Errorf("%w: el equipo %s está dado de baja", ErrDatoInvalido, e.NumeroInventario)
	}
	t, err := s.cat.repo.GetTipo(ctx, e.TipoID)
	if err != nil {
		return err
	}
	if !t.Prestable {
		return fmt.Errorf("%w: los equipos de tipo %q no se prestan", ErrDatoInvalido, t.Nombre)
	}
	return nil
}

func (s *prestamoService) Prestar(ctx context.Context, p models.Prestamo) (models.Prestamo, error) {
	usuario, err := actorDe(ctx, s.cat.repo, true)
	if err != nil {
		return p, err
	}
	if err := validarID(p.EquipoID); err != nil {
		return p, err
	}
	if p.PersonaID <= 0 {
		return p, fmt.Errorf("%w: indicá a quién se presta", ErrDatoInvalido)
	}
	if p.Observacion, err = texto(p.Observacion, "la observación", 1000, false); err != nil {
		return p, err
	}
	if err := s.equipoPrestable(ctx, p.EquipoID); err != nil {
		return p, err
	}
	persona, err := s.cat.repo.GetPersona(ctx, p.PersonaID)
	switch {
	case errors.Is(err, repository.ErrNoEncontrado):
		return p, fmt.Errorf("%w: la persona %d no existe", ErrDatoInvalido, p.PersonaID)
	case err != nil:
		return p, err
	case !persona.Activo:
		return p, fmt.Errorf("%w: %s está inactiva", ErrDatoInvalido, persona.NombreCompleto())
	}

	ahora := s.ahora()
	p.FechaSalida = ahora
	if p.DevolucionPrevista.IsZero() {
		anio, mes, dia := ahora.Date()
		p.DevolucionPrevista = time.Date(anio, mes, dia, 0, 0, 0, 0, ahora.Location()).Add(s.horaSalida)
		if !p.DevolucionPrevista.After(ahora) {
			return p, fmt.Errorf("%w: ya pasó la hora de salida (%s); indicá cuándo se devuelve",
				ErrDatoInvalido, p.DevolucionPrevista.Format("15:04"))
		}
	} else if !p.DevolucionPrevista.After(ahora) {
		return p, fmt.Errorf("%w: la devolución prevista tiene que ser posterior a ahora", ErrDatoInvalido)
	}
	return s.repo.CreatePrestamo(ctx, p, usuario)
}

func (s *prestamoService) Devolver(ctx context.Context, id int, nota string) (models.Prestamo, error) {
	if err := validarID(id); err != nil {
		return models.Prestamo{}, err
	}
	usuario, err := actorDe(ctx, s.cat.repo, true)
	if err != nil {
		return models.Prestamo{}, err
	}
	if nota, err = normalizarNota(nota); err != nil {
		return models.Prestamo{}, err
	}
	return s.repo.DevolverPrestamo(ctx, id, nota, usuario)
}
