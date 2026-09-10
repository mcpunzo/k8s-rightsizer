# Stage 1: Build
FROM golang:1.26.6-alpine@sha256:1a9c10cf505a9e6b1e96ea77ebdbfe79a0f10380181faf88bc3b51d7e4315fae AS builder
RUN apk add --no-cache make git
WORKDIR /app
COPY . .
RUN make build-bin

# Stage 2: Runtime
FROM alpine:3.24@sha256:a2d49ea686c2adfe3c992e47dc3b5e7fa6e6b5055609400dc2acaeb241c829f4
RUN apk upgrade --no-cache && apk add --no-cache ca-certificates
WORKDIR /app
COPY --from=builder /app/bin/k8s-rightsizer .
RUN chmod +x k8s-rightsizer

ENTRYPOINT ["./k8s-rightsizer"]