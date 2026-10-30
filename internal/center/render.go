package center

import (
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"crypto/sha512"
	"embed"
	"encoding/base64"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"sort"
	"strings"
	"text/template"
)

//go:embed templates/*
var templates embed.FS

//go:embed assets/safe-server-init.sh
var serverInit string

type renderData struct {
	PrivateJob
	Target          Profile
	Base            string
	CA              string
	Guard           string
	Bootstrap       string
	VersionCheck    string
	DebianLate      string
	UbiquityLate    string
	UbuntuStorage   string
	UbuntuSizes     string
	StorageSizing   string
	StoragePreview  string
	InstallerCache  string
	CacheHost       string
	CacheDirectory  string
	StorageGuard    string
	StorageRecipe   string
	DataFilesystem  string
	FilesystemCheck string
	RAIDSetup       string
	InstallFailure  string
	FailureShell    string
	NetworkKeyfiles string
	Partition       int
	InjectionName   string
	UserDataSHA512  string
	KernelPackages  string
	UpdatePolicy    string
	SSHPolicy       string
	ServerInit      string
	UbuntuKernel    string
	UbuntuReboot    string
	PXEMenu         bool
	PXESettings     string
	PXEISO          string
	PXECommand      string
	OfflineMedia    bool
	SetupPreload    bool
}

