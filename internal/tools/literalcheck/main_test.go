package main

import "testing"

// URLs are assembled at run time so that this file does not trip the check itself.
const sep = "://"

func TestCheckGo(t *testing.T) {
	src := "package x\n\n" +
		"// A comment with a URL is fine: https" + sep + "docs.invalid-but-comment.org\n" +
		"var a = \"https" + sep + "registry.npmjs.org/\"\n" +
		"var b = \"https" + sep + "nexus.example.com/repository/npm/\"\n" +
		"var c = `git+https" + sep + "git.corp.internal/x.git`\n" +
		"var d = \"http" + sep + "127.0.0.1:7878\"\n" +
		"var e = \"${java.vendorApi}\"\n"
	got := checkGo("x.go", []byte(src))
	wantLines := []int{4, 6}
	if len(got) != len(wantLines) {
		t.Fatalf("findings = %v, want lines %v", got, wantLines)
	}
	for i, f := range got {
		if f.Line != wantLines[i] {
			t.Errorf("finding %d on line %d, want %d", i, f.Line, wantLines[i])
		}
	}
}

func TestCheckScript(t *testing.T) {
	src := "// https" + sep + "comment.org\n" +
		"/*\n * https" + sep + "block.org\n */\n" +
		"const ok = 'https" + sep + "api.example.org/v1';\n" +
		"const bad = 'wss" + sep + "real-host.org/ws';\n" +
		"const product = width\n" +
		"  * fetchSize('https" + sep + "registry.npmjs.org');\n"
	got := checkScript("x.ts", []byte(src))
	if len(got) != 2 || got[0].Line != 6 || got[1].Line != 8 {
		t.Fatalf("findings = %v, want lines 6 and 8", got)
	}
}

func TestAllowedHost(t *testing.T) {
	for host, want := range map[string]bool{
		"example.com":       true,
		"nexus.example.com": true,
		"localhost":         true,
		"::1":               true,
		"svc.test":          true,
		"github.com":        false,
		"example.com.evil":  false,
		"notexample.com":    false,
	} {
		if got := allowedHost(host); got != want {
			t.Errorf("allowedHost(%q) = %v, want %v", host, got, want)
		}
	}
}
