package center

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

var installerOrigins = map[string]string{
	"ubuntu":          "https://archive.ubuntu.com/ubuntu/",
	"ubuntu-security": "https://security.ubuntu.com/ubuntu/",
	"ubuntu-archive":  "https://old-releases.ubuntu.com/ubuntu/",
	"debian":          "https://deb.debian.org/debian/",
	"debian-security": "https://deb.debian.org/debian-security/",
	"debian-archive":  "https://archive.debian.org/debian/",
}

const installerObjectLimit = 256 << 20
const installerDownloadTimeout = 10 * time.Minute

var cacheObjectRE = regexp.MustCompile(`^[a-f0-9]{64}\.data$`)

func (s *Server) cacheRenderSettings(d *renderData) {
	if s.config.InstallerCacheDir == "" || s.config.InstallerCacheURL == "" {
		return
	}
	d.InstallerCache = s.config.InstallerCacheURL + "/installer-cache"
	u, err := url.Parse(d.InstallerCache)
	if err != nil {
		return
	}
	d.CacheHost = u.Host
	d.CacheDirectory = u.Path + "/debian"
	if d.Target.Archived {
		d.CacheDirectory += "-archive"
	}
}

func installerUpstream(origin, name string) (string, error) {
	base, ok := installerOrigins[origin]
	if !ok || name == "" || path.Clean(name) != name || strings.ContainsAny(name, "\\%?#\x00\r\n") || strings.HasPrefix(name, "/") ||
		!(strings.HasPrefix(name, "dists/") || strings.HasPrefix(name, "pool/")) {
		return "", errors.New("unsupported repository path")
	}
	return base + name, nil
}

func installerHTTPClient() *http.Client {
	return &http.Client{Timeout: installerDownloadTimeout, CheckRedirect: func(r *http.Request, via []*http.Request) error {
		if len(via) >= 4 {
			return errors.New("too many repository redirects")
		}
		// Repository aliases may redirect between the official allowlisted hosts.
		if r.URL.Scheme != "https" || r.URL.User != nil || r.URL.Port() != "" {
			return errors.New("unsafe repository redirect")
		}
		for _, raw := range installerOrigins {
			u, _ := url.Parse(raw)
			if r.URL.Host == u.Host {
				return nil
			}
		}
		return errors.New("repository redirect outside allowlist")
	}}
}

func (s *Server) cacheLimit() int64 {
	if s.config.InstallerCacheMaxBytes > 0 {
		return s.config.InstallerCacheMaxBytes
	}
	return 8 << 30
}

type cacheFile struct {
	name     string
	size     int64
	modified time.Time
}

func cacheFiles(dir string) ([]cacheFile, int64) {
	if dir == "" {
		return nil, 0
	}
	entries, _ := os.ReadDir(dir)
	files := []cacheFile{}
	var total int64
	for _, e := range entries {
		if !e.Type().IsRegular() || !cacheObjectRE.MatchString(e.Name()) {
			continue
		}
		st, err := e.Info()
		if err != nil {
			continue
		}
		files = append(files, cacheFile{e.Name(), st.Size(), st.ModTime()})
		total += st.Size()
	}
	sort.Slice(files, func(i, j int) bool { return files[i].modified.Before(files[j].modified) })
	return files, total
}

func (s *Server) installerCacheStatus(w http.ResponseWriter, r *http.Request) {
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	_, total := cacheFiles(s.config.InstallerCacheDir)
	respond(w, 200, map[string]any{"enabled": s.config.InstallerCacheDir != "", "bytes": total, "max_bytes": s.cacheLimit(), "origins": installerOrigins, "package_signatures": "verified by the original package manager", "metadata_ttl_seconds": 600, "rpm_installation": "EL installers use their local DVD repositories", "concurrency": s.cacheConcurrency(), "active_downloads": len(s.cacheFlights), "reserved_bytes": s.cacheReserved, "hits": s.cacheHits, "downloads": s.cacheDownloads, "failures": s.cacheFailures, "statistics_since": "process start", "warm_evidence": s.cacheWarmEvidence()})
}

func (s *Server) cacheConcurrency() int {
	if s.config.InstallerCacheConcurrency > 0 {
		return s.config.InstallerCacheConcurrency
	}
	return 4
}

