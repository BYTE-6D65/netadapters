# Build stage
FROM golang:1.23-alpine AS builder

WORKDIR /build

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the application
ARG EXAMPLE=http-echo
RUN cd examples/${EXAMPLE} && go build -o /app

# Runtime stage
FROM alpine:latest

RUN apk --no-cache add ca-certificates

WORKDIR /root/

# Copy binary from builder
COPY --from=builder /app .

# Expose default port
EXPOSE 8080

# Run the application
CMD ["./app"]
