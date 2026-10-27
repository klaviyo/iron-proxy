# syntax=docker/dockerfile:1.7
FROM golang:1.27.3-alpine AS builder
WORKDIR /src
COPY . .
WORKDIR /src/ssh
RUN CGO_ENABLED=0 go build -trimpath -o /iron-ssh ./cmd/iron-ssh

FROM ironsh/iron-proxy:0.53.0@sha256:51f01ab057637c28672f2025216680cdc0b61dbc58a0337f77e0ea95dd6153a6
COPY --from=builder /iron-ssh /usr/local/bin/iron-ssh
