package audio

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"
)

var ErrYouTubeLogin = errors.New("youtube_login_required")
var ErrYouTubeUnavailable = errors.New("youtube_unavailable")
var ErrYouTubeRequest = errors.New("youtube_request_failed")
var ErrYouTubePartial = errors.New("youtube_partial")

var videoID = regexp.MustCompile(`^[A-Za-z0-9_-]{11}$`)
var playlistID = regexp.MustCompile(`^[A-Za-z0-9_-]{10,200}$`)

func YouTubeURL(raw string) (string, error) {
	u, e := url.Parse(raw)
	if e != nil || u.Scheme != "https" || u.User != nil || u.Port() != "" {
		return "", errors.New("invalid YouTube URL")
	}
	host := strings.ToLower(u.Hostname())
	var id string
	switch host {
	case "youtu.be":
		id = strings.Trim(u.Path, "/")
	case "youtube.com", "www.youtube.com", "music.youtube.com", "m.youtube.com":
		if list := u.Query().Get("list"); list != "" {
			if !playlistID.MatchString(list) {
				return "", errors.New("invalid playlist ID")
			}
			return "https://www.youtube.com/playlist?list=" + list, nil
		}
		id = u.Query().Get("v")
		if id == "" && (strings.HasPrefix(u.Path, "/shorts/") || strings.HasPrefix(u.Path, "/live/")) {
			parts := strings.Split(strings.Trim(u.Path, "/"), "/")
			if len(parts) == 2 {
				id = parts[1]
			}
		}
	default:
		return "", errors.New("only YouTube URLs are supported")
	}
	if !videoID.MatchString(id) {
		return "", errors.New("invalid video ID")
	}
	return "https://www.youtube.com/watch?v=" + id, nil
}

type YouTubeEntry struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	URL      string `json:"webpage_url"`
	Uploader string `json:"uploader"`
}
type YouTubeList struct {
	ID      string         `json:"id"`
	Title   string         `json:"title"`
	Entries []YouTubeEntry `json:"entries"`
}

// All downloader traffic crosses a local proxy with a public-address-only dialer.
// CONNECT is restricted to YouTube's first-party hosts; redirects cannot escape it.
func allowedYouTubeHost(host string) bool {
	for _, suffix := range []string{"youtube.com", "googlevideo.com", "ytimg.com", "ggpht.com", "youtubei.googleapis.com"} {
		if host == suffix || strings.HasSuffix(host, "."+suffix) {
			return true
		}
	}
	return false
}
func publicDial(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, e := net.SplitHostPort(address)
	if e != nil || port != "443" || !allowedYouTubeHost(strings.ToLower(host)) {
		return nil, errors.New("destination blocked")
	}
	ips, e := net.DefaultResolver.LookupIPAddr(ctx, host)
	if e != nil {
		return nil, e
	}
	for _, ip := range ips {
		if !ip.IP.IsGlobalUnicast() || ip.IP.IsPrivate() || ip.IP.IsLoopback() || ip.IP.IsLinkLocalUnicast() {
			return nil, errors.New("private destination blocked")
		}
	}
	for _, ip := range ips {
		c, e := (&net.Dialer{Timeout: 15 * time.Second}).DialContext(ctx, network, net.JoinHostPort(ip.IP.String(), port))
		if e == nil {
			return c, nil
		}
	}
	return nil, errors.New("destination unavailable")
}
func youtubeProxy(ctx context.Context) (string, func(), error) {
	listener, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		return "", nil, e
	}
	var mu sync.Mutex
	connections := map[net.Conn]bool{}
	server := &http.Server{ReadHeaderTimeout: 10 * time.Second}
	server.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "CONNECT" {
			http.Error(w, "HTTPS required", 403)
			return
		}
		remote, e := publicDial(ctx, "tcp", r.Host)
		if e != nil {
			http.Error(w, "Destination unavailable", 403)
			return
		}
		client, _, e := w.(http.Hijacker).Hijack()
		if e != nil {
			remote.Close()
			return
		}
		mu.Lock()
		connections[client] = true
		connections[remote] = true
		mu.Unlock()
		defer func() {
			client.Close()
			remote.Close()
			mu.Lock()
			delete(connections, client)
			delete(connections, remote)
			mu.Unlock()
		}()
		_, _ = io.WriteString(client, "HTTP/1.1 200 Connection Established\r\n\r\n")
		done := make(chan struct{})
		go func() { _, _ = io.Copy(remote, client); remote.Close(); close(done) }()
		_, _ = io.Copy(client, remote)
		client.Close()
		<-done
	})
	go func() { _ = server.Serve(listener) }()
	closeProxy := func() {
		_ = server.Close()
		mu.Lock()
		defer mu.Unlock()
		for c := range connections {
			_ = c.Close()
		}
	}
	return "http://" + listener.Addr().String(), closeProxy, nil
}
func YouTubeCommand(ctx context.Context, args ...string) ([]byte, error) {
	proxy, closeProxy, e := youtubeProxy(ctx)
	if e != nil {
		return nil, e
	}
	defer closeProxy()
	base := []string{"--ignore-config", "--js-runtimes", "node", "--no-cache-dir", "--no-progress", "--no-warnings", "--socket-timeout", "20", "--retries", "2", "--fragment-retries", "2", "--proxy", proxy}
	cmd := exec.CommandContext(ctx, "yt-dlp", append(base, args...)...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = 5 * time.Second
	var out limitedOutput
	cmd.Stdout = &out
	var diagnostic limitedOutput
	cmd.Stderr = &diagnostic
	if e = cmd.Run(); e != nil {
		return nil, youtubeCommandError(ctx, e, diagnostic.data)
	}
	return out.data, nil
}

type limitedOutput struct{ data []byte }

func (b *limitedOutput) Write(p []byte) (int, error) {
	if len(b.data)+len(p) > 16<<20 {
		return 0, errors.New("provider response too large")
	}
	b.data = append(b.data, p...)
	return len(p), nil
}
func PreviewYouTube(ctx context.Context, raw string) (YouTubeList, error) {
	u, e := YouTubeURL(raw)
	if e != nil {
		return YouTubeList{}, e
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	b, e := YouTubeCommand(ctx, "--flat-playlist", "--dump-single-json", "--playlist-end", "10000", "--", u)
	if e != nil {
		return YouTubeList{}, e
	}
	var v YouTubeList
	if e = json.Unmarshal(b, &v); e != nil {
		return v, e
	}
	if len(v.Entries) == 0 && videoID.MatchString(v.ID) {
		v.Entries = []YouTubeEntry{{ID: v.ID, Title: v.Title}}
	}
	return v, nil
}
