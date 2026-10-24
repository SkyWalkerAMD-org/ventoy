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
	Archived  bool   `json:"-"`
	Desktop   bool   `json:"desktop,omitempty"`
}

var profiles = []Profile{
	{ID: "centos-7.6", OS: "centos", Label: "CentOS Linux 7.6", Version: "7.6", Installer: "kickstart", Media: "x86_64 DVD 1810 ISO", Notes: "原版介质精确内核适配；E610 实卡验收待测。"},
	{ID: "centos-8.0", OS: "centos", Label: "CentOS Linux 8.0.1905", Version: "8.0", Installer: "kickstart", Media: "x86_64 完整 DVD ISO", Notes: "旧版完整光盘安装，不在线升级内核。"},
	{ID: "centos-8.4", OS: "centos", Label: "CentOS Linux 8.4.2105", Version: "8.4", Installer: "kickstart", Media: "x86_64 完整 DVD ISO", Notes: "旧版完整光盘安装，不在线升级内核。"},
	{ID: "centos-stream-8", OS: "centos", Label: "CentOS Stream 8", Version: "8", Installer: "kickstart", Media: "x86_64 完整 DVD ISO", Notes: "保留 Stream 发行版校验，实卡验收待测。"},
	{ID: "almalinux-8", OS: "almalinux", Label: "AlmaLinux 8.x", Version: "8", Installer: "kickstart", Media: "x86_64 完整 DVD ISO", Notes: "统一 PXE 选盘、RAID1/LVM 与安装后策略；独立发行版校验，实装待验收。"},
	{ID: "almalinux-9", OS: "almalinux", Label: "AlmaLinux 9.x", Version: "9", Installer: "kickstart", Media: "x86_64 完整 DVD ISO", Notes: "统一 PXE 选盘、RAID1/LVM 与安装后策略；独立发行版校验，实装待验收。"},
	{ID: "rhel-7", OS: "rhel", Label: "Red Hat Enterprise Linux 7.x", Version: "7", Installer: "kickstart", Media: "x86_64 Server DVD ISO", Notes: "使用已有完整 DVD 安装；不配置订阅或自动更新。实装待验收。"},
	{ID: "rhel-8", OS: "rhel", Label: "Red Hat Enterprise Linux 8.x", Version: "8", Installer: "kickstart", Media: "x86_64 完整 DVD ISO", Notes: "使用已有完整 DVD 安装；不配置订阅或自动更新。实装待验收。"},
	{ID: "rhel-9", OS: "rhel", Label: "Red Hat Enterprise Linux 9.x", Version: "9", Installer: "kickstart", Media: "x86_64 完整 DVD ISO", Notes: "使用已有完整 DVD 安装；不配置订阅或自动更新。实装待验收。"},
	{ID: "rocky-9", OS: "rocky", Label: "Rocky Linux 9.x（含 9.8）", Version: "9", Installer: "kickstart", Media: "x86_64 DVD ISO", Notes: "首要适配目标；模板已实现，实机 PXE 待验收。"},
	{ID: "rocky-8", OS: "rocky", Label: "Rocky Linux 8.x（至 8.10）", Version: "8", Installer: "kickstart", Media: "x86_64 DVD ISO", Notes: "模板已实现；具体小版本与驱动需实机验收。"},
	{ID: "rocky-10", OS: "rocky", Label: "Rocky Linux 10.x（含 10.2）", Version: "10", Installer: "kickstart", Media: "x86_64 DVD ISO", Notes: "需要符合 x86-64-v3 的 CPU；模板已实现，实机待验收。"},
	{ID: "centos-7.9", OS: "centos", Label: "CentOS Linux 7.9", Version: "7.9", Installer: "kickstart", Media: "x86_64 DVD / Everything 2009 ISO", Notes: "旧环境兼容；已停止上游维护。使用完整安装镜像，软件源需另行准备。"},
	{ID: "centos-8.5", OS: "centos", Label: "CentOS Linux 8.5.2111", Version: "8.5", Installer: "kickstart", Media: "x86_64 完整 DVD ISO", Notes: "旧环境兼容；已停止上游维护。独立版本保护，实机安装待验收。"},
	{ID: "centos-stream-9", OS: "centos", Label: "CentOS Stream 9", Version: "9", Installer: "kickstart", Media: "x86_64 完整 DVD ISO", Notes: "核对 Stream 发行版身份；具体镜像与硬件需实机验收。"},
	{ID: "centos-stream-10", OS: "centos", Label: "CentOS Stream 10", Version: "10", Installer: "kickstart", Media: "x86_64 完整 DVD ISO", Notes: "需要符合 x86-64-v3 的 CPU；具体镜像与硬件需实机验收。"},
	{ID: "ubuntu-20.04", OS: "ubuntu", Label: "Ubuntu Server 20.04 LTS", Version: "20.04", Installer: "autoinstall", Media: "amd64 live-server ISO", Notes: "适配现场 20.04.3 live-server；不适用于 legacy-server 或 Desktop ISO。"},
	{ID: "ubuntu-22.04", OS: "ubuntu", Label: "Ubuntu Server 22.04 LTS", Version: "22.04", Installer: "autoinstall", Media: "amd64 live-server ISO", Notes: "模板已实现；安装器及网卡驱动需实机验收。"},
	{ID: "ubuntu-24.04", OS: "ubuntu", Label: "Ubuntu Server 24.04 LTS", Version: "24.04", Installer: "autoinstall", Media: "amd64 live-server ISO", Notes: "模板已实现；安装器及网卡驱动需实机验收。"},
	{ID: "ubuntu-24.10", OS: "ubuntu", Label: "Ubuntu Server 24.10", Version: "24.10", Installer: "autoinstall", Media: "amd64 live-server ISO", Notes: "现场旧版兼容；使用官方归档软件源。", Archived: true},
	{ID: "ubuntu-25.04", OS: "ubuntu", Label: "Ubuntu Server 25.04", Version: "25.04", Installer: "autoinstall", Media: "amd64 live-server ISO", Notes: "现场旧版兼容；使用官方归档软件源。", Archived: true},
	{ID: "ubuntu-26.04", OS: "ubuntu", Label: "Ubuntu Server 26.04 LTS", Version: "26.04", Installer: "autoinstall", Media: "amd64 live-server ISO", Notes: "模板已实现；安装器及网卡驱动需实机验收。"},
	{ID: "ubuntu-22.04-desktop", OS: "ubuntu", Label: "Ubuntu Desktop 22.04 LTS", Version: "22.04", Installer: "ubiquity", Media: "amd64 Desktop ISO", Notes: "原生 Ubiquity 自动安装；默认 CLI 登录，应用统一账户和内核更新策略。Desktop 介质中的桌面软件仍保留。", Desktop: true},
	{ID: "ubuntu-26.04-desktop", OS: "ubuntu", Label: "Ubuntu Desktop 26.04 LTS", Version: "26.04", Installer: "autoinstall", Media: "amd64 Desktop ISO", Notes: "原生 Desktop Subiquity 自动安装；默认 CLI 登录，应用统一账户和内核更新策略。Desktop 介质中的桌面软件仍保留。", Desktop: true},
	{ID: "debian-10", OS: "debian", Label: "Debian 10（Buster）", Version: "10", Installer: "preseed", Media: "amd64 DVD-1 ISO", Notes: "现场旧版 DVD 兼容；从完整光盘安装，不依赖已迁移的软件源。", Suite: "buster", Archived: true},
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

// Full DVD media supplies the installation packages. Avoid fetching newer
// packages while installing an older point release; netinst still needs a mirror.
func debianDVD(p Profile, iso string) bool {
	return p.OS == "debian" && strings.Contains(strings.ToLower(filepath.Base(iso)), "dvd-1")
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
	{regexp.MustCompile(`(?i)^centos[-_]7\.6([._-]|$)`), "centos-7.6"},
	{regexp.MustCompile(`(?i)^centos[-_]7[-_]x86_64[-_]dvd[-_]1810([._-]|$)`), "centos-7.6"},
	{regexp.MustCompile(`(?i)^centos[-_]8\.0([._-]|$)`), "centos-8.0"},
	{regexp.MustCompile(`(?i)^centos[-_]8[-_]x86_64[-_]1905[-_]`), "centos-8.0"},
	{regexp.MustCompile(`(?i)^centos[-_]8\.4([._-]|$)`), "centos-8.4"},
	{regexp.MustCompile(`(?i)^centos[-_]stream[-_]8([._-]|$)`), "centos-stream-8"},
	{regexp.MustCompile(`(?i)^almalinux[-_]8[._-]`), "almalinux-8"},
	{regexp.MustCompile(`(?i)^almalinux[-_]9[._-]`), "almalinux-9"},
	{regexp.MustCompile(`(?i)^rhel[-_](server[-_])?7[._-]`), "rhel-7"},
	{regexp.MustCompile(`(?i)^rhel[-_]8[._-]`), "rhel-8"},
	{regexp.MustCompile(`(?i)^rhel[-_]9[._-]`), "rhel-9"},
	{regexp.MustCompile(`(?i)^centos[-_]7\.9([._-]|$)`), "centos-7.9"},
	// The official 7.9 release uses 7 + build 2009 in its ISO filenames.
	{regexp.MustCompile(`(?i)^centos[-_]7[-_]x86_64[-_](dvd|everything|minimal|netinstall)[-_]2009([._-]|$)`), "centos-7.9"},
	{regexp.MustCompile(`(?i)^centos[-_]8\.5([._-]|$)`), "centos-8.5"},
	{regexp.MustCompile(`(?i)^centos[-_]stream[-_]9([._-]|$)`), "centos-stream-9"},
	{regexp.MustCompile(`(?i)^centos[-_]stream[-_]10([._-]|$)`), "centos-stream-10"},
	{regexp.MustCompile(`(?i)^rocky[-_]8[._-]`), "rocky-8"},
	{regexp.MustCompile(`(?i)^rocky[-_]9[._-]`), "rocky-9"},
	{regexp.MustCompile(`(?i)^rocky[-_]10[._-]`), "rocky-10"},
	{regexp.MustCompile(`(?i)^ubuntu[-_]22\.04([._-].*)?[-_]desktop[-_]`), "ubuntu-22.04-desktop"},
	{regexp.MustCompile(`(?i)^ubuntu[-_]26\.04([._-].*)?[-_]desktop[-_]`), "ubuntu-26.04-desktop"},
	{regexp.MustCompile(`(?i)^ubuntu[-_]20\.04[._-]`), "ubuntu-20.04"},
	{regexp.MustCompile(`(?i)^ubuntu[-_]22\.04[._-]`), "ubuntu-22.04"},
	{regexp.MustCompile(`(?i)^ubuntu[-_]24\.04[._-]`), "ubuntu-24.04"},
	{regexp.MustCompile(`(?i)^ubuntu[-_]24\.10[._-]`), "ubuntu-24.10"},
	{regexp.MustCompile(`(?i)^ubuntu[-_]25\.04[._-]`), "ubuntu-25.04"},
	{regexp.MustCompile(`(?i)^ubuntu[-_]26\.04[._-]`), "ubuntu-26.04"},
	{regexp.MustCompile(`(?i)^debian[-_]10[._-]`), "debian-10"},
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
	if p.OS == "ubuntu" && (strings.Contains(name, "legacy-server") || (strings.Contains(name, "desktop") && !p.Desktop) || (strings.Contains(name, "server") && p.Desktop)) {
		return errors.New("Ubuntu 镜像的 Server / Desktop 类型必须与模板一致")
	}
	if p.Installer == "kickstart" && (strings.Contains(name, "netinstall") || strings.Contains(name, "minimal") || strings.Contains(name, "live") || strings.Contains(name, "-boot")) {
		return errors.New("此模板需要完整 DVD 镜像；CentOS 7.9 也可使用 Everything 2009")
	}
	if p.OS == "debian" && strings.Contains(name, "live") {
		return errors.New("Debian 模板需要 netinst 或 DVD-1，不能使用 Live 镜像")
	}
	if p.ID == "debian-10" && strings.Contains(name, "netinst") {
		return errors.New("Debian 10 归档版本需要完整 DVD-1 镜像")
	}
	return nil
}
