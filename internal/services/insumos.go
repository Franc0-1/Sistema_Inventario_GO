package services

import (
	"context"
	"errors"
	"fmt"

	"inventario/internal/models"
	"inventario/internal/repository"
)

// Límite de filas del listado de movimientos de insumos.
const (
	LimiteMovimientosPorDefecto = 200
	LimiteMovimientosMaximo     = 1000
)

// InsumoService administra los insumos y su stock. Las escrituras exigen el
// usuario. El stock solo cambia con RegistrarMovimiento; en un AJUSTE,
// Cantidad es el stock contado.
type InsumoService interface {
	ListarInsumos(ctx context.Context, f models.FiltroInsumos) ([]models.Insumo, error)
	ObtenerInsumo(ctx context.Context, id int) (models.Insumo, error)
	CrearInsumo(ctx context.Context, i models.Insumo) (models.Insumo, error)
	ActualizarInsumo(ctx context.Context, i models.Insumo) (models.Insumo, error)
	RegistrarMovimiento(ctx context.Context, m models.MovimientoInsumo) (models.MovimientoInsumo, models.Insumo, error)
	ListarMovimientos(ctx context.Context, f models.FiltroMovimientosInsumo) ([]models.MovimientoInsumo, error)
}

type insumoService struct {
	repo repository.InsumoRepository
	cat  *catalogoService
}

func NewInsumoService(repo repository.InsumoRepository, catalogos repository.CatalogoRepository) InsumoService {
	return &insumoService{repo: repo, cat: &catalogoService{repo: catalogos}}
}

func (s *insumoService) catalogos() repository.CatalogoRepository { return s.cat.repo }

func (s *insumoService) ListarInsumos(ctx context.Context, f models.FiltroInsumos) ([]models.Insumo, error) {
	return s.repo.ListInsumos(ctx, f)
}

func (s *insumoService) ObtenerInsumo(ctx context.Context, id int) (models.Insumo, error) {
	if err := validarID(id); err != nil {
		return models.Insumo{}, err
	}
	return s.repo.GetInsumo(ctx, id)
}

func (s *insumoService) validarInsumo(ctx context.Context, i models.Insumo, tipoAnterior int) (models.Insumo, error) {
	var err error
	for _, c := range []struct {
		v     *string
		campo string
		max   int
	}{{&i.Marca, "la marca", 100}, {&i.Modelo, "el modelo", 100}, {&i.Observacion, "la observación", 1000}} {
		if *c.v, err = texto(*c.v, c.campo, c.max, false); err != nil {
			return i, err
		}
	}
	if i.StockMinimo < 0 {
		return i, fmt.Errorf("%w: el stock mínimo no puede ser negativo", ErrDatoInvalido)
	}
	if i.TipoID <= 0 {
		return i, fmt.Errorf("%w: el tipo es obligatorio", ErrDatoInvalido)
	}
	if i.TipoID != tipoAnterior {
		if err := s.cat.tipoDeClase(ctx, i.TipoID, models.ClaseInsumo); err != nil {
			return i, err
		}
	}
	return i, nil
}

func (s *insumoService) CrearInsumo(ctx context.Context, i models.Insumo) (models.Insumo, error) {
	usuario, err := actorDe(ctx, s.catalogos(), true)
	if err != nil {
		return i, err
	}
	if i.Stock < 0 {
		return i, fmt.Errorf("%w: el stock inicial no puede ser negativo", ErrDatoInvalido)
	}
	if i, err = s.validarInsumo(ctx, i, 0); err != nil {
		return i, err
	}
	return s.repo.CreateInsumo(ctx, i, usuario)
}

func (s *insumoService) ActualizarInsumo(ctx context.Context, i models.Insumo) (models.Insumo, error) {
	if err := validarEdicion(i.ID, i.Version); err != nil {
		return i, err
	}
	usuario, err := actorDe(ctx, s.catalogos(), true)
	if err != nil {
		return i, err
	}
	anterior, err := s.repo.GetInsumo(ctx, i.ID)
	if err != nil {
		return i, err
	}
	if i, err = s.validarInsumo(ctx, i, anterior.TipoID); err != nil {
		return i, err
	}
	return s.repo.UpdateInsumo(ctx, i, usuario)
}

