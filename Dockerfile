# Stage 1: Build
FROM golang:1.26.3-alpine@sha256:6df14f4a4bc9d979a3721f488981e0d1b318006377e473ed23d026796f5f4c0a AS builder
RUN apk add --no-cache make git
WORKDIR /app
COPY . .
RUN make build-bin

# Stage 2: Runtime
FROM alpine:latest@sha256:5b10f432ef3da1b8d4c7eb6c487f2f5a8f096bc91145e68878dd4a5019afde11
RUN apk add --no-cache ca-certificates
WORKDIR /app
COPY --from=builder /app/bin/k8s-rightsizer .
RUN chmod +x k8s-rightsizer

ENTRYPOINT ["./k8s-rightsizer"]