package audio

import (
	"bytes"
	"context"
	"errors"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

func ValidArtwork(data []byte) bool {
	if len(data) > 600000 {
		return false
	}
	cfg, format, e := image.DecodeConfig(bytes.NewReader(data))
	return e == nil && (format == "jpeg" || format == "png") && cfg.Width > 0 && cfg.Height > 0 && cfg.Width <= 8000 && cfg.Height <= 8000
}
func CoverArt(ctx context.Context, id string) ([]byte, error) {
	allowed := func(u *url.URL) bool {
		host := u.Hostname()
		return u.Scheme == "https" && u.User == nil && u.Port() == "" && (host == "coverartarchive.org" || host == "archive.org" || strings.HasSuffix(host, ".archive.org"))
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, e := net.SplitHostPort(address)
		if e != nil || port != "443" {
			return nil, errors.New("blocked destination")
		}
		ips, e := net.DefaultResolver.LookupIPAddr(ctx, host)
		if e != nil {
			return nil, e
		}
		for _, ip := range ips {
			if !ip.IP.IsGlobalUnicast() || ip.IP.IsPrivate() || ip.IP.IsLoopback() || ip.IP.IsLinkLocalUnicast() {
				return nil, errors.New("blocked destination")
			}
		}
		for _, ip := range ips {
			c, e := (&net.Dialer{Timeout: 10 * time.Second}).DialContext(ctx, network, net.JoinHostPort(ip.IP.String(), port))
			if e == nil {
				return c, nil
			}
		}
		return nil, errors.New("unavailable")
	}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 20 * time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) > 5 || !allowed(req.URL) {
			return errors.New("blocked redirect")
		}
		return nil
	}}
	req, e := http.NewRequestWithContext(ctx, "GET", "https://coverartarchive.org/release/"+id+"/front-250", nil)
	if e != nil {
		return nil, e
	}
	if !allowed(req.URL) {
		return nil, errors.New("invalid cover URL")
	}
	resp, e := client.Do(req)
	if e != nil {
		return nil, errors.New("artwork unavailable")
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, errors.New("artwork unavailable")
	}
	b, e := io.ReadAll(io.LimitReader(resp.Body, 600001))
	if e != nil || !ValidArtwork(b) {
		return nil, errors.New("unsupported artwork")
	}
	return b, nil
}
