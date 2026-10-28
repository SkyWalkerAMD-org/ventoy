package center

import (
	"io"
	"net/http"
	"os"
	"runtime"
	"strings"
)

// Kept outside tools and setup bundles. Rotation takes effect on the next setup
// run without publishing the secret in a manifest, image or public repository.
func (s *Server) softwareFYHOCUnlock(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	name := s.config.FYHOCUnlockPasswordFile
	if name == "" {
		respond(w, 200, map[string]any{"enabled": false})
		return
	}
	info, err := os.Lstat(name)
	if err != nil || !info.Mode().IsRegular() || (runtime.GOOS != "windows" && info.Mode().Perm()&0077 != 0) {
		fail(w, 503, "FYHOC unlock credential is unavailable or not private")
		return
	}
	f, err := os.Open(name)
	if err != nil {
		fail(w, 503, "FYHOC unlock credential is unavailable")
		return
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !os.SameFile(info, opened) {
		fail(w, 503, "FYHOC unlock credential changed; retry")
		return
	}
	b, err := io.ReadAll(io.LimitReader(f, 129))
	key := strings.TrimSuffix(strings.TrimSuffix(string(b), "\n"), "\r")
	valid := err == nil && len(key) > 0 && len(key) < 128
	for _, c := range []byte(key) {
		valid = valid && c >= 33 && c <= 126
	}
	if !valid {
		fail(w, 503, "FYHOC unlock credential has an invalid format")
		return
	}
	respond(w, 200, map[string]any{"enabled": true, "password": key})
}
