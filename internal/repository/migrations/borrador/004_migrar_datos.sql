-- Copia Items al modelo nuevo (003_modelo_nuevo.sql), en una sola transacción:
-- si algo no cierra, no queda nada cargado. Items, Movements y Categories no se
-- tocan; Movements queda como historial del sistema anterior (IDAnterior
-- vincula cada registro nuevo con su ítem de antes).
--
--   Equipos:     ítems de tipo equipo (CPU y Gabinete pasan a ser el tipo "PC");
--                los que no tienen N° de inventario quedan pendientes de numerar.
--   Componentes: procesadores, RAM y fuentes; en su PC o, si están sueltos, en su oficina.
--   Insumos:     se agrupan por tipo + marca + modelo sumando la cantidad (un solo stock).
--   Oficinas:    una por cada ubicación distinta. Personas: no hay en los datos actuales.
--   Bajas:       fecha de la última modificación y motivo "Migrado del sistema anterior".

SET XACT_ABORT ON;
BEGIN TRANSACTION;

IF EXISTS (SELECT 1 FROM dbo.Equipos) OR EXISTS (SELECT 1 FROM dbo.Componentes) OR EXISTS (SELECT 1 FROM dbo.Insumos)
    THROW 50001, N'El modelo nuevo ya tiene datos: la migración solo corre una vez.', 1;

DECLARE @sistema INT = (SELECT ID FROM dbo.Usuarios WHERE Nombre = N'Sistema');
DECLARE @motivo NVARCHAR(100) = N'Migrado del sistema anterior';
DECLARE @msg NVARCHAR(2048);

-- Clasificación de cada tipo del sistema anterior (acordada con el usuario).
DECLARE @clases TABLE (
    Anterior NVARCHAR(100) COLLATE DATABASE_DEFAULT PRIMARY KEY,
    Nombre   NVARCHAR(100) COLLATE DATABASE_DEFAULT NOT NULL,
    Clase    VARCHAR(12)   NOT NULL
);
INSERT INTO @clases (Anterior, Nombre, Clase) VALUES
    (N'CPU', N'PC', 'EQUIPO'), (N'Gabinete', N'PC', 'EQUIPO'),
    (N'Monitor', N'Monitor', 'EQUIPO'), (N'Notebook', N'Notebook', 'EQUIPO'),
    (N'Impresora', N'Impresora', 'EQUIPO'), (N'Escaner', N'Escaner', 'EQUIPO'),
    (N'Estabilizador', N'Estabilizador', 'EQUIPO'), (N'UPS', N'UPS', 'EQUIPO'),
    (N'Parlante', N'Parlante', 'EQUIPO'), (N'Trituradora', N'Trituradora', 'EQUIPO'),
    (N'Procesador', N'Procesador', 'COMPONENTE'), (N'RAM', N'RAM', 'COMPONENTE'),
    (N'Fuente de alimentacion', N'Fuente de alimentacion', 'COMPONENTE'),
    (N'Headset', N'Headset', 'INSUMO'), (N'Pendrive', N'Pendrive', 'INSUMO');

-- Controles previos: cualquier dato que no encaje frena la migración con un
-- mensaje que dice qué corregir.
SET @msg = (SELECT STRING_AGG(DeviceType, N', ') FROM (SELECT DISTINCT i.DeviceType FROM dbo.Items i
            WHERE NOT EXISTS (SELECT 1 FROM @clases c WHERE c.Anterior = i.DeviceType)) d);
IF @msg IS NOT NULL BEGIN SET @msg = N'Tipos sin clasificar: ' + @msg; THROW 50002, @msg, 1; END;

SET @msg = (SELECT STRING_AGG(CONVERT(NVARCHAR(20), i.ID), N', ') FROM dbo.Items i JOIN @clases c ON c.Anterior = i.DeviceType
            WHERE c.Clase IN ('EQUIPO', 'COMPONENTE') AND i.Quantity <> 1);
IF @msg IS NOT NULL BEGIN SET @msg = N'Equipos o componentes con cantidad distinta de 1 (IDs): ' + @msg; THROW 50003, @msg, 1; END;

SET @msg = (SELECT STRING_AGG(CONVERT(NVARCHAR(20), i.ID), N', ') FROM dbo.Items i JOIN @clases c ON c.Anterior = i.DeviceType
            WHERE (c.Clase = 'EQUIPO' AND i.EquipmentID IS NOT NULL)
               OR (c.Clase = 'INSUMO' AND (i.EquipmentID IS NOT NULL OR i.HasInventory = 1)));
