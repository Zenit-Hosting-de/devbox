package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha512"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/rogpeppe/go-internal/testscript"
)

// fakeMaven serves what the maven plugin downloads: the version list
// (maven-metadata.xml) and the distribution archives with their .sha512 files.
type fakeMaven struct {
	mu       sync.Mutex
	versions []string
	badSum   map[string]bool
	srv      *httptest.Server
}

func mavenArchive(version string) []byte {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	root := "apache-maven-" + version + "/"
	_ = tw.WriteHeader(&tar.Header{Name: root, Typeflag: tar.TypeDir, Mode: 0o755})
	_ = tw.WriteHeader(&tar.Header{Name: root + "bin/", Typeflag: tar.TypeDir, Mode: 0o755})
	script := "#!/bin/sh\necho \"Apache Maven " + version + "\"\n"
	_ = tw.WriteHeader(&tar.Header{Name: root + "bin/mvn", Typeflag: tar.TypeReg, Mode: 0o755, Size: int64(len(script))})
	_, _ = tw.Write([]byte(script))
	_ = tw.Close()
	_ = gz.Close()
	return buf.Bytes()
}

func (f *fakeMaven) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	versions := append([]string{}, f.versions...)
	bad := f.badSum
	f.mu.Unlock()
	path := strings.Trim(r.URL.Path, "/")
	switch {
	case path == "maven2/org/apache/maven/apache-maven/maven-metadata.xml":
		var b strings.Builder
		b.WriteString("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n<metadata>\n  <groupId>org.apache.maven</groupId>\n  <artifactId>apache-maven</artifactId>\n  <versioning>\n    <versions>\n")
		// like the real list: pre-releases and a 4.x line are in it
		b.WriteString("      <version>3.10.0-alpha-1</version>\n      <version>4.0.0-rc-1</version>\n")
		for _, v := range versions {
			b.WriteString("      <version>" + v + "</version>\n")
		}
		b.WriteString("    </versions>\n  </versioning>\n</metadata>\n")
		w.Header().Set("Content-Type", "application/xml")
		_, _ = io.WriteString(w, b.String())
	case strings.HasPrefix(path, "dist/maven/maven-3/"):
		parts := strings.Split(strings.TrimPrefix(path, "dist/maven/maven-3/"), "/")
		if len(parts) != 3 || parts[1] != "binaries" {
			http.NotFound(w, r)
			return
		}
		version, file := parts[0], parts[2]
		known := false
		for _, v := range versions {
			known = known || v == version
		}
		archive := "apache-maven-" + version + "-bin.tar.gz"
		switch {
		case !known:
			http.NotFound(w, r)
		case file == archive:
			data := mavenArchive(version)
			w.Header().Set("Content-Length", strconv.Itoa(len(data)))
			_, _ = w.Write(data)
		case file == archive+".sha512":
			sum := sha512.Sum512(mavenArchive(version))
			text := hex.EncodeToString(sum[:])
			if bad[version] {
				text = strings.Repeat("0", len(text))
			}
			// Like archive.apache.org, only 3.9.8 ends the file with a newline.
			if version == "3.9.8" {
				text += "\n"
			}
			_, _ = io.WriteString(w, text)
		default:
			http.NotFound(w, r)
		}
	default:
		http.NotFound(w, r)
	}
}

var (
	mavenMu      sync.Mutex
	mavenServers = map[string]*fakeMaven{}
)

// cmdMaven is the testscript command
//
//	maven start              start a server; sets $MAVEN_DIST (distUrl) and $MAVEN_META (metadataUrl)
//	maven release VERSION    serve VERSION as a released 3.x version
//	maven badsum VERSION     serve a wrong .sha512 for VERSION
//	maven xml FILE           the file is well-formed XML
//	maven count FILE NAME N  the file has exactly N elements named NAME
func cmdMaven(ts *testscript.TestScript, neg bool, args []string) {
	if neg || len(args) == 0 {
		ts.Fatalf("usage: maven start | release VERSION | badsum VERSION | xml FILE | count FILE NAME N")
	}
	key := ts.Getenv("WORK")
	mavenMu.Lock()
	f, started := mavenServers[key]
	mavenMu.Unlock()
	switch args[0] {
	case "start":
		f = &fakeMaven{badSum: map[string]bool{}}
		f.srv = httptest.NewServer(f)
		mavenMu.Lock()
		mavenServers[key] = f
		mavenMu.Unlock()
		ts.Defer(f.srv.Close)
		ts.Setenv("MAVEN_DIST", f.srv.URL+"/dist/maven/maven-3")
		ts.Setenv("MAVEN_META", f.srv.URL+"/maven2/org/apache/maven/apache-maven/maven-metadata.xml")
	case "release", "badsum":
		if !started || len(args) != 2 {
			ts.Fatalf("maven %s: start the server first and give VERSION", args[0])
		}
		f.mu.Lock()
		if args[0] == "release" {
			f.versions = append(f.versions, args[1])
			sort.Strings(f.versions)
		} else {
			f.badSum[args[1]] = true
		}
		f.mu.Unlock()
	case "xml":
		if len(args) != 2 {
			ts.Fatalf("usage: maven xml FILE")
		}
		if _, err := xmlElements(ts.MkAbs(args[1])); err != nil {
			ts.Fatalf("%s is not well-formed XML: %v", args[1], err)
		}
	case "count":
		if len(args) != 4 {
			ts.Fatalf("usage: maven count FILE NAME N")
		}
		counts, err := xmlElements(ts.MkAbs(args[1]))
		if err != nil {
			ts.Fatalf("%s is not well-formed XML: %v", args[1], err)
		}
		if want := args[3]; strconv.Itoa(counts[args[2]]) != want {
			ts.Fatalf("%s has %d <%s> elements, want %s", args[1], counts[args[2]], args[2], want)
		}
	default:
		ts.Fatalf("maven: unknown command %q", args[0])
	}
}

// xmlElements parses the file strictly and counts its elements by local name.
func xmlElements(path string) (map[string]int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	dec := xml.NewDecoder(bytes.NewReader(data))
	counts := map[string]int{}
	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			return counts, nil
		}
		if err != nil {
			return nil, fmt.Errorf("%w", err)
		}
		if se, ok := tok.(xml.StartElement); ok {
			counts[se.Name.Local]++
		}
	}
}
