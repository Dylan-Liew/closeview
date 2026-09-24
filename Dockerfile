FROM alpine:3.22 AS codex
ARG CODEX_VERSION=0.156.1
ARG TARGETARCH
RUN apk add --no-cache ca-certificates && \
    case "${TARGETARCH:-amd64}" in \
      amd64) asset="codex-package-x86_64-unknown-linux-musl.tar.gz" ;; \
      arm64) asset="codex-package-aarch64-unknown-linux-musl.tar.gz" ;; \
      *) echo "unsupported Codex architecture: ${TARGETARCH:-amd64}" >&2; exit 1 ;; \
    esac && \
    base="https://github.com/openai/codex/releases/download/rust-v${CODEX_VERSION}" && \
    wget -q "${base}/${asset}" -O /tmp/codex.tar.gz && \
    wget -q "${base}/codex-package_SHA256SUMS" -O /tmp/SHA256SUMS && \
    expected="$(grep "  ${asset}$" /tmp/SHA256SUMS | cut -d ' ' -f 1)" && \
    test -n "$expected" && \
    printf '%s  %s\n' "$expected" /tmp/codex.tar.gz | sha256sum -c - && \
    mkdir -p /out && \
    tar -xzf /tmp/codex.tar.gz -C /out bin/codex && \
    mv /out/bin/codex /out/codex && \
    test -x /out/codex

FROM golang:1.24-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -o /out/closeview ./cmd/closeview

FROM alpine:3.22
COPY --from=build /out/closeview /usr/local/bin/closeview
COPY --from=codex /out/codex /usr/local/bin/codex
ENV HOME=/tmp
ENV CLOSEVIEW_CODEX_BIN=/usr/local/bin/codex
USER 1000:1000
ENTRYPOINT ["closeview"]
CMD ["serve", "--host", "127.0.0.1", "--port", "3434"]
