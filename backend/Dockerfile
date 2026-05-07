FROM golang:1.26 AS build

WORKDIR /src

COPY go.mod go.sum ./

RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -o /out/routegate-manager ./cmd/routegate-manager

FROM gcr.io/distroless/static-debian12:nonroot

WORKDIR /app

COPY --from=build /out/routegate-manager /app/routegate-manager
COPY migrations /app/migrations

EXPOSE 8080

ENTRYPOINT ["/app/routegate-manager"]
