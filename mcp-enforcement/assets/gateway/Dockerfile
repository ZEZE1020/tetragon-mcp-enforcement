# syntax=docker/dockerfile:1
FROM golang:1.22-alpine AS build
WORKDIR /src
COPY go.mod ./
RUN go mod download || true
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /out/mcp-gateway .

FROM alpine:3.20
RUN apk add --no-cache ca-certificates curl \
    && addgroup -g 1000 -S mcp \
    && adduser -u 1000 -S mcp -G mcp
WORKDIR /
LABEL org.opencontainers.image.source="https://github.com/ZEZE1020/tetragon-mcp-enforcement"
LABEL org.opencontainers.image.description="MCP enforcement gateway, tool-runner, and approved-backend multi-mode container"
LABEL org.opencontainers.image.licenses="Apache-2.0"
COPY --from=build /out/mcp-gateway /mcp-gateway
EXPOSE 8080 8081 8090
USER 1000:1000
ENTRYPOINT ["/mcp-gateway"]
