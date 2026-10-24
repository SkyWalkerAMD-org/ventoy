package center

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
)

// iVentoy supplies NoCloud data but does not pass the autoinstall flag to
// Subiquity. Use its live injection entry point and Subiquity's command-line
// option, leaving the original ISO, kernel command line and snap untouched.
// The hook activates only for this exact task's user-data and Ubuntu release.
func ubuntuInjection(d renderData) (string, error) {
	var out bytes.Buffer
	gz := gzip.NewWriter(&out)
	tw := tar.NewWriter(gz)
	for _, f := range []struct{ template, path string }{
		{"ubuntu-injection-hook.sh", "live_injection_7ed136ec_7a61_4b54_adc3_ae494d5106ea/hook.sh"},
		{"ubuntu-live.sh", "ventoy-center/ubuntu-live.sh"},
		{"ubuntu-subiquity.sh", "ventoy-center/ubuntu-subiquity.sh"},
		{"ubuntu-ubiquity.sh", "ventoy-center/ubuntu-ubiquity.sh"},
	} {
		content, err := render(f.template, d)
		if err != nil {
			return "", err
		}
		if err := tw.WriteHeader(&tar.Header{Name: f.path, Typeflag: tar.TypeReg, Mode: 0755, Size: int64(len(content)), Format: tar.FormatUSTAR}); err != nil {
			return "", err
		}
		if _, err := tw.Write([]byte(content)); err != nil {
			return "", err
		}
	}
	if err := tw.Close(); err != nil {
		return "", err
	}
	if err := gz.Close(); err != nil {
		return "", err
	}
	return out.String(), nil
}