func (s *Server) installerCache(w http.ResponseWriter, r *http.Request) {
	if s.config.InstallerCacheDir == "" {
		fail(w, 503, "安装依赖缓存未启用")
		return
	}
	name := r.PathValue("path")
	upstream, err := installerUpstream(r.PathValue("origin"), name)
	if err != nil || r.URL.RawQuery != "" {
		fail(w, 400, "不支持的镜像源路径")
		return
	}
	key := fmt.Sprintf("%x.data", sha256.Sum256([]byte(upstream)))
	dir := s.config.InstallerCacheDir
	if st, e := os.Lstat(dir); e == nil && (!st.IsDir() || st.Mode()&os.ModeSymlink != 0) {
		fail(w, 500, "缓存路径无效")
		return
	}
	if err = os.MkdirAll(dir, 0700); err != nil {
		fail(w, 500, "无法建立缓存")
		return
	}
	immutable := strings.HasPrefix(name, "pool/") || strings.Contains(name, "/by-hash/")
	serve := func() bool {
		f, e := safeOpen(dir, key, ".data")
		if e != nil {
			return false
		}
		defer f.Close()
		st, e := f.Stat()
		if e != nil || (!immutable && time.Since(st.ModTime()) > 10*time.Minute) {
			return false
		}
		w.Header().Set("X-Ventoy-Cache", "HIT")
		s.cacheMu.Lock()
		s.cacheHits++
		s.cacheMu.Unlock()
		w.Header().Set("Content-Type", "application/octet-stream")
		http.ServeContent(w, r, path.Base(name), st.ModTime(), f)
		return true
	}
	if serve() {
		return
	}
	// One transfer per object; unrelated objects can progress concurrently.
	// Reserve bytes after response headers, before receiving the body. Known
	// small files need not evict an entire object-limit worth of offline hits.
	wait := time.NewTimer(5 * time.Minute)
	defer wait.Stop()
	poll := time.NewTicker(100 * time.Millisecond)
	defer poll.Stop()
	limit := int64(installerObjectLimit)
	if s.cacheLimit() < limit {
		limit = s.cacheLimit()
	}
	var completed chan struct{}
	var reserved int64
	for completed == nil {
		s.cacheMu.Lock()
		other := s.cacheFlights[key]
		if other == nil && len(s.cacheFlights) < s.cacheConcurrency() {
			if s.cacheFlights == nil {
				s.cacheFlights = make(map[string]chan struct{})
			}
			completed = make(chan struct{})
			s.cacheFlights[key] = completed
		}
		s.cacheMu.Unlock()
		if completed != nil {
			break
		}
		select {
		case <-r.Context().Done():
			return
		case <-wait.C:
			w.Header().Set("Retry-After", "2")
			fail(w, 503, "缓存正在下载其他依赖，请重试")
			return
		case <-other:
			if !serve() {
				fail(w, 502, "同一依赖的下载失败，请重试")
			}
			return
		case <-poll.C:
			if serve() {
				return
			}
		}
	}
	success := false
	defer func() {
		s.cacheMu.Lock()
		delete(s.cacheFlights, key)
		s.cacheReserved -= reserved
		if success {
			s.cacheDownloads++
		} else {
			s.cacheFailures++
		}
		close(completed)
		s.cacheMu.Unlock()
	}()
	if serve() {
		success = true
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), installerDownloadTimeout)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, upstream, nil)
	client := s.cacheClient
	if client == nil {
		client = installerHTTPClient()
	}
	res, err := client.Do(req)
	if err != nil {
		fail(w, 502, "软件源暂时不可达；已缓存的软件包仍可读取")
		return
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		code := http.StatusBadGateway
		if res.StatusCode == 404 {
			code = 404
		}
		fail(w, code, "软件源未提供该文件")
		return
	}
	if res.ContentLength > limit {
		fail(w, 413, "依赖文件超过缓存对象上限")
		return
	}
	allocation := limit
	if res.ContentLength >= 0 {
		allocation = res.ContentLength
	}
	s.cacheMu.Lock()
	if s.cacheReserved+allocation > s.cacheLimit() {
		s.cacheMu.Unlock()
		w.Header().Set("Retry-After", "2")
		fail(w, 503, "其他下载已预留缓存空间，请重试")
		return
	}
	files, total := cacheFiles(dir)
	for _, old := range files {
		if total+s.cacheReserved+allocation <= s.cacheLimit() {
			break
		}
		if s.cacheFlights[old.name] != nil && old.name != key {
			continue
		}
		if os.Remove(filepath.Join(dir, old.name)) == nil {
			total -= old.size
		}
	}
	if total+s.cacheReserved+allocation > s.cacheLimit() {
		s.cacheMu.Unlock()
		fail(w, 507, "缓存空间不足")
		return
	}
	reserved = allocation
	s.cacheReserved += reserved
	s.cacheMu.Unlock()
	tmp, err := os.CreateTemp(dir, ".download-")
	if err != nil {
		fail(w, 507, "缓存空间不足")
		return
	}
	defer os.Remove(tmp.Name())
	n, err := io.Copy(tmp, io.LimitReader(res.Body, allocation+1))
	syncErr := tmp.Sync()
	closeErr := tmp.Close()
	if err != nil || syncErr != nil || closeErr != nil || n > allocation || (res.ContentLength >= 0 && n != res.ContentLength) {
		fail(w, 502, "依赖下载不完整；未发布缓存文件")
		return
	}
	if _, err = replaceSnapshot(tmp.Name(), filepath.Join(dir, key)); err != nil {
		fail(w, 500, "缓存发布失败")
		return
	}
	success = true
	// Do not rewrite Release/Packages or bypass APT signatures/hash checks.
	if !serve() {
		fail(w, 500, "缓存读取失败")
	}
}
