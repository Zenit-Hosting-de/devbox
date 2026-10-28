package api

import (
	"sort"
	"strings"
	"testing"
)

// TestContractCoverage runs last (files run in name order): every operation of
// api/openapi.json must have been answered, and the answer validated against the
// contract, by one of the tests above. Run the whole package; a filtered run skips it.
func TestContractCoverage(t *testing.T) {
	doc, _ := contract(t)
	coverMu.Lock()
	defer coverMu.Unlock()
	if len(covered) == 0 {
		t.Skip("no other test ran; run the whole package")
	}
	var missing []string
	for tpl, item := range asMap(doc["paths"]) {
		for method := range asMap(item) {
			key := strings.ToUpper(method) + " " + tpl
			if covered[key] {
				if reason, ok := pending[key]; ok {
					t.Errorf("%s is tested now: remove it from the pending list (%s)", key, reason)
				}
				continue
			}
			if _, ok := pending[key]; !ok {
				missing = append(missing, key)
			}
		}
	}
	sort.Strings(missing)
	for _, key := range missing {
		t.Errorf("%s was never exercised by a validated test", key)
	}
}
