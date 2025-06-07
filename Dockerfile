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

# Install ca-certificates, tzdata, bash, and netcat-openbsd
RUN apk --no-cache add ca-certificates tzdata bash netcat-openbsd

# Copy the binary from the builder stage
COPY --from=builder /app/codular-backend /app/codular-backend

# Copy wait-for-it.sh, fix line endings, and make executable
COPY wait-for-it.sh /app/wait-for-it.sh
RUN sed -i 's/\r$//' /app/wait-for-it.sh && \
    chmod +x /app/wait-for-it.sh

# Copy .env file
COPY .env /app/.env
COPY ./config/local.yaml /app/config/local.yaml
COPY ./config/system_prompts.yaml /app/config/system_prompts.yaml
COPY ./config/skips_check_prompt.yaml /app/config/skips_check_prompt.yaml

# Expose the backend port
EXPOSE 8082

# Command to run the backend
CMD ["/app/wait-for-it.sh", "db:5432", "-t", "60", "--", "/app/codular-backend"]