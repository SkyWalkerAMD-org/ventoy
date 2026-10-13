package center

import (
	"errors"
	"path/filepath"
	"regexp"
	"strings"
)

// Profiles describe generated installer configurations, not hardware certification.
type Profile struct {
	ID        string `json:"id"`
	OS        string `json:"os"`
	Label     string `json:"label"`
	Version   string `json:"version"`
	Installer string `json:"installer"`
	Media     string `json:"media"`
	Notes     string `json:"notes"`
	Suite     string `json:"-"`
}

var profiles = []Profile{
	{ID: "rocky-9", OS: "rocky", Label: "Rocky Linux 9.x（含 9.8）", Version: "9", Installer: "kickstart", Media: "x86_64 DVD ISO", Notes: "首要适配目标；模板已实现，实机 PXE 待验收。"},
	{ID: "rocky-8", OS: "rocky", Label: "Rocky Linux 8.x（至 8.10）", Version: "8", Installer: "kickstart", Media: "x86_64 DVD ISO", Notes: "模板已实现；具体小版本与驱动需实机验收。"},
	{ID: "rocky-10", OS: "rocky", Label: "Rocky Linux 10.x（含 10.2）", Version: "10", Installer: "kickstart", Media: "x86_64 DVD ISO", Notes: "需要符合 x86-64-v3 的 CPU；模板已实现，实机待验收。"},
	{ID: "centos-7.9", OS: "centos", Label: "CentOS Linux 7.9", Version: "7.9", Installer: "kickstart", Media: "x86_64 DVD / Everything 2009 ISO", Notes: "旧环境兼容；已停止上游维护。使用完整安装镜像，软件源需另行准备。"},
	{ID: "ubuntu-20.04", OS: "ubuntu", Label: "Ubuntu Server 20.04 LTS", Version: "20.04", Installer: "autoinstall", Media: "amd64 live-server ISO", Notes: "使用 20.04.6 live-server；不适用于旧版 legacy-server 或 Desktop ISO。"},
	{ID: "ubuntu-22.04", OS: "ubuntu", Label: "Ubuntu Server 22.04 LTS", Version: "22.04", Installer: "autoinstall", Media: "amd64 live-server ISO", Notes: "模板已实现；安装器及网卡驱动需实机验收。"},
	{ID: "ubuntu-24.04", OS: "ubuntu", Label: "Ubuntu Server 24.04 LTS", Version: "24.04", Installer: "autoinstall", Media: "amd64 live-server ISO", Notes: "模板已实现；安装器及网卡驱动需实机验收。"},
	{ID: "ubuntu-26.04", OS: "ubuntu", Label: "Ubuntu Server 26.04 LTS", Version: "26.04", Installer: "autoinstall", Media: "amd64 live-server ISO", Notes: "模板已实现；安装器及网卡驱动需实机验收。"},
	{ID: "debian-11", OS: "debian", Label: "Debian 11（Bullseye）", Version: "11", Installer: "preseed", Media: "amd64 netinst / DVD-1 ISO", Notes: "需要可达的 Debian 软件源和控制器 CA；模板已实现，实机待验收。", Suite: "bullseye"},
	{ID: "debian-12", OS: "debian", Label: "Debian 12（Bookworm）", Version: "12", Installer: "preseed", Media: "amd64 netinst / DVD-1 ISO", Notes: "需要可达的 Debian 软件源和控制器 CA；模板已实现，实机待验收。", Suite: "bookworm"},
	{ID: "debian-13", OS: "debian", Label: "Debian 13（Trixie）", Version: "13", Installer: "preseed", Media: "amd64 netinst / DVD-1 ISO", Notes: "需要可达的 Debian 软件源和控制器 CA；模板已实现，实机待验收。", Suite: "trixie"},
	{ID: "windows-11", OS: "windows", Label: "Windows 11（次要支持）", Version: "11", Installer: "unattend", Media: "x64 ISO + 自备 WinPE 组件", Notes: "仅 UEFI；需要 WinPE PowerShell / StorageWMI，实机待验收。"},
	{ID: "windows-server-2022", OS: "windows", Label: "Windows Server 2022（次要支持）", Version: "2022", Installer: "unattend", Media: "x64 ISO + 自备 WinPE 组件", Notes: "保留安装模板，当前开发优先 Linux。"},
}

