FROM golang:1.26 AS build

WORKDIR /src

COPY go.mod go.sum ./

RUN go mod download

COPY . .

ARG ROUTEGATE_VERSION=dev
ARG ROUTEGATE_GIT_COMMIT=unknown
ARG ROUTEGATE_BUILD_DATE=unknown

RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags "-X github.com/ikaevus/routegate/backend/internal/buildinfo.Version=${ROUTEGATE_VERSION} -X github.com/ikaevus/routegate/backend/internal/buildinfo.GitCommit=${ROUTEGATE_GIT_COMMIT} -X github.com/ikaevus/routegate/backend/internal/buildinfo.BuildDate=${ROUTEGATE_BUILD_DATE}" \
    -o /out/routegate-manager ./cmd/routegate-manager

FROM gcr.io/distroless/static-debian12:nonroot

WORKDIR /app

COPY --from=build /out/routegate-manager /app/routegate-manager
COPY migrations /app/migrations

EXPOSE 8080

ENTRYPOINT ["/app/routegate-manager"]
