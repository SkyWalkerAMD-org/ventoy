package center

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
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

type renderData struct {
	PrivateJob
	Base      string
	CA        string
	Guard     string
	Bootstrap string
	Partition int
}

func render(name string, data renderData) (string, error) {
	functions := template.FuncMap{
		"sh":   func(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" },
		"ps":   func(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" },
		"yaml": func(s string) string { b, _ := json.Marshal(s); return string(b) },
		"xml":  func(s string) string { var b bytes.Buffer; xml.EscapeText(&b, []byte(s)); return b.String() },
		"b64":  func(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) },
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
	d := renderData{PrivateJob: j, Base: s.config.PublicURL + "/bootstrap/" + j.ID, CA: string(s.ca), Partition: 3}
	if j.Machine.Firmware == "bios" {
		d.Partition = 2
	}
	files := map[string]string{}
	var err error
	if j.OS == "windows" {
		for _, pair := range [][2]string{{"windows.xml", "autounattend.xml"}, {"guard.ps1", "injection/ventoy-center/guard.ps1"}, {"postinstall.ps1", "injection/ventoy-center/postinstall.ps1"}, {"preinstall.cmd", "injection/ventoy-center/preinstall.cmd"}, {"make-injection.ps1", "Make-Injection.ps1"}} {
			files[pair[1]], err = render(pair[0], d)
			if err != nil {
				return nil, err
			}
		}
		files["injection/ventoy-center/ca.crt"] = d.CA
	} else {
		d.Guard, err = render("guard.sh", d)
		if err != nil {
			return nil, err
		}
		d.Bootstrap, err = render("bootstrap.sh", d)
		if err != nil {
			return nil, err
		}
		name := "kickstart.cfg"
		if j.OS == "ubuntu" {
			name = "user-data"
		}
		files[name], err = render(name, d)
		if err != nil {
			return nil, err
		}
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
