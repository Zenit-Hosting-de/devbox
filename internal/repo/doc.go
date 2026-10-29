// Package repo manages plugin repositories (kind "plugins" of the "repositories"
// key, docs/concept.md, "Eigene Repositories"): it resolves the entries of the
// configuration layers, fetches a repository from Git, as HTTPS tarball or from a
// local directory into a snapshot per revision, lists the plugins with the
// permissions they request, keeps a pending revision until the user approves it, and
// reports the plugin directories of the active revisions to the plugin search.
//
// Everything a repository stores lives in ${xdg:data}/devbox/repositories/<name>/:
// one directory per revision and state.json (schema/repository-state.v1.json).
package repo
