// Cliente de la API. Es el ÚNICO archivo del frontend que conoce URLs, métodos
// HTTP y el formato de las respuestas ({"data": ...} / {"error": {code, message}}).

// Ruta relativa: funciona con cualquier host, puerto o contenedor, porque el
// frontend lo sirve el mismo servidor que expone la API.
const API_BASE = "/api";
const API_INVENTARIO = `${API_BASE}/inventory`;
const API_MOVIMIENTOS = `${API_BASE}/movements`;

// Campos que el cliente puede enviar al crear o editar (mismos nombres JSON que
// devuelve la API). ID, fechas y préstamo los maneja el servidor.
const CAMPOS_EDITABLES = [
  "numero_inventario", "tiene_inventario", "tipo_dispositivo", "marca", "modelo",
  "numero_serie", "cantidad", "ubicacion", "estado", "observacion", "equipo_id",
];

// Error de la API con el código estable del backend (ITEM_NOT_FOUND, …).
class ApiError extends Error {
  constructor(code, message, status) {
    super(message);
    this.name = "ApiError";
    this.code = code;
    this.status = status;
  }
}

// solicitud envía a API_INVENTARIO + ruta. Para otro recurso, pasar la URL completa en `url`.
async function solicitud(metodo, ruta = "", cuerpo, url = API_INVENTARIO + ruta) {
  const opciones = { method: metodo, headers: { Accept: "application/json" } };
  if (cuerpo !== undefined) {
    if (cuerpo instanceof FormData) {
      opciones.body = cuerpo;
    } else {
      opciones.headers["Content-Type"] = "application/json";
      opciones.body = JSON.stringify(cuerpo);
    }
  }

  let resp;
  try {
    resp = await fetch(url, opciones);
  } catch {
    throw new ApiError("NETWORK", "No se pudo conectar con el servidor.", 0);
  }
  if (resp.status === 204) return null;

  let json = null;
  try {
    json = await resp.json();
  } catch {
    // Sin cuerpo JSON (p. ej. un proxy que responde HTML): se trata abajo.
  }
  if (!resp.ok) {
    const e = json && json.error;
    throw new ApiError((e && e.code) || `HTTP_${resp.status}`, (e && e.message) || resp.statusText, resp.status);
  }
  if (!json || !("data" in json)) {
    throw new ApiError("INVALID_RESPONSE", "Respuesta inesperada del servidor.", resp.status);
  }
  return json.data;
}

function soloEditables(datos) {
  return Object.fromEntries(CAMPOS_EDITABLES.filter((c) => c in datos).map((c) => [c, datos[c]]));
}

const porId = (id) => `/${encodeURIComponent(id)}`;

// conParametros arma la query string omitiendo los valores vacíos: el backend
// rechaza parámetros desconocidos o repetidos, no los vacíos, pero así la URL
// queda limpia.
function conParametros(ruta, parametros = {}) {
  const pares = Object.entries(parametros)
    .filter(([, valor]) => valor !== "" && valor !== null && valor !== undefined)
    .map(([nombre, valor]) => [nombre, String(valor)]);
  return pares.length ? `${ruta}?${new URLSearchParams(pares)}` : ruta;
}

const api = {
  getEquipments: () => solicitud("GET", "", undefined, `${API_BASE}/equipment?includeRetired=true`),
  setEquipment: (id, equipo_id) => solicitud("PUT", `${porId(id)}/equipment`, { equipo_id }),
  getSummary: () => solicitud("GET", "/summary"),
  getReports: (parametros) => solicitud("GET", conParametros("/reports", parametros)),
  importInventory: (archivo) => {
    const cuerpo = new FormData();
    cuerpo.append("file", archivo);
    return solicitud("POST", "/import", cuerpo);
  },
  exportInventory: async () => {
    let resp;
    try {
      resp = await fetch(`${API_INVENTARIO}/export`);
    } catch {
      throw new ApiError("NETWORK", "No se pudo conectar con el servidor.", 0);
    }
    if (!resp.ok) {
      let json;
      try { json = await resp.json(); } catch {}
      throw new ApiError(json?.error?.code || `HTTP_${resp.status}`, json?.error?.message || "No se pudo exportar el inventario.", resp.status);
    }
    const disposicion = resp.headers.get("Content-Disposition") || "";
    const nombre = disposicion.match(/filename="([^"]+)"/i)?.[1] || "inventario.xlsx";
    try {
      return { archivo: await resp.blob(), nombre };
    } catch {
      throw new ApiError("NETWORK", "No se pudo completar la descarga.", 0);
    }
  },
  // Listado con filtros, orden y paginación. Parámetros (todos opcionales):
  // q, deviceType, excludeDeviceType, brand, model, inventoryNumber, serialNumber,
  // location, status, availability, hasInventory, includeRetired, sort, order,
  // page, pageSize. Devuelve { items, pagination: { page, pageSize, totalItems, totalPages } }.
  getInventory: (parametros) => solicitud("GET", conParametros("", parametros)),
  getInventoryById: (id) => solicitud("GET", porId(id)),
  // Búsqueda general: mismos parámetros y respuesta que getInventory.
  searchInventory: (texto, parametros = {}) => solicitud("GET", conParametros("/search", { ...parametros, q: texto })),
  createInventoryItem: (datos) => solicitud("POST", "", soloEditables(datos)),
  updateInventoryItem: (id, datos) => solicitud("PUT", porId(id), soloEditables(datos)),
  deleteInventoryItem: (id) => solicitud("DELETE", porId(id)),
  updateInventoryStock: (id, cantidad) => solicitud("PATCH", `${porId(id)}/stock`, { cantidad }),

  // Historial (solo lectura)
  getMovements: () => solicitud("GET", "", undefined, API_MOVIMIENTOS),
  getItemMovements: (id) => solicitud("GET", `${porId(id)}/movements`),
};