func render(name string, data renderData) (string, error) {
	if data.DataFilesystem == "" {
		data.DataFilesystem = "ext4"
	}
	functions := template.FuncMap{
		// Debian 10's installer BusyBox omits base64. Encode shell metacharacters
		// and line breaks for its built-in printf without requiring a new binary.
		"printfb": func(s string) string {
			s = strings.NewReplacer("\\", "\\0134", "'", "\\0047", "\n", "\\0012", "\r", "\\0015", "\t", "\\0011").Replace(s)
			return "printf '%b' '" + s + "'"
		},
		"sh":   func(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" },
		"ps":   func(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" },
		"yaml": func(s string) string { b, _ := json.Marshal(s); return string(b) },
		"xml":  func(s string) string { var b bytes.Buffer; xml.EscapeText(&b, []byte(s)); return b.String() },
		"b64":  func(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) },
		// Ubiquity and Debian preseed pass hooks as one /bin/sh -c argument. The
		// nested policy scripts can exceed Linux's per-argument size limit.
		"b64gz": func(s string) (string, error) {
			var b bytes.Buffer
			z := gzip.NewWriter(&b)
			if _, err := z.Write([]byte(s)); err != nil {
				return "", err
			}
			if err := z.Close(); err != nil {
				return "", err
			}
			encoded := base64.StdEncoding.EncodeToString(b.Bytes())
			if len(encoded) > 112*1024 {
				return "", fmt.Errorf("compressed installer script exceeds command argument budget (%d bytes)", len(encoded))
			}
			return encoded, nil
		},
		"indent": func(n int, s string) string {
			p := strings.Repeat(" ", n)
			return p + strings.ReplaceAll(strings.TrimRight(s, "\n"), "\n", "\n"+p)
		},
	}
	t, err := template.New(name).Funcs(functions).ParseFS(templates, "templates/"+name)
	if err != nil {
		return "", err
	}
	var b bytes.Buffer
	err = t.Execute(&b, data)
	return b.String(), err
}

func (s *Server) renderFiles(j PrivateJob) (map[string]string, error) {
	p, err := resolveProfile(j.Profile, j.OS)
	if err != nil {
		return nil, err
	}
	d := renderData{PrivateJob: j, Target: p, Base: s.config.PublicURL + "/bootstrap/" + j.ID, CA: string(s.ca), Partition: 3, OfflineMedia: debianDVD(p, j.ISO)}
	s.cacheRenderSettings(&d)
	if p.OS != "windows" {
		d.DataFilesystem, err = linuxFilesystem(j.Request.Filesystem)
		if err != nil {
			return nil, err
		}
	} else if j.Request.Filesystem != "" {
		return nil, fmt.Errorf("Linux filesystem selection does not apply to Windows")
	}
	d.SetupPreload = s.config.LinuxSetupPreload && p.OS != "windows"
	d.InjectionName = bootInjectionName(p.ID)
	if j.Machine.Firmware == "bios" {
		d.Partition = 2
	}
	files := map[string]string{}
	if j.Request.StoragePlan != nil {
		if err := validateStorageShape(j.Request.StoragePlan); err != nil {
			return nil, err
		}
		if !rebuildInstaller(p) || (raidPlan(j.Request.StoragePlan) && !raidInstaller(p)) || j.Machine.DiskSerial != j.Request.StoragePlan.DiskSerials[0] {
			return nil, fmt.Errorf("Storage rebuild does not match the installer or primary disk")
		}
		d.StorageGuard, err = render("storage-raid-guard.sh", d)
		if err != nil {
			return nil, err
		}
	}
	if raidPlan(j.Request.StoragePlan) {
		d.RAIDSetup, err = render("raid-installed.sh", d)
		if err != nil {
			return nil, err
		}
		files["raid-installed.sh"] = d.RAIDSetup
		if p.Installer != "autoinstall" {
			name := "raid-kickstart.sh"
			if p.Installer == "preseed" {
				name = "raid-debian.sh"
			}
			d.StorageRecipe, err = render(name, d)
			if err != nil {
				return nil, err
			}
		}
	}
	if j.OS == "windows" {
		for _, pair := range [][2]string{{"windows.xml", "autounattend.xml"}, {"guard.ps1", "injection/ventoy-center/guard.ps1"}, {"postinstall.ps1", "injection/ventoy-center/postinstall.ps1"}, {"preinstall.cmd", "injection/ventoy-center/preinstall.cmd"}, {"make-injection.ps1", "Make-Injection.ps1"}} {
			files[pair[1]], err = render(pair[0], d)
			if err != nil {
				return nil, err
			}
		}
		files["injection/ventoy-center/ca.crt"] = d.CA
	} else {
		d.FilesystemCheck, err = render("filesystem-check.sh", d)
		if err != nil {
			return nil, err
		}
		files["filesystem-check.sh"] = d.FilesystemCheck
		d.StorageSizing, err = render("storage-sizing.sh", d)
		if err != nil {
			return nil, err
		}
		if !raidPlan(j.Request.StoragePlan) && p.Installer != "autoinstall" {
			name := "single-kickstart.sh"
			if p.Installer == "preseed" || p.Installer == "ubiquity" {
				name = "single-debian.sh"
			}
			d.StorageRecipe, err = render(name, d)
			if err != nil {
				return nil, err
			}
		}
		d.FailureShell, err = render("installer-failure.sh", d)
		if err != nil {
			return nil, err
		}
		d.ServerInit = serverInit
		files["safe-server-init.sh"] = serverInit
		if j.Request.RootPasswordHash != "" {
			d.SSHPolicy, err = render("root-ssh-policy.sh", d)
			if err != nil {
				return nil, err
			}
			files["root-ssh-policy.sh"] = d.SSHPolicy
		}
		d.KernelPackages = kernelPackagePatterns()
		d.UpdatePolicy, err = render("linux-update-policy.sh", d)
		if err != nil {
			return nil, err
		}
		files["linux-update-policy.sh"] = d.UpdatePolicy
		d.VersionCheck, err = render("version-check.sh", d)
		if err != nil {
			return nil, err
		}
		guardName := "guard.sh"
		if p.Installer == "autoinstall" {
			d.UbuntuSizes, err = render("ubuntu-storage-sizes.py", d)
			if err != nil {
				return nil, err
			}
			d.InstallFailure, err = render("ubuntu-install-failed.py", d)
			if err != nil {
				return nil, err
			}
			files["ubuntu-install-failed.py"] = d.InstallFailure
			d.UbuntuKernel, err = render("ubuntu-kernel.py", d)
			if err != nil {
				return nil, err
			}
			storageTemplate := "ubuntu-storage.py"
			if raidPlan(j.Request.StoragePlan) {
				storageTemplate = "ubuntu-raid-storage.py"
			}
			d.UbuntuStorage, err = render(storageTemplate, d)
			if err != nil {
				return nil, err
			}
		}
		if p.OS == "debian" {
			guardName = "debian-guard.sh"
		}
		d.Guard, err = render(guardName, d)
		if err != nil {
			return nil, err
		}
		d.Bootstrap, err = render("bootstrap.sh", d)
		if err != nil {
			return nil, err
		}
		name := "kickstart.cfg"
		if p.ID == "rocky-9" || p.ID == "almalinux-9" || p.ID == "rhel-9" {
			d.NetworkKeyfiles, err = render("network-keyfiles.py", d)
			if err != nil {
				return nil, err
			}
		}
		if j.OS == "ubuntu" {
			name = "user-data"
			d.UbuntuReboot, err = render("ubuntu-reboot.sh", d)
			if err != nil {
				return nil, err
			}
			files["ubuntu-reboot.sh"] = d.UbuntuReboot
		}
		if j.OS == "debian" {
			name = "preseed.cfg"
			d.DebianLate, err = render("debian-late.sh", d)
			if err != nil {
				return nil, err
			}
		}
		tpl := name
		if p.Installer == "ubiquity" {
			name = "preseed.cfg"
			tpl = "ubiquity.cfg"
			d.DebianLate, err = render("debian-late.sh", d)
			if err != nil {
				return nil, err
			}
			d.UbiquityLate, err = render("ubiquity-late.sh", d)
			if err != nil {
				return nil, err
			}
		}
		files[name], err = render(tpl, d)
		if err != nil {
			return nil, err
		}
		if j.PXEEntryID != "" {
			files["preinstall.sh"] = d.Guard
			if p.Installer == "kickstart" {
				files["preinstall.sh"] += "\n" + d.StorageRecipe
			}
		}
		if d.InjectionName != "" {
			if p.OS == "debian" {
				var empty []byte
				empty, err = packInjection(nil)
				files[d.InjectionName] = string(empty)
			} else if p.OS == "ubuntu" {
				// Ubuntu 22.04.3's initramfs includes sha512sum, not sha256sum.
				d.UserDataSHA512 = fmt.Sprintf("%x", sha512.Sum512([]byte(files[name])))
				files[d.InjectionName], err = ubuntuInjection(d)
			} else {
				files[d.InjectionName], err = kickstartInjection()
			}
			if err != nil {
				return nil, err
			}
		}
	}
	if err = s.addE610Media(j.ISO, p.ID, files); err != nil {
		return nil, err
	}
	if err = s.addLinuxSetupPreload(p.ID, files); err != nil {
		return nil, err
	}
	files["README.txt"], err = render("bundle.txt", d)
	if err != nil {
		return nil, err
	}
	manifest, _ := json.MarshalIndent(j.Job, "", "  ")
	files["manifest.json"] = string(manifest) + "\n"
	keys := make([]string, 0, len(files))
	for k := range files {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var sums strings.Builder
	for _, name := range keys {
		fmt.Fprintf(&sums, "%x  %s\n", sha256.Sum256([]byte(files[name])), name)
	}
	files["SHA256SUMS"] = sums.String()
	return files, nil
}

func kernelPackagePatterns() string {
	b, _ := templates.ReadFile("templates/kernel-packages.txt")
	return strings.TrimSpace(string(b))
}

func (s *Server) renderBundle(j PrivateJob) ([]byte, error) {
	files, err := s.renderFiles(j)
	if err != nil {
		return nil, err
	}
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	keys := make([]string, 0, len(files))
	for k := range files {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, name := range keys {
		h := &zip.FileHeader{Name: name, Method: zip.Deflate}
		h.SetMode(0600)
		h.SetModTime(j.Created)
		w, err := z.CreateHeader(h)
		if err != nil {
			return nil, err
		}
		if _, err = w.Write([]byte(files[name])); err != nil {
			return nil, err
		}
	}
	if err = z.Close(); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}
