# Stage 1: Build
FROM golang:1.27.3-alpine@sha256:d200d2dd76b03a99b52651fd1325aff7dcd35422c35c01bac56571b0507f3deb AS builder
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