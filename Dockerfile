FROM golang:1.23.2-alpine

WORKDIR /app

# Install bash and necessary tools
RUN apk add --no-cache bash

# Copy go modules and download dependencies
COPY go.mod go.sum ./
RUN go mod download

# Copy the entire codebase
COPY . .

# Copy and make the wait-for-it script executable
COPY wait-for-it.sh /wait-for-it.sh
RUN chmod +x /wait-for-it.sh

# Build the Go binary
RUN go build -o codular-backend ./cmd/codular-backend

EXPOSE 8082

# Start the application with wait-for-it
CMD ["/wait-for-it.sh", "db:5432", "-t", "30", "--", "./codular-backend"]