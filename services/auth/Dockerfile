FROM golang:1.26-alpine AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY proto ./proto
COPY shared ./shared
COPY services/auth ./services/auth

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/auth ./services/auth/cmd/auth

FROM alpine:3.20

RUN addgroup -S app && adduser -S -G app app \
    && apk add --no-cache ca-certificates

WORKDIR /app

COPY --from=builder /out/auth /app/auth

USER app

EXPOSE 8081 50051

ENTRYPOINT ["/app/auth"]