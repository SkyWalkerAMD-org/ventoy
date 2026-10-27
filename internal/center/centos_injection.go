package center

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
)

const centosInjectionName = "centos7-iventoy.tar.gz"
const rockyInjectionName = "rocky-iventoy.tar.gz"
const ubuntuInjectionName = "ubuntu-iventoy.tar.gz"
const debianInjectionName = "debian-iventoy.tar.gz"

func bootInjectionName(profile string) string {
	switch profile {
	case "debian-10", "debian-11", "debian-12", "debian-13":
		return debianInjectionName
	case "centos-7.6", "centos-7.9":
		return centosInjectionName
	case "centos-8.0", "centos-8.4", "centos-8.5", "centos-stream-8", "centos-stream-9", "centos-stream-10", "rocky-8", "rocky-9", "rocky-10", "almalinux-8", "almalinux-9", "rhel-7", "rhel-8", "rhel-9":
		return rockyInjectionName
	case "ubuntu-20.04", "ubuntu-22.04", "ubuntu-24.04", "ubuntu-24.10", "ubuntu-25.04", "ubuntu-26.04", "ubuntu-22.04-desktop", "ubuntu-26.04-desktop":
		return ubuntuInjectionName
	default:
		return ""
	}
}

// iVentoy 1.0.44's CentOS hook appends inst.ks to /etc/vtoycmdline,
// which the original CentOS 7 dracut does not read. Rocky 8.10 also reaches
// Anaconda without the selected Kickstart. The same issue was observed on
// Rocky 9.2 with iVentoy 1.0.45 Pro. Its supported injection
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

// A shared ISO also boots without an answer file. Only supply inst.ks when
// iVentoy actually selected this entry's script. The early dracut hook covers
// installers which populate /ventoy/autoinstall after the live injection hook;
// updating CMDLINE also handles dracut's already cached command line.
func kickstartSelectionInjection(answer string) (string, error) {
	const hook = `#!/bin/sh
vc_cmp=cmp
[ ! -x /ventoy/busybox/cmp ] || vc_cmp=/ventoy/busybox/cmp
if [ -s /ventoy/autoinstall ] && "$vc_cmp" -s /ventoy/autoinstall /ventoy-center/selected-kickstart.cfg; then
    mkdir -p /etc/cmdline.d
    printf '%s\n' 'inst.ks=file:/ventoy/autoinstall' > /etc/cmdline.d/99-ventoy-center-ks.conf
    case " ${CMDLINE:-} " in
        *' inst.ks=file:/ventoy/autoinstall '*) ;;
        *) if [ "${CMDLINE+x}" = x ]; then CMDLINE="$CMDLINE inst.ks=file:/ventoy/autoinstall"; fi ;;
    esac
fi
unset vc_cmp
`
	b, err := packInjection(map[string]injectionFile{
		liveInjectionHook: {[]byte(hook), 0755},
		"usr/lib/dracut/hooks/cmdline/01-ventoy-center-ks.sh": {[]byte(hook), 0755},
		"ventoy-center/selected-kickstart.cfg":                {[]byte(answer), 0600},
	})
	return string(b), err
}