IF @msg IS NOT NULL BEGIN SET @msg = N'Equipos vinculados a una PC o insumos con N° de inventario (IDs): ' + @msg; THROW 50004, @msg, 1; END;

SET @msg = (SELECT STRING_AGG(CONVERT(NVARCHAR(20), ID), N', ') FROM dbo.Items WHERE LTRIM(RTRIM(Location)) = N'');
IF @msg IS NOT NULL BEGIN SET @msg = N'Ítems sin ubicación (IDs): ' + @msg; THROW 50005, @msg, 1; END;

SET @msg = (SELECT STRING_AGG(CONVERT(NVARCHAR(20), ID), N', ') FROM dbo.Items WHERE Availability = 'PRESTADO' OR AssignedTo <> N'');
IF @msg IS NOT NULL BEGIN SET @msg = N'Ítems prestados o asignados: cargar antes las personas (IDs): ' + @msg; THROW 50006, @msg, 1; END;

-- Oficinas y tipos.
INSERT INTO dbo.Oficinas (Nombre, ActualizadoPor)
SELECT DISTINCT LTRIM(RTRIM(Location)), @sistema FROM dbo.Items;

INSERT INTO dbo.Tipos (Nombre, Clase, Prestable, ActualizadoPor)
SELECT c.Nombre, c.Clase,
       CONVERT(BIT, MAX(CASE WHEN c.Clase = 'EQUIPO' AND cat.Loanable = 1 THEN 1 ELSE 0 END)), @sistema
FROM @clases c
LEFT JOIN dbo.Categories cat ON cat.Name IN (c.Anterior, c.Nombre)
WHERE EXISTS (SELECT 1 FROM dbo.Items i WHERE i.DeviceType = c.Anterior)
GROUP BY c.Nombre, c.Clase;

-- Equipos.
INSERT INTO dbo.Equipos (IDAnterior, NumeroInventario, TipoID, Marca, Modelo, NumeroSerie, Estado, OficinaID,
                         Observacion, FechaBaja, MotivoBaja, CreadoEn, ActualizadoEn, ActualizadoPor)
SELECT i.ID, i.InventoryNumber, t.ID, i.Brand, i.Model, i.SerialNumber, i.Status, o.ID, i.Notes,
       CASE WHEN i.Status = 'BAJA' THEN i.UpdatedAt END,
       CASE WHEN i.Status = 'BAJA' THEN @motivo ELSE N'' END,
       i.CreatedAt, i.UpdatedAt, @sistema
FROM dbo.Items i
JOIN @clases c    ON c.Anterior = i.DeviceType AND c.Clase = 'EQUIPO'
JOIN dbo.Tipos t  ON t.Nombre = c.Nombre
JOIN dbo.Oficinas o ON o.Nombre = LTRIM(RTRIM(i.Location));

-- Componentes: en su PC (la PC ya migrada) o, si están sueltos, en su oficina.
INSERT INTO dbo.Componentes (IDAnterior, NumeroInventario, TipoID, Marca, Modelo, NumeroSerie, Estado, EquipoID, OficinaID,
                             Observacion, FechaBaja, MotivoBaja, CreadoEn, ActualizadoEn, ActualizadoPor)
SELECT i.ID, i.InventoryNumber, t.ID, i.Brand, i.Model, i.SerialNumber, i.Status, e.ID,
       CASE WHEN i.EquipmentID IS NULL THEN o.ID END, i.Notes,
       CASE WHEN i.Status = 'BAJA' THEN i.UpdatedAt END,
       CASE WHEN i.Status = 'BAJA' THEN @motivo ELSE N'' END,
       i.CreatedAt, i.UpdatedAt, @sistema
FROM dbo.Items i
JOIN @clases c    ON c.Anterior = i.DeviceType AND c.Clase = 'COMPONENTE'
JOIN dbo.Tipos t  ON t.Nombre = c.Nombre
JOIN dbo.Oficinas o ON o.Nombre = LTRIM(RTRIM(i.Location))
LEFT JOIN dbo.Equipos e ON e.IDAnterior = i.EquipmentID;

SET @msg = (SELECT STRING_AGG(CONVERT(NVARCHAR(20), i.ID), N', ') FROM dbo.Items i
            JOIN dbo.Componentes k ON k.IDAnterior = i.ID WHERE i.EquipmentID IS NOT NULL AND k.EquipoID IS NULL);
