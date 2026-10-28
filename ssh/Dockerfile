# syntax=docker/dockerfile:1.7
FROM golang:1.27.3-alpine AS source
WORKDIR /src
COPY . .
ENV GOFLAGS=-mod=readonly

# Run only credential-free unit tests; real cloud integrations remain separate.
FROM source AS test
RUN apk add --no-cache gcc musl-dev
RUN go test -race -count=1 ./cmd/... ./internal/... ./gen/... && go vet ./cmd/... ./internal/... ./gen/...
RUN cd ssh && go test -race -count=1 ./... && go vet ./...

FROM source AS builder
RUN CGO_ENABLED=0 go build -trimpath -o /iron-proxy ./cmd/iron-proxy
WORKDIR /src/ssh
RUN CGO_ENABLED=0 go build -trimpath -o /iron-ssh ./cmd/iron-ssh

FROM ironsh/iron-proxy:0.53.0@sha256:51f01ab057637c28672f2025216680cdc0b61dbc58a0337f77e0ea95dd6153a6
# Build both binaries from this checkout rather than shipping the base's HTTP binary.
COPY --from=builder /iron-proxy /usr/local/bin/iron-proxy
COPY --from=builder /iron-ssh /usr/local/bin/iron-ssh
