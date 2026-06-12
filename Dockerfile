# Talos builds three binaries (server + migrator + drainer) into one
# distroless image. The module lives at the repo root and consumes the
# published go-kit pinned in go.mod (no replace directive), so the build
# context is this repo.
ARG GO_VERSION=1.25
FROM --platform=$BUILDPLATFORM golang:${GO_VERSION}-alpine AS builder
ARG TARGETOS TARGETARCH
WORKDIR /src

# Pull dependencies first so they cache across source changes.
COPY go.mod go.sum ./
ENV GOWORK=off
RUN go mod download

# Build everything from the module root.
COPY . .
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -o /out/server   ./cmd/server
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -o /out/migrator ./cmd/migrator
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -o /out/drainer  ./cmd/drainer

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=builder /out/server   /app/server
COPY --from=builder /out/migrator /app/migrator
COPY --from=builder /out/drainer  /app/drainer
# 8080 = REST/OpenAPI, 9090 = gRPC
EXPOSE 8080 9090
USER nonroot:nonroot
ENTRYPOINT ["/app/server"]
