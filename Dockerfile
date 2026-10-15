FROM --platform=$BUILDPLATFORM caddy:2.11.4-builder-alpine AS builder
ARG TARGETOS TARGETARCH
COPY decompress /src/decompress
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    xcaddy build --with github.com/cowprotocol/caddy/decompress=/src/decompress

FROM caddy:2.11.4-alpine
LABEL org.opencontainers.image.source=https://github.com/cowprotocol/caddy
COPY --from=builder /usr/bin/caddy /usr/bin/caddy
