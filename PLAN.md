# Backend JFE: bốn binary, typed routes và OpenAPI tự sinh

## 1. Scope bản đầu

Giữ các quyết định đã chốt:

- API riêng, không duy trì tương thích Jellyfin trong bản đầu.
- Phim/series: users, libraries, scan, metadata local + TMDB, tìm kiếm/infinite scroll, favorites, watched, resume và next episode.
- Direct play, remux, HLS NVIDIA NVENC, audio/subtitle selection và lưu tiến độ xem. Chỉ hai chế độ video: NVIDIA hoặc tắt chuyển mã (mặc định); không fallback encode video bằng CPU.
- User độc lập, vai trò admin/user và quyền truy cập thư viện.
- Bốn process chạy cùng máy, chung PostgreSQL và filesystem volumes.
- Chưa gồm household profiles, GPU khác NVIDIA, plugin runtime, Redis, S3 hoặc cluster.

Frontend giữ thiết kế hiện tại; những chức năng ngoài scope được ẩn theo capabilities backend.

## 2. Monorepo và bốn binary

Chuyển toàn bộ source, assets, tests, package/lockfile và cấu hình frontend vào `frontend/`. Backend và frontend có Makefile riêng trong từng thư mục; root giữ hướng dẫn toàn project và `AGENTS.md`.

Backend dùng **Go, Fiber v3, pgx/v5, sqlc, PostgreSQL và rs/zerolog**, có đúng bốn binary:

| Binary | Trách nhiệm |
|---|---|
| `api` | HTTP API, auth/authorization, catalog, cấu hình, tạo job/playback session, phục vụ ảnh/phụ đề/direct stream/HLS |
| `scanner` | Scan filesystem, ffprobe, nhận diện phim/episode, metadata local/TMDB, artwork và lịch scan |
| `downloader` | Xem trước/tải YouTube, quản lý nguồn và lịch đồng bộ audio |
| `transcoder` | FFmpeg remux/transcode, chuẩn bị phụ đề phục vụ playback, quản lý tiến trình và dọn tài nguyên phiên phát |

- Dùng tên `scanner` trong thư mục, binary và Compose.
- Migration dùng trực tiếp golang-migrate CLI (image chính thức trong Compose); không nhúng migration runner vào API. Export OpenAPI qua `api openapi`. Project chỉ build bốn binary ứng dụng.
- Ba worker claim loại job tương ứng từ PostgreSQL bằng transaction ngắn, `SKIP LOCKED`, lease và heartbeat.
- Scan/metadata có retry giới hạn và chạy lại an toàn. Playback gắn với phiên, hỗ trợ cancellation; worker chết phải khiến phiên báo lỗi có thể phục hồi từ tiến độ đã lưu.
- Video mount chỉ đọc; scanner được ghi thư viện audio để sửa tag. Kho import riêng cho scanner/downloader ghi, API/transcoder chỉ đọc; artwork và playback cache có volume ghi riêng. API phục vụ file, không chạy FFmpeg trong request handler.
- Mặc định mỗi loại worker có một instance; giới hạn concurrency cấu hình riêng.

Tái sử dụng có chọn lọc logic/tests của Silo về naming, probing, scan và playback. Viết mới transport, schema, sqlc repositories và bootstrap; không copy nguyên kiến trúc Silo. Giữ attribution/AGPL cho backend phái sinh, không lấy branding Silo.

## 3. Route wrapper là nguồn sinh OpenAPI

Tạo package đăng ký route tập trung, với hình thức:

```go
Register[RequestDTO, QueryDTO, ParamsDTO, ResponseDTO](
    router,
    operation,
    handler,
)
```

`operation` khai báo method, Fiber path, operation ID, summary, tags, yêu cầu auth/quyền và các response status/content type.

Mỗi lần đăng ký đồng thời:

1. Đăng ký handler và middleware vào Fiber v3.
2. Bind/validate request body, query và path params theo DTO.
3. Thêm operation cùng schemas vào tài liệu OpenAPI 3.1.
4. Kiểm tra operation ID, route và path params không trùng hoặc lệch khai báo.

