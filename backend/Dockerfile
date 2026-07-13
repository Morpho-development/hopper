FROM golang:1.26.5-alpine AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/hopper ./cmd/hopper

FROM alpine:3.22

RUN addgroup -S hopper && adduser -S hopper -G hopper

WORKDIR /app
COPY --from=builder /out/hopper /app/hopper

RUN mkdir -p /data /storage && chown -R hopper:hopper /data /storage /app

USER hopper
EXPOSE 8080

ENTRYPOINT ["/app/hopper"]
