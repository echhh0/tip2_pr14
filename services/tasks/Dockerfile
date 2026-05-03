FROM golang:1.26-alpine AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY proto ./proto
COPY shared ./shared
COPY services/tasks ./services/tasks

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/tasks ./services/tasks/cmd/tasks

FROM alpine:3.20

RUN addgroup -S app && adduser -S -G app app \
    && apk add --no-cache ca-certificates

WORKDIR /app

COPY --from=builder /out/tasks /app/tasks

USER app

EXPOSE 8082

ENTRYPOINT ["/app/tasks"]