// RegistrarMovimiento valida el movimiento. Una entrega a una persona queda
// también registrada con su oficina (si no se indicó otra), así se puede
// consultar qué insumos se llevó cada oficina.
func (s *insumoService) RegistrarMovimiento(ctx context.Context, m models.MovimientoInsumo) (models.MovimientoInsumo, models.Insumo, error) {
	var insumo models.Insumo
	if err := validarID(m.InsumoID); err != nil {
		return m, insumo, err
	}
	usuario, err := actorDe(ctx, s.catalogos(), true)
	if err != nil {
		return m, insumo, err
	}
	tipo, ok := oneOf(string(m.Tipo), models.TiposMovimientoInsumo)
	if !ok {
		return m, insumo, fmt.Errorf("%w: tipo %q desconocido (válidos: ENTRADA, SALIDA, AJUSTE)", ErrDatoInvalido, m.Tipo)
	}
	m.Tipo = tipo
	switch {
	case m.Tipo == models.MovimientoAjuste && m.Cantidad < 0:
		return m, insumo, fmt.Errorf("%w: el stock contado no puede ser negativo", ErrDatoInvalido)
	case m.Tipo != models.MovimientoAjuste && m.Cantidad <= 0:
		return m, insumo, fmt.Errorf("%w: la cantidad debe ser mayor que 0", ErrDatoInvalido)
	case m.PersonaID < 0 || m.OficinaID < 0:
		return m, insumo, fmt.Errorf("%w: persona u oficina inválida", ErrDatoInvalido)
	}
	if m.Observacion, err = texto(m.Observacion, "la observación", 1000, false); err != nil {
		return m, insumo, err
	}
	if m.PersonaID > 0 {
		p, err := s.catalogos().GetPersona(ctx, m.PersonaID)
		switch {
		case errors.Is(err, repository.ErrNoEncontrado):
			return m, insumo, fmt.Errorf("%w: la persona %d no existe", ErrDatoInvalido, m.PersonaID)
		case err != nil:
			return m, insumo, err
		case !p.Activo:
			return m, insumo, fmt.Errorf("%w: %s está inactiva", ErrDatoInvalido, p.NombreCompleto())
		}
		if m.OficinaID == 0 {
			m.OficinaID = p.OficinaID
		}
	}
	if m.OficinaID > 0 {
		if err := s.cat.referenciaActiva(ctx, models.CatalogoOficinas, "la oficina", "inactiva", m.OficinaID); err != nil {
			return m, insumo, err
		}
	}
	if m.Tipo == models.MovimientoSalida && m.PersonaID == 0 && m.OficinaID == 0 {
		return m, insumo, fmt.Errorf("%w: indicá a quién o a qué oficina se entrega", ErrDatoInvalido)
	}
	return s.repo.RegistrarMovimiento(ctx, m, usuario)
}

func (s *insumoService) ListarMovimientos(ctx context.Context, f models.FiltroMovimientosInsumo) ([]models.MovimientoInsumo, error) {
	if f.Tipo != "" {
		tipo, ok := oneOf(string(f.Tipo), models.TiposMovimientoInsumo)
		if !ok {
			return nil, fmt.Errorf("%w: tipo %q desconocido", ErrDatoInvalido, f.Tipo)
		}
		f.Tipo = tipo
	}
	switch {
	case f.Limite == 0:
		f.Limite = LimiteMovimientosPorDefecto
	case f.Limite < 0 || f.Limite > LimiteMovimientosMaximo:
		return nil, fmt.Errorf("%w: el límite debe estar entre 1 y %d", ErrDatoInvalido, LimiteMovimientosMaximo)
	}
	if !f.Desde.IsZero() && !f.Hasta.IsZero() && f.Hasta.Before(f.Desde) {
		return nil, fmt.Errorf("%w: la fecha hasta es anterior a la fecha desde", ErrDatoInvalido)
	}
	return s.repo.ListMovimientosInsumo(ctx, f)
}
