# Multi-stage build — final image is just the compiled binary on
# distroless, per the Technical Architecture document's ~15MB target.

FROM golang:1.22-bookworm AS builder
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /out/chalked-api ./cmd/api

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=builder /out/chalked-api /chalked-api
USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/chalked-api"]
