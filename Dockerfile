# syntax=docker/dockerfile:1
FROM golang:1.26 AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o /out/controlplane ./cmd/controlplane

FROM gcr.io/distroless/static:nonroot
WORKDIR /app
COPY --from=builder /out/controlplane /app/controlplane
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/app/controlplane"]
