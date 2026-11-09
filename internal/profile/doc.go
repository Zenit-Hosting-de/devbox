// Package profile fetches profiles from files, HTTPS or Git, verifies their Sigstore
// signatures and caches remote profiles.
//
// A profile source (the entries of "profiles" in config.json) is a local file
// (file://), a JSON file on a web server (https://) or a file in a Git repository
// (git+https://, git+ssh://) with ref and path. Remote profiles are fetched into
// ~/.cache/devbox/profiles/<key>/profile.json; loading the configuration reads the
// cached copy and fetches only when there is none, `devbox profile update` fetches
// again. With trust, a source needs a Sigstore bundle next to the profile
// (<profile>.sigstore.json) signed by the given identity; cosign checks it.
package profile
