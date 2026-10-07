# ---- Compilación ----
FROM golang:1.23-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/server ./cmd/server

# ---- Ejecución ----
FROM alpine:3.20
RUN apk add --no-cache tzdata ca-certificates wget \
 && adduser -D -u 10001 inventario
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
HEALTHCHECK --interval=30s --timeout=3s CMD wget -qO- http://localhost:8080/healthz || exit 1
ENTRYPOINT ["/app/server"]
