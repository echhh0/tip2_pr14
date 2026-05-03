FROM golang:1.26-alpine AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /bin/graphql ./services/graphql/cmd/graphql

FROM alpine:3.22

WORKDIR /app

COPY --from=builder /bin/graphql /app/graphql

EXPOSE 8090

ENTRYPOINT ["/app/graphql"]