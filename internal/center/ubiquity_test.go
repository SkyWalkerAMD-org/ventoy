package center

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"io"
	"math/rand"
	"regexp"
	"strings"
	"testing"
)

func decodeTemplateScript(t *testing.T, encoded string) string {
	t.Helper()
	b, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.HasPrefix(b, []byte{0x1f, 0x8b}) {
		z, err := gzip.NewReader(bytes.NewReader(b))
		if err != nil {
			t.Fatal(err)
		}
		defer z.Close()
		b, err = io.ReadAll(z)
		if err != nil {
			t.Fatal(err)
		}
	}
	return string(b)
}

func TestUbiquityLargeSuccessScriptFitsExecArgument(t *testing.T) {
	// The real nested policy exceeded 128 KiB when encoded without compression.
	script := "#!/bin/sh\n" + strings.Repeat("printf '%s\\n' 'literal $HOME and 中文'\n", 5000)
	if len(script) < 128*1024 {
		t.Fatal("fixture must reproduce the original argument-size failure")
	}
	text, err := render("ubiquity.cfg", renderData{UbiquityLate: script})
	if err != nil {
		t.Fatal(err)
	}
	text = strings.ReplaceAll(text, "\\\n", " ")
	const prefix = "ubiquity ubiquity/success_command string "
	command := ""
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, prefix) {
			command = strings.TrimPrefix(line, prefix)
		}
	}
	if command == "" || len(command) >= 128*1024 {
		t.Fatalf("success command cannot be passed to exec: %d bytes", len(command))
	}
	match := regexp.MustCompile(`printf '%s' '([A-Za-z0-9+/=]+)' \| base64 -d \| gzip -dc`).FindStringSubmatch(command)
	if len(match) != 2 || decodeTemplateScript(t, match[1]) != script {
		t.Fatal("success hook did not preserve the complete script")
	}
	checkShell(t, "ubiquity-success-command", command)
}

func TestUbiquityRejectsIncompressibleOversizedScript(t *testing.T) {
	b := make([]byte, 200*1024)
	_, _ = rand.New(rand.NewSource(1)).Read(b)
	_, err := render("ubiquity.cfg", renderData{UbiquityLate: string(b)})
	if err == nil || !strings.Contains(err.Error(), "argument budget") {
		t.Fatal("oversized script must fail before an installation is issued", err)
	}
}
