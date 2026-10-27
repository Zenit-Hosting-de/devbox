// Command literalcheck is a lint tool (make lint). It enforces the rule "nothing is
// hard-coded": Go and TypeScript sources must not contain URL literals, except for
// reserved example and loopback hosts (RFC 2606, RFC 6761). Real addresses belong in
// plugin settings with a default in plugin.json, or in profiles.
//
// Go files are tokenised, so only string literals count, not comments. For TypeScript
// and JavaScript, lines that are comments are skipped.
package main

import (
	"bufio"
	"context"
	"fmt"
	"go/scanner"
	"go/token"
	"io"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/Zenit-Hosting-de/devbox/internal/tools/repofiles"
)

var exclude = []string{
	"internal/gen/",
	"web/src/app/gen/",
	"sdk/ts/src/gen/",
	"node_modules/",
	"test/fixtures/valid/concept/",
}

var urlPattern = regexp.MustCompile(`(?i)\b(?:https?|wss?|ftp|git\+https|git\+ssh|ssh)://[^\s"'` + "`" + `<>)]+`)

// allowedHost reports whether host is reserved for documentation or is a loopback host.
func allowedHost(host string) bool {
	host = strings.ToLower(host)
	if host == "" || strings.Contains(host, "${") || host == "localhost" || host == "127.0.0.1" || host == "::1" {
		return true
	}
	for _, reserved := range []string{"example.com", "example.org", "example.net", "example", "test", "invalid", "localhost"} {
		if host == reserved || strings.HasSuffix(host, "."+reserved) {
			return true
		}
	}
	return false
}

// Finding is one forbidden URL literal.
type Finding struct {
	File string
	Line int
	URL  string
}

func (f Finding) String() string {
	return fmt.Sprintf("%s:%d: URL literal %q; use a setting with a default in plugin.json or example.com in tests", f.File, f.Line, f.URL)
}

func forbiddenURLs(s string) []string {
	var out []string
	for _, raw := range urlPattern.FindAllString(s, -1) {
		u, err := url.Parse(raw)
		if err != nil || !allowedHost(u.Hostname()) {
			out = append(out, raw)
		}
	}
	return out
}

// checkGo reports URL literals in the string literals of Go source.
func checkGo(file string, src []byte) []Finding {
	fset := token.NewFileSet()
	tf := fset.AddFile(file, fset.Base(), len(src))
	var s scanner.Scanner
	s.Init(tf, src, nil, 0)
	var out []Finding
	for {
		pos, tok, lit := s.Scan()
		if tok == token.EOF {
			return out
		}
		if tok != token.STRING {
			continue
		}
		value, err := strconv.Unquote(lit)
		if err != nil {
			value = lit
		}
		for _, u := range forbiddenURLs(value) {
			out = append(out, Finding{File: file, Line: fset.Position(pos).Line, URL: u})
		}
	}
}

// checkScript reports URLs on non-comment lines of TypeScript or JavaScript source.
func checkScript(file string, src []byte) []Finding {
	var out []Finding
	sc := bufio.NewScanner(strings.NewReader(string(src)))
	inBlock := false
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSpace(sc.Text())
		if inBlock {
			if strings.Contains(line, "*/") {
				inBlock = false
			}
			continue
		}
		if strings.HasPrefix(line, "/*") {
			inBlock = !strings.Contains(line, "*/")
			continue
		}
		if strings.HasPrefix(line, "//") {
			continue
		}
		for _, u := range forbiddenURLs(line) {
			out = append(out, Finding{File: file, Line: n, URL: u})
		}
	}
	return out
}

func check(file string, src []byte) []Finding {
	if strings.HasSuffix(file, ".go") {
		return checkGo(file, src)
	}
	return checkScript(file, src)
}

func main() {
	os.Exit(run(context.Background(), os.Stdout))
}

func run(ctx context.Context, out io.Writer) int {
	files, err := repofiles.List(ctx, []string{".go", ".ts", ".mjs", ".js"}, exclude)
	if err != nil {
		_, _ = fmt.Fprintln(out, "literalcheck:", err)
		return 1
	}
	var findings []Finding
	for _, f := range files {
		src, err := os.ReadFile(f)
		if err != nil {
			_, _ = fmt.Fprintln(out, "literalcheck:", err)
			return 1
		}
		findings = append(findings, check(f, src)...)
	}
	for _, f := range findings {
		_, _ = fmt.Fprintln(out, f)
	}
	if len(findings) > 0 {
		return 1
	}
	return 0
}
