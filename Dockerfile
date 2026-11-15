# syntax=docker/dockerfile:1

FROM golang:1.22-alpine AS build

ARG VERSION=dev

WORKDIR /src

COPY go.mod ./
COPY . .

RUN CGO_ENABLED=0 go build \
    -trimpath \
    -ldflags="-s -w -X github.com/arthuRHD/unkindled-mcp/internal/mcp.Version=${VERSION}" \
    -o /out/unkindled-mcp \
    ./cmd/unkindled-mcp

FROM gcr.io/distroless/static-debian12:nonroot

LABEL org.opencontainers.image.title="unkindled-mcp" \
      org.opencontainers.image.description="MCP server for Unkindled workstations" \
      org.opencontainers.image.source="https://github.com/arthuRHD/unkindled-mcp"

COPY --from=build /out/unkindled-mcp /usr/local/bin/unkindled-mcp

USER nonroot:nonroot
ENTRYPOINT ["/usr/local/bin/unkindled-mcp"]
