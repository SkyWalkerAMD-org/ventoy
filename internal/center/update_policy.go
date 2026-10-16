package center

import "net/http"

// This download contains no machine identity or credential and never runs on
// the controller. Manual installations apply it inside the installed system.
func (s *Server) linuxUpdatePolicy(w http.ResponseWriter, r *http.Request) {
	script, err := render("linux-update-policy.sh", renderData{KernelPackages: kernelPackagePatterns()})
	if err != nil {
		http.Error(w, "Cannot render update policy", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/x-shellscript; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="linux-update-policy.sh"`)
	w.Header().Set("Cache-Control", "no-store")
	w.Write([]byte(script))
}