Quy ước bắt buộc:

- Mỗi endpoint có DTO rõ ràng cho **request body, query, path params và response**. Phần không sử dụng dùng kiểu `Empty`; GET không bị sinh request body giả.
- Không dùng `map[string]any`, database row hoặc sqlc-generated model làm public DTO.
- Wrapper chuyển Fiber path như `:id` thành OpenAPI `{id}`; không khai báo hai đường dẫn độc lập.
- Constraints trên DTO là nguồn chung cho runtime validation và schema: required, enum, độ dài và khoảng giá trị.
- Handler nhận input đã bind và request context; trả response DTO hoặc lỗi chuẩn hóa. Services không phụ thuộc Fiber.
- Các route streaming dùng response descriptor riêng cho binary body, content type, headers và các status như `200`, `206`, `416`; không bọc stream thành JSON.
- Auth metadata được dùng cả để gắn middleware thực tế và sinh OpenAPI security requirements.
- Toàn bộ product API đi qua wrapper. Static assets và trang hiển thị docs là các ngoại lệ hạ tầng.

Các hàm Get/Post/Put/Patch/Delete wrap trực tiếp Fiber; không dùng Huma. Sinh schema bằng invopop/jsonschema và validate bằng santhosh-tekuri/jsonschema từ cùng DTO tags. Export OpenAPI phải chạy được không cần database, worker hoặc FFmpeg. Config/env dùng Viper.

Cung cấp `/openapi.json` và `/docs`. CI kiểm tra spec hợp lệ, route coverage và artifacts được sinh lại không lệch source.

## 4. API và frontend phải đi cùng nhau

**Một API chưa hoàn thành nếu frontend tương ứng chưa được cập nhật.** Áp dụng cho cả endpoint mới và thay đổi contract.

- Sinh TypeScript types từ OpenAPI, dùng typed API client chung cho frontend.
- Mỗi lát cắt tính năng gồm: migration/query → service → typed route/docs → frontend hook/screen → tests.
- Thay đổi DTO phải cập nhật generated types, API calls, trạng thái loading/error/empty và tests frontend trong cùng thay đổi.
- Với API hạ tầng không có màn hình riêng, tích hợp vào luồng sử dụng tương ứng; không tạo trang giả chỉ để đánh dấu hoàn thành.
- Bỏ dần Jellyfin SDK/contracts khi chuyển từng luồng. Kết thúc bản đầu, mọi tính năng đang hiển thị phải chạy với backend mới.
- Backend cung cấp capabilities thực tế; frontend không hiển thị thao tác chưa được hỗ trợ.

Hợp đồng chính:

- REST `/api/v1`; nhóm system/setup, auth/users, libraries/jobs, catalog/metadata, user-state, playback và admin.
- Catalog trả `items`, `nextCursor`, `hasMore`; dùng thứ tự ổn định và cursor gắn với bộ lọc.
- Opaque bearer sessions, lưu hash phía server; stream token ngắn hạn gắn với playback session.
- Playback start trả phương thức, trạng thái chuẩn bị, URL và tracks; progress có sequence, stop idempotent.
- PostgreSQL schema mới tách catalog item/media file; không import database Silo/Jellyfin.
- TMDB tùy chọn; scan local vẫn hoạt động khi chưa cấu hình token.

## 5. Thứ tự triển khai và nghiệm thu

1. **Cấu trúc repo và contract foundation:** chuyển frontend, tạo bốn binary, pgx/sqlc/migrations, route wrapper/OpenAPI, typed client và Compose.
2. **Setup/auth/users:** triển khai đầy đủ backend và frontend; auth, session revocation và library authorization.
3. **Libraries/scanner/catalog:** scan jobs, ffprobe, metadata/artwork, tìm kiếm và infinite scroll; frontend quản lý thư viện và theo dõi jobs.
4. **Playback/transcoder:** direct play trước, sau đó remux/HLS NVIDIA NVENC, audio/phụ đề, resume, cancellation và cleanup; nối player hiện tại.
5. **Admin và ổn định:** cấu hình NVIDIA NVENC hoặc tắt chuyển mã, worker status, lịch scan, metadata editor cơ bản, capabilities và tài liệu vận hành.

