package center

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
)

const centosInjectionName = "centos7-iventoy.tar.gz"
const rockyInjectionName = "rocky-iventoy.tar.gz"

func kickstartInjectionName(profile string) string {
	switch profile {
	case "centos-7.9":
		return centosInjectionName
	case "rocky-8":
		return rockyInjectionName
	default:
		return ""
	}
}

// iVentoy 1.0.44's CentOS hook appends inst.ks to /etc/vtoycmdline,
// which the original CentOS 7 dracut does not read. Rocky 8.10 also reaches
// Anaconda without the selected Kickstart. Its supported injection
// mechanism lets us supply the argument through dracut's own cmdline.d.
// The archive is bound only to the task image, together with its Kickstart;
// identity, existing RAID/LVM and one-use authorization checks remain in %pre.
func kickstartInjection() (string, error) {
	const content = "inst.ks=file:/ventoy/autoinstall\n"
	var out bytes.Buffer
	gz := gzip.NewWriter(&out)
	tw := tar.NewWriter(gz)
	h := &tar.Header{
		Name:     "etc/cmdline.d/99-ventoy-center-ks.conf",
		Typeflag: tar.TypeReg,
		Mode:     0644,
		Size:     int64(len(content)),
		Format:   tar.FormatUSTAR,
	}
	if err := tw.WriteHeader(h); err != nil {
		return "", err
	}
	if _, err := tw.Write([]byte(content)); err != nil {
		return "", err
	}
	if err := tw.Close(); err != nil {
		return "", err
	}
	if err := gz.Close(); err != nil {
		return "", err
	}
	return out.String(), nil
}
