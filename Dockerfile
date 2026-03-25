# Stage 1: Build
FROM golang:1.25-alpine AS builder

# Install make
RUN apk add --no-cache make

WORKDIR /app
COPY . .

# build via Makefile
RUN make build-bin

# Stage 2: Runtime
FROM alpine:latest
WORKDIR /root/
# Copy the binary generated
COPY --from=builder /app/bin/k8s-rightsizer .

CMD ["./k8s-rightsizer"]