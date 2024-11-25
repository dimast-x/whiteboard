FROM golang:1.23 as builder

WORKDIR /app

COPY go.mod go.sum ./

RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /build ./...

FROM alpine:3.20

RUN apk --no-cache add ca-certificates

WORKDIR /

COPY --from=builder /build /build

EXPOSE 3000

ENTRYPOINT ["/build"]
