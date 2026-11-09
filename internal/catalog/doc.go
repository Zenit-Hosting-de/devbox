// Package catalog searches, installs and updates plugins from catalogs and verifies
// their signatures.
//
// A catalog is a repositories entry of kind catalog whose source is an index.json
// (schema/catalog.v1.json). The index is cached under ~/.cache/devbox/catalog/<name>/.
// Installing a plugin downloads its package, checks the SHA-256 digest of the index and
// the Sigstore bundle with cosign against the trust identity of the catalog, unpacks it
// to ~/.local/share/devbox/catalog/<id>/ and records it in plugins.lock.json. The caller
// confirms the declared permissions before Commit; New lists those an installed version
// did not declare.
package catalog
