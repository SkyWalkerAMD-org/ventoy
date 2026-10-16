package center

import (
	"net/http"
	"strings"
	"testing"
)

func TestManualPolicyDownload(t *testing.T) {
	s := fixture(t)
	mustCode(t, request(s, "GET", "/api/linux-update-policy", "", nil), http.StatusUnauthorized)
	w := request(s, "GET", "/api/linux-update-policy", s.admin, nil)
	mustCode(t, w, http.StatusOK)
	if w.Header().Get("Content-Disposition") != `attachment; filename="linux-update-policy.sh"` {
		t.Fatal("missing download filename")
	}
	if !strings.HasPrefix(w.Body.String(), "#!/bin/sh\n") || strings.Contains(w.Body.String(), s.admin) {
		t.Fatal("invalid standalone policy or leaked administrator token")
	}
	j := arm(t, s, createFixtureJob(t, s, "rocky", false))
	files, err := s.renderFiles(j)
	if err != nil {
		t.Fatal(err)
	}
	if files["linux-update-policy.sh"] != w.Body.String() {
		t.Fatal("manual and unattended policy differ")
	}
}
