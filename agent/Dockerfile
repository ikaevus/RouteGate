FROM golang:1.23 AS build
WORKDIR /src
COPY go.mod ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/routegate-agent ./cmd/routegate-agent

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/routegate-agent /routegate-agent
ENTRYPOINT ["/routegate-agent"]
