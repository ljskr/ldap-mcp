# syntax=docker/dockerfile:1

FROM golang:1.24-trixie AS builder

# Mainland China-friendly defaults; override: docker build --build-arg GOPROXY=...
ARG GOPROXY=https://goproxy.cn,direct
ARG GOSUMDB=sum.golang.google.cn
ENV GOPROXY=${GOPROXY} GOSUMDB=${GOSUMDB}

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/ldap-mcp ./cmd/server

FROM alpine:3.21

RUN apk add --no-cache ca-certificates

COPY --from=builder /out/ldap-mcp /ldap-mcp

USER nobody:nobody

EXPOSE 8080

ENTRYPOINT ["/ldap-mcp"]
