FROM golang:1.23 AS build
WORKDIR /src
COPY go.mod ./
RUN go mod download
COPY . .
ARG ROUTEGATE_VERSION=dev
ARG ROUTEGATE_GIT_COMMIT=unknown
ARG ROUTEGATE_BUILD_DATE=unknown
RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags "-X github.com/ikaevus/routegate/agent/internal/buildinfo.Version=${ROUTEGATE_VERSION} -X github.com/ikaevus/routegate/agent/internal/buildinfo.GitCommit=${ROUTEGATE_GIT_COMMIT} -X github.com/ikaevus/routegate/agent/internal/buildinfo.BuildDate=${ROUTEGATE_BUILD_DATE}" \
    -o /out/routegate-agent ./cmd/routegate-agent

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/routegate-agent /routegate-agent
ENTRYPOINT ["/routegate-agent"]
