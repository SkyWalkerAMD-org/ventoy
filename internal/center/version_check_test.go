package center

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// Execute the actual version guard against isolated release files. A matching
// numeric major alone must never turn Linux 8 into Stream or accept Rocky as CentOS.
func TestCentOSDistributionIdentity(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("requires Linux /bin/bash")
	}
	for _, test := range []struct {
		profile, id, version, release string
		allowed                       bool
	}{
		{"almalinux-8", "almalinux", "8.10", "", true},
		{"almalinux-9", "almalinux", "9.5", "", true},
		{"almalinux-9", "rocky", "9.5", "", false},
		{"rhel-7", "rhel", "7.9", "", true},
		{"rhel-8", "rhel", "8.10", "", true},
		{"rhel-9", "rhel", "9.4", "", true},
		{"rhel-8", "centos", "8", "", false},
		{"rhel-9", "rhel", "8.10", "", false},
		{"centos-7.9", "centos", "7", "CentOS Linux release 7.9.2009 (Core)", true},
		{"centos-8.5", "centos", "8", "CentOS Linux release 8.5.2111", true},
		{"centos-stream-9", "centos", "9", "CentOS Stream release 9", true},
		{"centos-stream-10", "centos", "10", "CentOS Stream release 10 (Coughlan)", true},
		{"centos-8.5", "centos", "8", "CentOS Linux release 8.4.2105", false},
		{"centos-8.5", "centos", "8", "CentOS Stream release 8", false},
		{"centos-8.5", "centos", "8", "CentOS Linux release 8.5.21110", false},
		{"centos-stream-9", "centos", "9", "CentOS Linux release 9", false},
		{"centos-stream-9", "rocky", "9", "CentOS Stream release 9", false},
		{"centos-stream-9", "centos", "10", "CentOS Stream release 10", false},
		{"centos-stream-10", "centos", "10", "CentOS Stream release 100", false},
		{"centos-stream-10", "centos", "9", "CentOS Stream release 10", false},
		{"centos-stream-10", "centos", "10", "", false},
	} {
		t.Run(test.profile+"/"+test.id+"/"+test.release, func(t *testing.T) {
			root := t.TempDir()
			for name, text := range map[string]string{
				"os-release":     "ID=" + test.id + "\nVERSION_ID=" + test.version + "\n",
				"centos-release": test.release + "\n",
			} {
				if err := os.WriteFile(filepath.Join(root, name), []byte(text), 0600); err != nil {
					t.Fatal(err)
				}
			}
			profile, ok := findProfile(test.profile)
			if !ok {
				t.Fatal("missing profile")
			}
			script, err := render("version-check.sh", renderData{Target: profile})
			if err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command("/bin/bash", "-e")
			cmd.Stdin = strings.NewReader(strings.ReplaceAll(script, "/etc/", root+"/"))
			output, err := cmd.CombinedOutput()
			if (err == nil) != test.allowed {
				t.Fatalf("allowed=%v, error=%v, output=%s", test.allowed, err, output)
			}
		})
	}
}
