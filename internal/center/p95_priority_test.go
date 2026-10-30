package center

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"testing"
)

// Pin and embedded asset must travel together; a stale pin would break every
// node's software transaction even if the Go server itself built successfully.
func TestP95PriorityEmbeddedIntegrity(t *testing.T) {
	script, err := manualAssets.ReadFile("assets/manual/p95-priority-setup.sh")
	if err != nil {
		t.Fatal(err)
	}
	client, err := manualAssets.ReadFile("assets/manual/p95_priority.py")
	if err != nil {
		t.Fatal(err)
	}
	want := fmt.Sprintf("SETUP_SHA256 = '%x'", sha256.Sum256(script))
	if !strings.Contains(string(client), want) {
		t.Fatal("bundled P95 installer and verification pin differ")
	}
	if !strings.HasPrefix(string(script), "#!/bin/bash\n") || !strings.Contains(string(script), "\n__P95_PAYLOAD_BELOW__\n") {
		t.Fatal("incomplete P95 offline installer")
	}
}
