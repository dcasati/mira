# syntax=docker/dockerfile:1.7

ARG GO_VERSION=1.25
ARG WHISPER_REF=592feef04a18
ARG WHISPER_MODEL=tiny.en

FROM golang:${GO_VERSION}-bookworm AS build
ARG WHISPER_REF
ARG WHISPER_MODEL
RUN apt-get update && apt-get install -y --no-install-recommends \
    build-essential ca-certificates cmake git libopus-dev libopusfile-dev pkg-config \
    && rm -rf /var/lib/apt/lists/*
WORKDIR /src
RUN git clone https://github.com/ggerganov/whisper.cpp /opt/whisper.cpp \
    && cd /opt/whisper.cpp && git checkout "${WHISPER_REF}" \
    && cmake -S /opt/whisper.cpp -B /opt/whisper.cpp/build -DWHISPER_BUILD_TESTS=OFF -DWHISPER_BUILD_EXAMPLES=OFF \
    && cmake --build /opt/whisper.cpp/build --target whisper -j"$(nproc)" \
    && mkdir -p /usr/local/lib /usr/local/include \
    && cp -a /opt/whisper.cpp/build/bin/lib*.so* /usr/local/lib/ \
    && cp /opt/whisper.cpp/include/whisper.h /usr/local/include/ \
    && find /opt/whisper.cpp/ggml -name '*.h' -exec cp {} /usr/local/include/ \; \
    && bash /opt/whisper.cpp/models/download-ggml-model.sh "${WHISPER_MODEL}" \
    && mkdir -p /out/models \
    && cp "/opt/whisper.cpp/models/ggml-${WHISPER_MODEL}.bin" /out/models/ggml-tiny.en.bin
COPY go.mod go.sum* ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=1 CGO_CFLAGS="-I/usr/local/include" CGO_LDFLAGS="-L/usr/local/lib" \
    go build -tags "opus whisper" -trimpath -ldflags="-s -w" -o /out/mira ./cmd/mira

FROM debian:bookworm-slim AS runtime
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates libgomp1 libopus0 libopusfile0 \
    && rm -rf /var/lib/apt/lists/* \
    && groupadd --system --gid 10001 mira && useradd --system --uid 10001 --gid 10001 --home-dir /nonexistent --shell /usr/sbin/nologin mira
COPY --from=build /usr/local/lib/libwhisper* /usr/local/lib/
COPY --from=build /usr/local/lib/libggml* /usr/local/lib/
COPY --from=build /out/mira /usr/local/bin/mira
COPY --from=build /out/models /models
ENV LD_LIBRARY_PATH=/usr/local/lib
USER 10001:10001
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/mira"]
