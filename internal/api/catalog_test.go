package api

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

const catalogIdentity = "https://ci.example.com/release"

// fakeCosign accepts a bundle that contains the expected identity.
const fakeCosign = `#!/bin/sh
bundle= identity=
while [ $# -gt 1 ]; do
  case "$1" in
    --bundle) bundle=$2; shift ;;
    --certificate-identity) identity=$2; shift ;;
  esac
  shift
done
grep -q "$identity" "$bundle" || { echo "Error: none of the expected identities matched" >&2; exit 1; }
`

// writeCatalog writes index.json with one plugin and its signed package into dir.
func writeCatalog(t *testing.T, dir, id, version, manifest, signer string) {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: id + "/plugin.json", Mode: 0o644, Size: int64(len(manifest)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write([]byte(manifest)); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	name := id + "-" + version + ".tar.gz"
	write(t, filepath.Join(dir, name), buf.String(), 0o600)
	write(t, filepath.Join(dir, name+".sigstore.json"), `{"identity":"`+signer+`"}`, 0o600)
	sum := sha256.Sum256(buf.Bytes())
	writeJSONFile(t, filepath.Join(dir, "index.json"), map[string]any{
		"generatedAt": "2026-11-09T09:00:00Z",
		"plugins": map[string]any{id: map[string]any{"name": "Kubeish", "versions": []any{map[string]any{
			"version": version, "url": name, "digest": "sha256:" + hex.EncodeToString(sum[:]),
			"bundle": name + ".sigstore.json", "publishedAt": "2026-11-09T09:00:00Z",
		}}}},
	})
}

func newCatalogEnv(t *testing.T) (*testEnv, string) {
	e := newTestEnv(t)
	bin := filepath.Join(e.home, "bin")
	write(t, filepath.Join(bin, "cosign"), fakeCosign, 0o700)
	e.getenv["PATH"] = bin + ":" + os.Getenv("PATH")
	dir := filepath.Join(e.home, "catalog-src")
	e.writeUserConfig(map[string]any{"repositories": map[string]any{"team": map[string]any{
		"kind": "catalog", "source": "file://" + filepath.Join(dir, "index.json"),
		"trust": map[string]any{"issuer": "https://issuer.example.com", "identity": catalogIdentity},
	}}})
	return e, dir
}

const kubeish = `{"id": "kubeish", "version": "1.0.0", "name": "Kubeish", "category": "tool",
 "permissions": {"network": ["https://downloads.example.com"]}}`

func TestCatalogInstall(t *testing.T) {
	e, dir := newCatalogEnv(t)
	writeCatalog(t, dir, "kubeish", "1.0.0", kubeish, catalogIdentity)

	search := e.get("/catalog/search?q=kube")
	e.wantStatus(search, http.StatusOK)
	got := search.json(t)
	hits, _ := got["plugins"].([]any)
	if len(hits) != 1 || hits[0].(map[string]any)["version"] != "1.0.0" {
		t.Fatalf("search = %v", got)
	}

	// without confirm: the permissions come back, nothing is installed
	plan := e.mutate(http.MethodPost, "/catalog/install", map[string]any{"id": "kubeish"}, http.StatusOK).json(t)
	if plan["installed"] != false || len(plan["newPermissions"].([]any)) != 1 {
		t.Fatalf("plan = %v", plan)
	}
	e.wantStatus(e.get("/plugins/kubeish"), http.StatusNotFound)

	done := e.mutate(http.MethodPost, "/catalog/install", map[string]any{"id": "kubeish", "confirm": true}, http.StatusOK).json(t)
	if done["installed"] != true || done["signedBy"] != catalogIdentity {
		t.Fatalf("install = %v", done)
	}
	e.wantBroadcast([]string{"kubeish"}, nil)
	p := e.get("/plugins/kubeish").json(t)
	if origin, _ := p["origin"].(map[string]any); origin["kind"] != "catalog" || origin["repository"] != "team" || origin["revision"] != "1.0.0" {
		t.Errorf("origin = %v", p["origin"])
	}
	e.wantProblem(e.request(http.MethodPost, "/catalog/install", map[string]any{"id": "kubeish", "confirm": true}), http.StatusConflict, codeConflict)

	e.mutate(http.MethodDelete, "/plugins/kubeish", nil, http.StatusNoContent)
	e.wantStatus(e.get("/plugins/kubeish"), http.StatusNotFound)
	e.wantProblem(e.request(http.MethodDelete, "/plugins/kubeish", nil), http.StatusNotFound, codeNotFound)
	e.wantProblem(e.request(http.MethodDelete, "/plugins/demo", nil), http.StatusConflict, codeConflict)
}

func TestCatalogInstallBadSignature(t *testing.T) {
	e, dir := newCatalogEnv(t)
	writeCatalog(t, dir, "kubeish", "1.0.0", kubeish, "https://ci.example.com/other")
	p := e.wantProblem(e.request(http.MethodPost, "/catalog/install", map[string]any{"id": "kubeish", "confirm": true}), http.StatusUnprocessableEntity, codeValidation)
	if p.Detail == "" {
		t.Error("no detail")
	}
	e.wantStatus(e.get("/plugins/kubeish"), http.StatusNotFound)
	e.wantProblem(e.request(http.MethodPost, "/catalog/install", map[string]any{"id": "nope"}), http.StatusNotFound, codeNotFound)
}
