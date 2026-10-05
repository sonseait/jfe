FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/api ./cmd/api && CGO_ENABLED=0 go build -o /out/scanner ./cmd/scanner && CGO_ENABLED=0 go build -o /out/downloader ./cmd/downloader && CGO_ENABLED=0 go build -o /out/transcoder ./cmd/transcoder
FROM debian:bookworm-slim AS media-runtime
ARG TARGETARCH
# NVIDIA driver libraries injected by Container Toolkit require glibc.
# Upstream Debian FFmpeg lacks tonemap_cuda. Install this pinned FFmpeg build
# only; JFE does not depend on the Jellyfin server or its API.
RUN apt-get update && apt-get install -y --no-install-recommends curl ca-certificates \
    && case "$TARGETARCH" in \
      amd64) ffmpeg_sha=4829b34df16843ecf1aca8b1a600874b5c767396e1d1990c889c761fd143d928 ;; \
      arm64) ffmpeg_sha=3497002f1b7a7664875dd0aa71d1916a74cbee6598d8b3b413b83732cd1d8794 ;; \
      *) echo "Unsupported image architecture: $TARGETARCH" >&2; exit 1 ;; \
    esac \
    && curl -fL --retry 3 "https://github.com/jellyfin/jellyfin-ffmpeg/releases/download/v8.1.3-1/jellyfin-ffmpeg8_8.1.3-1-bookworm_${TARGETARCH}.deb" -o /tmp/jfe-ffmpeg.deb \
    && echo "$ffmpeg_sha  /tmp/jfe-ffmpeg.deb" | sha256sum -c - \
    && apt-get install -y --no-install-recommends /tmp/jfe-ffmpeg.deb mkvtoolnix fonts-dejavu-core python3 python3-venv \
    && ln -s /usr/lib/jellyfin-ffmpeg/ffmpeg /usr/local/bin/ffmpeg \
    && ln -s /usr/lib/jellyfin-ffmpeg/ffprobe /usr/local/bin/ffprobe \
    && rm /tmp/jfe-ffmpeg.deb && rm -rf /var/lib/apt/lists/*
FROM media-runtime
RUN useradd --system --user-group jfe && mkdir /cache && chown jfe:jfe /cache
ENV NVIDIA_DRIVER_CAPABILITIES=compute,video,utility
ENV NVIDIA_VISIBLE_DEVICES=all
COPY --from=node:24-bookworm-slim /usr/local/bin/node /usr/local/bin/node
COPY requirements-audio.txt /tmp/requirements-audio.txt
RUN python3 -m venv /opt/audio && /opt/audio/bin/pip install --no-cache-dir -r /tmp/requirements-audio.txt
ENV PATH="/opt/audio/bin:${PATH}"
COPY --from=build /out/ /usr/local/bin/
USER jfe
EXPOSE 8090
CMD ["api"]
