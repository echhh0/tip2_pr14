FROM golang:1.26-alpine AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY shared ./shared
COPY services/worker ./services/worker

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/worker ./services/worker/cmd/worker

FROM alpine:3.20

RUN addgroup -S app && adduser -S -G app app \
    && apk add --no-cache ca-certificates

WORKDIR /app

COPY --from=builder /out/worker /app/worker

USER app

ENTRYPOINT ["/app/worker"]
