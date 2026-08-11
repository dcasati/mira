# syntax=docker/dockerfile:1.7

ARG GO_VERSION=1.24
ARG WHISPER_REF=v1.7.6

FROM golang:${GO_VERSION}-bookworm AS build
ARG WHISPER_REF
RUN apt-get update && apt-get install -y --no-install-recommends \
    build-essential ca-certificates cmake git libopus-dev pkg-config \
    && rm -rf /var/lib/apt/lists/*
WORKDIR /src
RUN git clone --depth 1 --branch "${WHISPER_REF}" https://github.com/ggerganov/whisper.cpp /opt/whisper.cpp \
    && cmake -S /opt/whisper.cpp -B /opt/whisper.cpp/build -DWHISPER_BUILD_TESTS=OFF -DWHISPER_BUILD_EXAMPLES=OFF \
    && cmake --build /opt/whisper.cpp/build --target whisper -j"$(nproc)" \
    && cmake --install /opt/whisper.cpp/build --prefix /usr/local
COPY go.mod go.sum* ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=1 CGO_CFLAGS="-I/usr/local/include" CGO_LDFLAGS="-L/usr/local/lib -lwhisper" \
    go build -tags "opus whisper" -trimpath -ldflags="-s -w" -o /out/mira ./cmd/mira

FROM debian:bookworm-slim AS runtime
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates libgomp1 libopus0 \
    && rm -rf /var/lib/apt/lists/* \
    && groupadd --system mira && useradd --system --gid mira --home-dir /nonexistent --shell /usr/sbin/nologin mira
COPY --from=build /usr/local/lib/libwhisper* /usr/local/lib/
COPY --from=build /out/mira /usr/local/bin/mira
ENV LD_LIBRARY_PATH=/usr/local/lib
USER mira:mira
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/mira"]