IF @msg IS NOT NULL BEGIN SET @msg = N'Componentes vinculados a algo que no es un equipo (IDs): ' + @msg; THROW 50007, @msg, 1; END;

-- Insumos: un registro por tipo + marca + modelo con la suma de las cantidades.
-- Queda inactivo si todas sus filas anteriores estaban de baja.
INSERT INTO dbo.Insumos (IDAnterior, TipoID, Marca, Modelo, Stock, Observacion, Activo, CreadoEn, ActualizadoEn, ActualizadoPor)
SELECT MIN(i.ID), t.ID, MIN(i.Brand), MIN(i.Model), SUM(i.Quantity),
       LEFT(ISNULL(STRING_AGG(CONVERT(NVARCHAR(MAX), NULLIF(i.Notes, N'')), N' | '), N''), 1000),
       CONVERT(BIT, MAX(CASE WHEN i.Status <> 'BAJA' THEN 1 ELSE 0 END)),
       MIN(i.CreatedAt), MAX(i.UpdatedAt), @sistema
FROM dbo.Items i
JOIN @clases c   ON c.Anterior = i.DeviceType AND c.Clase = 'INSUMO'
JOIN dbo.Tipos t ON t.Nombre = c.Nombre
GROUP BY t.ID, i.Brand, i.Model;

-- Punto de partida del historial nuevo.
INSERT INTO dbo.MovimientosInsumo (InsumoID, Tipo, Cantidad, Observacion, Fecha, UsuarioID)
SELECT ID, 'AJUSTE', Stock, N'Stock inicial migrado del sistema anterior', ActualizadoEn, @sistema
FROM dbo.Insumos WHERE Stock > 0;

INSERT INTO dbo.HistorialEquipos (EquipoID, OficinaNuevaID, Estado, Observacion, Fecha, UsuarioID)
SELECT ID, OficinaID, Estado, @motivo, CreadoEn, @sistema FROM dbo.Equipos;

INSERT INTO dbo.HistorialComponentes (ComponenteID, EquipoNuevoID, Observacion, Fecha, UsuarioID)
SELECT ID, EquipoID, @motivo, CreadoEn, @sistema FROM dbo.Componentes;

-- Verificación: cada ítem anterior tiene que estar en exactamente un lugar y el
-- stock total de insumos no puede cambiar.
DECLARE @items INT = (SELECT COUNT(*) FROM dbo.Items);
DECLARE @migrados INT = (SELECT COUNT(*) FROM dbo.Equipos) + (SELECT COUNT(*) FROM dbo.Componentes)
    + (SELECT COUNT(*) FROM dbo.Items i JOIN @clases c ON c.Anterior = i.DeviceType AND c.Clase = 'INSUMO');
IF @migrados <> @items
BEGIN
    SET @msg = CONCAT(N'Se migraron ', @migrados, N' de ', @items, N' ítems.'); THROW 50008, @msg, 1;
END;
IF (SELECT ISNULL(SUM(Stock), 0) FROM dbo.Insumos) <> (SELECT ISNULL(SUM(i.Quantity), 0) FROM dbo.Items i
        JOIN @clases c ON c.Anterior = i.DeviceType AND c.Clase = 'INSUMO')
    THROW 50009, N'El stock total de insumos no coincide con el anterior.', 1;

COMMIT TRANSACTION;

-- Resumen.
SELECT N'Ítems anteriores' AS Concepto, @items AS Cantidad
UNION ALL SELECT N'Oficinas', COUNT(*) FROM dbo.Oficinas
UNION ALL SELECT N'Tipos', COUNT(*) FROM dbo.Tipos
UNION ALL SELECT N'Equipos', COUNT(*) FROM dbo.Equipos
UNION ALL SELECT N'  pendientes de numerar', COUNT(*) FROM dbo.Equipos WHERE NumeroInventario = N''
UNION ALL SELECT N'Componentes', COUNT(*) FROM dbo.Componentes
UNION ALL SELECT N'  dentro de una PC', COUNT(*) FROM dbo.Componentes WHERE EquipoID IS NOT NULL
UNION ALL SELECT N'Insumos', COUNT(*) FROM dbo.Insumos
UNION ALL SELECT N'Stock total de insumos', ISNULL(SUM(Stock), 0) FROM dbo.Insumos;
