FROM golang:1.26-alpine AS builder
ARG CMD=server
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -o app ./cmd/${CMD}

FROM alpine:latest
WORKDIR /app
COPY --from=builder /app/app .
EXPOSE 8080 8081
CMD ["./app"]
