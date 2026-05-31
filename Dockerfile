FROM alpine:3.23@sha256:5b10f432ef3da1b8d4c7eb6c487f2f5a8f096bc91145e68878dd4a5019afde11

RUN apk add --no-cache ca-certificates
WORKDIR /app

COPY k8s-rightsizer* ./
COPY bin/k8s-rightsizer* ./

RUN if [ -f ./bin/k8s-rightsizer ]; then mv ./bin/k8s-rightsizer .; fi && \
    chmod +x k8s-rightsizer

ENTRYPOINT ["./k8s-rightsizer"]