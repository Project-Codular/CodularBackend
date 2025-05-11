# Stage 1: Build the Go binary
FROM golang:1.23-alpine AS builder

WORKDIR /app

# Copy go.mod and go.sum for dependency caching
COPY go.mod go.sum ./
RUN go mod download

# Copy the source code
COPY . .

# Build the binary with optimizations
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o codular-backend ./cmd/codular-backend/main.go

# Stage 2: Create minimal runtime image
FROM alpine:3.20

WORKDIR /app

# Install ca-certificates for HTTPS requests and tzdata for time zones
RUN apk --no-cache add ca-certificates tzdata

# Copy the binary from the builder stage
COPY --from=builder /app/codular-backend .

# Copy wait-for-it.sh for database dependency
COPY --from=builder /app/wait-for-it.sh .
RUN chmod +x wait-for-it.sh

# Expose the backend port
EXPOSE 8082

# Command to run the backend
CMD ["./wait-for-it.sh", "db:5432", "-t", "60", "--", "./codular-backend"]