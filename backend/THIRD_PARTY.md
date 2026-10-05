# FFmpeg runtime

The image packages the unmodified Jellyfin FFmpeg build **8.1.3-1** for Debian
bookworm to supply NVIDIA CUDA tone mapping. This is an FFmpeg distribution;
JFE uses its own API and does not install or depend on the Jellyfin server.

- Binary release and checksums: https://github.com/jellyfin/jellyfin-ffmpeg/releases/tag/v8.1.3-1
- Corresponding packaging, patches and build recipes: https://github.com/jellyfin/jellyfin-ffmpeg/tree/v8.1.3-1
- FFmpeg source: https://ffmpeg.org and the upstream source version specified by that build recipe.
- Copyrights and licenses shipped by the package remain in `/usr/share/doc/jellyfin-ffmpeg8/`.
  The GPL build includes GPL-covered FFmpeg components and CUDA tone-map patches;
  use the upstream source/recipes when redistributing it. No upstream CUDA source
  is copied into JFE application code.

The Dockerfile pins both amd64 and arm64 package SHA-256 values. JFE only selects
the NVIDIA CUDA/NVENC path; other encoders/backends present in FFmpeg are not
application features. NVIDIA driver libraries come from the host through NVIDIA
Container Toolkit and must be compatible with the packaged FFmpeg.
