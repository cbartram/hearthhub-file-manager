# Build stage
FROM golang:1.23-alpine AS builder

WORKDIR /app

COPY go.mod go.sum main.go ./

RUN go mod download

RUN CGO_ENABLED=0 GOOS=linux go build -o plugin-manager .

FROM alpine:3.18

WORKDIR /app

RUN apk add --no-cache ca-certificates

COPY --from=builder /app/plugin-manager .

CMD ["./plugin-manager"]