func findProfile(id string) (Profile, bool) {
	for _, p := range profiles {
		if p.ID == id {
			return p, true
		}
	}
	return Profile{}, false
}

func resolveProfile(id, osName string) (Profile, error) {
	// Preserve the meaning of alpha.1 requests and stored jobs.
	if id == "" {
		id = map[string]string{"rocky": "rocky-9", "ubuntu": "ubuntu-24.04", "windows": "windows-server-2022"}[osName]
	}
	p, ok := findProfile(id)
	if !ok || (osName != "" && osName != p.OS) {
		return Profile{}, errors.New("请选择有效的系统版本；系统类型必须与版本一致")
	}
	return p, nil
}

var mediaPatterns = []struct {
	re      *regexp.Regexp
	profile string
}{
	{regexp.MustCompile(`(?i)^centos[-_]7\.9([._-]|$)`), "centos-7.9"},
	// The official 7.9 release uses 7 + build 2009 in its ISO filenames.
	{regexp.MustCompile(`(?i)^centos[-_]7[-_]x86_64[-_](dvd|everything|minimal|netinstall)[-_]2009([._-]|$)`), "centos-7.9"},
	{regexp.MustCompile(`(?i)^rocky[-_]8[._-]`), "rocky-8"},
	{regexp.MustCompile(`(?i)^rocky[-_]9[._-]`), "rocky-9"},
	{regexp.MustCompile(`(?i)^rocky[-_]10[._-]`), "rocky-10"},
	{regexp.MustCompile(`(?i)^ubuntu[-_]20\.04[._-]`), "ubuntu-20.04"},
	{regexp.MustCompile(`(?i)^ubuntu[-_]22\.04[._-]`), "ubuntu-22.04"},
	{regexp.MustCompile(`(?i)^ubuntu[-_]24\.04[._-]`), "ubuntu-24.04"},
	{regexp.MustCompile(`(?i)^ubuntu[-_]26\.04[._-]`), "ubuntu-26.04"},
	{regexp.MustCompile(`(?i)^debian[-_]11[._-]`), "debian-11"},
	{regexp.MustCompile(`(?i)^debian[-_]12[._-]`), "debian-12"},
	{regexp.MustCompile(`(?i)^debian[-_]13[._-]`), "debian-13"},
}

// Filename hints are for selection only; they do not attest to ISO contents.
func suggestedProfile(path string) string {
	name := filepath.Base(path)
	for _, pattern := range mediaPatterns {
		if pattern.re.MatchString(name) {
			return pattern.profile
		}
	}
	return ""
}

func validateMediaProfile(p Profile, path string) error {
	if hint := suggestedProfile(path); hint != "" && hint != p.ID {
		return errors.New("镜像文件名与所选系统版本不一致，请重新核对")
	}
	name := strings.ToLower(filepath.Base(path))
	if p.OS != "windows" && (strings.Contains(name, "aarch64") || strings.Contains(name, "arm64") || strings.Contains(name, "i386") || strings.Contains(name, "ppc64") || strings.Contains(name, "s390x")) {
		return errors.New("当前模板仅适配 x86-64 / amd64 安装镜像")
	}
	if p.OS == "ubuntu" && (strings.Contains(name, "desktop") || strings.Contains(name, "legacy-server")) {
		return errors.New("Ubuntu 模板需要 live-server 安装镜像")
	}
	if p.OS == "centos" && (strings.Contains(name, "netinstall") || strings.Contains(name, "minimal") || strings.Contains(name, "live")) {
		return errors.New("CentOS 7.9 模板需要完整 DVD / Everything 2009 镜像")
	}
	if p.OS == "debian" && strings.Contains(name, "live") {
		return errors.New("Debian 模板需要 netinst 或 DVD-1，不能使用 Live 镜像")
	}
	return nil
}
