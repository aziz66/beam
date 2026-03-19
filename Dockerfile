# Build stage
FROM golang:1.23-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags "-s -w" -o beam .

# Runtime stage
FROM alpine:3.20
RUN apk --no-cache add ca-certificates && \
    addgroup -S beam && adduser -S -G beam beam
WORKDIR /app
COPY --from=builder /app/beam .
RUN chown -R beam:beam /app
USER beam
EXPOSE 8080
VOLUME ["/app/data"]
ENTRYPOINT ["./beam"]
