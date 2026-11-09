// Package sigstore checks Sigstore bundles with cosign: a file must be signed by an
// expected identity of an OIDC issuer (keyless signing, e.g. from a CI workflow).
package sigstore

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// Timeout bounds one cosign call.
const Timeout = 2 * time.Minute

// maxOutput bounds the text of cosign that goes into an error.
const maxOutput = 1000

// Identity is the signer a bundle must name.
type Identity struct {
	Issuer   string `json:"issuer"`
	Identity string `json:"identity"`
}

// Errors of Verify; callers turn them into their own messages.
var (
	// ErrNoBundle means the bundle file does not exist.
	ErrNoBundle = errors.New("no signature bundle")
	// ErrNoCosign means cosign is not installed (or not in PATH).
	ErrNoCosign = errors.New("cosign is not installed (or not in PATH)")
)

// InvalidError is a bundle cosign refused; Output is what cosign said.
type InvalidError struct{ Output string }

func (e *InvalidError) Error() string { return e.Output }

// Verifier runs cosign.
type Verifier struct {
	// LookPath finds cosign; nil uses the PATH of the process.
	LookPath func(string) (string, error)
	// Getenv reads the environment of the call; cosign gets a few variables of it.
	Getenv func(string) string
}

// verified remembers checks cosign accepted in this process (by the digest of file,
// bundle and identity), so a long-running service does not run cosign again and again.
var verified sync.Map

// Verify checks that bundle is a valid Sigstore bundle for file, signed by id.
func (v Verifier) Verify(ctx context.Context, file, bundle string, id Identity) error {
	data, err := os.ReadFile(file) //nolint:gosec // the file to check, chosen by the caller
	if err != nil {
		return err
	}
	sig, err := os.ReadFile(bundle) //nolint:gosec // the bundle, chosen by the caller
	if errors.Is(err, fs.ErrNotExist) {
		return ErrNoBundle
	}
	if err != nil {
		return err
	}
	h := sha256.New()
	for _, part := range [][]byte{data, sig, []byte(id.Issuer), []byte(id.Identity)} {
		sum := sha256.Sum256(part)
		h.Write(sum[:])
	}
	memo := hex.EncodeToString(h.Sum(nil))
	if _, ok := verified.Load(memo); ok {
		return nil
	}
	look := v.LookPath
	if look == nil {
		look = exec.LookPath
	}
	bin, err := look("cosign")
	if err != nil {
		return ErrNoCosign
	}
	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, "verify-blob", //nolint:gosec // cosign from the PATH of the call, arguments are built here
		"--bundle", bundle,
		"--certificate-identity", id.Identity,
		"--certificate-oidc-issuer", id.Issuer,
		file)
	cmd.Env = v.env()
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(out.String())
		if len(msg) > maxOutput {
			msg = msg[len(msg)-maxOutput:]
		}
		if msg == "" {
			msg = err.Error()
		}
		return &InvalidError{Output: msg}
	}
	verified.Store(memo, true)
	return nil
}

// envPassThrough lists the variables cosign gets: home, proxy, certificates and the
// Sigstore trust root.
var envPassThrough = []string{
	"HOME", "PATH", "TMPDIR", "LANG",
	"HTTP_PROXY", "HTTPS_PROXY", "NO_PROXY", "http_proxy", "https_proxy", "no_proxy",
	"SSL_CERT_FILE", "SSL_CERT_DIR", "TUF_ROOT", "SIGSTORE_ROOT_FILE",
}

func (v Verifier) env() []string {
	getenv := v.Getenv
	if getenv == nil {
		getenv = os.Getenv
	}
	var env []string
	for _, k := range envPassThrough {
		if val := getenv(k); val != "" {
			env = append(env, k+"="+val)
		}
	}
	return env
}
