# Stage 1: Build
FROM golang:1.25-alpine AS builder
RUN apk add --no-cache make git
WORKDIR /app
COPY . .
RUN make build-bin

# Stage 2: Runtime
FROM alpine:latest
RUN apk add --no-cache ca-certificates
WORKDIR /app
COPY --from=builder /app/bin/k8s-rightsizer .
RUN chmod +x k8s-rightsizer

ENTRYPOINT ["./k8s-rightsizer"]