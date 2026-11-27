FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS builder
ARG TARGETOS
ARG TARGETARCH
WORKDIR /src
COPY *.go ./
RUN go mod init sshsync && \
    CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH:-amd64} \
    go build -trimpath -ldflags="-s -w" -o /out/sshsync . && \
    mkdir -p /data

FROM scratch
COPY --from=builder /out/sshsync /sshsync
COPY --from=builder --chown=65532:65532 /data /data
USER 65532:65532
EXPOSE 54843
ENV SSHSYNC_LISTEN=:54843 \
    SSHSYNC_CERT_FILE=/data/cert.pem \
    SSHSYNC_KEY_FILE=/data/key.pem
ENTRYPOINT ["/sshsync"]
