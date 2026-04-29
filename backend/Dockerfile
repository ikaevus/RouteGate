FROM golang:1.23 AS build
WORKDIR /src
COPY go.mod ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/routegate-manager ./cmd/routegate-manager

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/routegate-manager /routegate-manager
EXPOSE 8080
ENTRYPOINT ["/routegate-manager"]
