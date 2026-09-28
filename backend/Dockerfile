FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/api ./cmd/api && CGO_ENABLED=0 go build -o /out/scanner ./cmd/scanner && CGO_ENABLED=0 go build -o /out/downloader ./cmd/downloader && CGO_ENABLED=0 go build -o /out/transcoder ./cmd/transcoder
FROM debian:bookworm-slim
# NVIDIA driver libraries injected by Container Toolkit require glibc.
RUN apt-get update && apt-get install -y --no-install-recommends ffmpeg fonts-dejavu-core ca-certificates python3 python3-venv && rm -rf /var/lib/apt/lists/* && useradd --system --user-group jfe && mkdir /cache && chown jfe:jfe /cache
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
