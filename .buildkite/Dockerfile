# syntax=docker/dockerfile:1.7
FROM golang:1.27.3-alpine AS source
WORKDIR /src
COPY . .
ENV GOFLAGS=-mod=readonly

# Cloud-backed integrations need separate credentials; these are unit tests.
FROM source AS test
RUN apk add --no-cache gcc musl-dev
RUN go test -race -count=1 ./cmd/... ./internal/... ./gen/... && go vet ./cmd/... ./internal/... ./gen/...
# The SSH module arrives separately; include it once that feature is merged.
RUN if [ -f ssh/go.mod ]; then cd ssh && go test -race -count=1 ./... && go vet ./...; fi

FROM source AS builder
RUN mkdir -p /out && CGO_ENABLED=0 go build -trimpath -o /out/iron-proxy ./cmd/iron-proxy
RUN if [ -f ssh/go.mod ]; then cd ssh && CGO_ENABLED=0 go build -trimpath -o /out/iron-ssh ./cmd/iron-ssh; fi

FROM ironsh/iron-proxy:0.53.0@sha256:51f01ab057637c28672f2025216680cdc0b61dbc58a0337f77e0ea95dd6153a6
COPY --from=builder /out/ /usr/local/bin/
