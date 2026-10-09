# ---- Compilación ----
FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY . .
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/server ./cmd/server

# ---- Ejecución ----
FROM alpine:3.22
# tzdata: hora local en los registros; ca-certificates: TLS hacia SQL Server.
# wget para el healthcheck ya viene en busybox.
RUN apk add --no-cache tzdata ca-certificates \
 && adduser -D -H -u 10001 inventario
WORKDIR /app
COPY --from=build /out/server /app/server
COPY templates /app/templates
RUN mkdir -p /app/data && chown inventario /app/data

ENV INVENTARIO_PUERTO=8080 \
    INVENTARIO_EXCEL=/app/data/inventario.xlsx \
    INVENTARIO_TEMPLATES=/app/templates/web \
    TZ=America/Argentina/Buenos_Aires

USER inventario
EXPOSE 8080
VOLUME ["/app/data"]
# /api/health comprueba también el almacenamiento (Excel o SQL Server):
# si la base no responde, `docker ps` muestra el contenedor como unhealthy.
HEALTHCHECK --interval=30s --timeout=5s --start-period=20s --retries=3 \
    CMD wget -qO- "http://127.0.0.1:${INVENTARIO_PUERTO}/api/health" >/dev/null || exit 1
ENTRYPOINT ["/app/server"]
