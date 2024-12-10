FROM golang:1.23 AS builder

WORKDIR /app

COPY go.mod go.sum ./

RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o app .

FROM alpine:3.20

RUN apk --no-cache add ca-certificates

WORKDIR /

COPY --from=builder /app/app /app

EXPOSE 3000

ENTRYPOINT ["/app"]
