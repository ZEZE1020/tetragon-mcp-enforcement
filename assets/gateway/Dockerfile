# syntax=docker/dockerfile:1
FROM golang:1.22-alpine AS build
WORKDIR /src
COPY go.mod ./
RUN go mod download || true
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/mcp-gateway .

FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /
COPY --from=build /out/mcp-gateway /mcp-gateway
EXPOSE 8090
USER nonroot:nonroot
ENTRYPOINT ["/mcp-gateway"]
