# Original subtitle management

The movie/episode detail action opens a wide Mantine ScrollArea modal with
preview/seek/timing on the left and cues on the right; narrow screens stack them.
Users select the video file and subtitle source; scanner prepares plain-text
preview cues and a source
fingerprint. The video uses an independent playback session with `preview=true`.
Direct play/remux is preferred; video conversion only uses enabled NVIDIA NVENC,
with a 720p/2 Mbit ceiling further bounded by the server's 720p policy. Seeking
preserves the chosen play/pause state, including when an unbuffered seek replaces
the session. Timing changes are immediate browser overlays and do not restart FFmpeg. Preview progress
renews the session without writing user state. Closing/changing source cleans up
playback; opening the editor stops the persistent player.

The slider shifts all cues, with an additional seconds field for ±600 seconds.
Positive means later. Original and shifted timings are shown in a virtualized cue
list; clicking a cue seeks one second before its shifted start. Sync does not edit
individual cue text. UTF-8 SRT/VTT timestamps are replaced without changing text,
markup, cue settings, metadata or line endings. ASS/SSA Start/End fields in the
Events section are changed, preserving styles and dialogue text; offsets must
respect their centisecond precision. Negative event starts are rejected rather
than truncated. Prepared text is bounded to 512 KiB; malformed/oversize sources
remain deletable, with timing sync unavailable. PGS/DVD may be deleted from MKV,
but have no OCR/text list and cannot be synced in this version.

## Access and deployment

Set `JFE_SUBTITLE_EDITING=true` on API and scanner; the default is false. Scanner
needs writable source files and parent directories, while API/transcoder retain
read-only video mounts. Live scanner heartbeats advertise FFmpeg/ffprobe and
MKVToolNix availability. The actual source/directory writability check runs in
scanner preparation, because API's read-only mount cannot establish scanner
permissions. Symlinks must resolve within the configured media root; hardlinked
original files are rejected. Resolved sidecars must still match the video basename
and have a supported subtitle extension; aliases to unrelated/video files cannot
be used to delete those targets. Admins and library members with import/edit permission
may change originals. The worker rechecks user status and permission before
publication. Audio APIs retain their separate audio-library permission restriction.

Typed `/files/:id/subtitle-sync` routes list capabilities, enqueue preparation,
read owner-scoped jobs, and enqueue save/delete against the preparation ID and
fingerprint. A SHA-256 revision of the probed track list pins preparation and
mutation to the same listing; rescan changes reject stale requests before any
original is edited. Requests cannot supply filesystem paths. The preparation identifies
one existing source; the fingerprint covers its path, inode, size and mtime.
Personal uploaded subtitles use the separate owner-scoped DELETE endpoint and do
not require original-source edit permission.

## Publication and recovery

Scanner shares the existing filesystem lock mechanism with scans and original-file
edits. Playback start and mutation enqueue serialize on a short PostgreSQL advisory
transaction lock. Pending original mutations block new playback; active sessions
and running transcoder jobs block mutations. Expired preview sessions are reaped by
existing playback expiry. Original-file operations have bounded worker retries. Library removal is blocked
until original subtitle publications finish or recover. Actor identity survives
account deletion so an already published edit can still be reconciled.

Sidecar updates use a same-directory temporary file. MKV uses `mkvmerge --sync` or
subtitle-track exclusion, with no video/audio encode. Mapping uses the prepared
source identity and the fresh subtitle ordinal; existing track UIDs, languages and
flags are checked. Chapters are extracted and explicitly reimported to preserve
existing chapter identities. Missing default IETF language/edition fields added by
MKVToolNix are allowed. Chapters and nonremoved tags are verified through extracted
XML, attachments are retained, and stream packet payload/extradata hashes are
compared. Nonselected packet timestamps must remain within 2 ms (AAC remux
rounding); selected text cues must reflect the requested shift. Full-file validation
makes MKV save/delete slower than interactive preview; sufficient free space for
a full remux is required.

A fsynced journal records the expected postpublication fingerprint. The source is
checked again immediately before atomic replacement. Sidecar deletion atomically
renames the file to a job-specific hidden tombstone, then refreshes catalog/cache
before deleting the tombstone. Scanner ignores its hidden temporary files during
scans. No backup survives successful completion.

Retries detect an already published output and only finish indexing, avoiding a
second offset. Scanner also recovers terminal failed/cancelled jobs: proven
publications finish catalog reconciliation even if editing was disabled or the
user's permission was revoked; unpublished output is abandoned. If an outside edit
makes publication ambiguous, recovery never overwrites that new source and retains
any deletion tombstone for manual inspection. Errors and phases are visible through
the owner-scoped job DTO; logs carry internal verification reasons.

After successful save/delete, frontend invalidates detail/subtitle queries, clears
the old track selection and resets offset to zero. A later prepare loads the updated
source. Deleting a source removes future preview/burn-in use; this does not erase
text encoded into the original video pixels. Embedded containers other than MKV
have no write/delete capability in this release.


## Personal player timing

The player timing slider is separate from original editing. All viewers with
library access may save a personal offset in PostgreSQL without scanner jobs or
filesystem writes. Values are scoped to account/file/source and restored on the
next playback, including on another device. Overlay and burn-in selection of the
same source share a value. Reset deletes the override and uses source timestamps.
The slider previews immediately and persists on release, with ±10/±60/±600-second
ranges and keyboard steps of 0.1 seconds. Original subtitle publication clears
original-source overrides with its catalog update, preventing a previously saved
personal correction from being applied again on top of the new file timing.
Track-list revision changes also invalidate old source offsets. Personal uploads
are kept separate. Deploy migration 00010 with API/frontend; this feature does
not require writable media or the original-editing flag.