Kiểm thử bắt buộc:

- Route wrapper: DTO binding/validation, optional/required fields, params, lỗi, status, auth và OpenAPI đúng với hành vi runtime.
- PostgreSQL integration: migrations, sqlc, phân quyền, worker claim/lease/retry và progress ordering.
- Media: HTTP Range/seek, FFmpeg thật, phụ đề, token hết hạn, worker restart và cleanup.
- Frontend: typecheck, lint, unit tests và E2E desktop/mobile; giữ các sửa lỗi fullscreen, infinite scroll và modal.
- E2E backend thật: setup → login → tạo thư viện → scan fixture → duyệt/tìm kiếm → phát → tiếp tục xem.
- CI sinh lại OpenAPI và frontend types, thất bại nếu artifacts chưa được cập nhật.

Cập nhật `AGENTS.md` ngay từ mốc đầu với cấu trúc mới, trách nhiệm bốn binary, quy tắc DTO/route wrapper, sqlc, generated artifacts và yêu cầu **API + frontend trong cùng thay đổi**.


## 6. Phạm vi audio đã chốt

- Thư viện music, podcasts, audiobooks; nguồn local và YouTube, chưa RSS.
- Nghệ sĩ/album/bài, chương trình/tập, sách/phần/chapter; nhạc có queue,
  shuffle/repeat; audio dài có resume, tốc độ nghe và hẹn giờ ngủ.
- Mini-player xuyên trang, audio/video không phát đồng thời. Chưa playlist cá nhân
  hoặc tải offline trên thiết bị.
- MusicBrainz gợi ý bản phát hành để người dùng chọn; Cover Art Archive tùy chọn.
  Không phụ thuộc dịch vụ ngoài để quản lý nhạc AI hoặc sửa metadata thủ công.
- UI sửa tag từng file hoặc nhiều file, ghi trực tiếp xuống file qua job scanner.
  Giữ tag không sửa, artwork/chapter không thay thế, nội dung audio và ID file/item.
  Ghi bản tạm, xác minh, thay nguyên tử; fingerprint và journal hỗ trợ xung đột/recovery.
- Tất cả thư viện audio cấp quyền ghi cho scanner; API/transcoder chỉ đọc.
  Admin và user có quyền import được sửa mọi file audio trong thư viện được cấp.
- Ghi tags MP3, FLAC, M4A/M4B, OGG/Opus, WAV, AIFF. AAC thô chỉ quét/phát,
  UI không cung cấp ghi tag cho định dạng này.
- Bốn binary api/scanner/transcoder/downloader. Downloader chạy yt-dlp, tải audio
  codec gốc; FFmpeg remux audio được phép, không encode video bằng CPU.
- Dán URL YouTube video/playlist/album Music không cần đăng nhập; xem trước,
  nhập một lần hoặc theo dõi mỗi sáu giờ. Không tìm kiếm YouTube, cookie hay channel.
- Mỗi video là một mục; giữ chapter có sẵn, không tự tách full album.
- Đồng bộ chỉ bổ sung, không xóa/ghi đè nội dung hoặc tag đã chỉnh. Ngừng theo dõi,
  xóa nguồn/thư viện không xóa file. Kho chung không quota từng user.
- Mặc định một job download đồng thời và ngưỡng 5 GiB trống, admin cấu hình được.
  Dùng staging, chống trùng video trong thư viện, lease/retry/backoff và kiểm tra
  quyền lúc thực thi. Downloader không dùng thư viện video làm đích nhập.
- Audio direct play hoặc HLS AAC do transcoder xử lý, hoạt động khi chế độ video
  disabled. Tái sử dụng token playback, Range và progress ordering hiện có.
- Mỗi thay đổi API đi cùng frontend EN/VI và tests. Kiểm tra file thực bằng
  Mutagen/FFmpeg, PostgreSQL disposable, worker recovery và E2E desktop/mobile.
