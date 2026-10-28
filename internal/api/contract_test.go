package api

import (
	"encoding/json"
	"mime"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"

	apifs "github.com/Zenit-Hosting-de/devbox/api"
	"github.com/Zenit-Hosting-de/devbox/internal/schema"
)

// The contract helper finds the operation of every response in api/openapi.json by
// method and path template, picks the response for the status (or default), checks
// the content type and the documented headers and validates the JSON body against the
// component it refers to. The coverage test fails when an operation was never
// exercised by a validated response.

const (
	componentPrefix = "#/components/schemas/"
	responsePrefix  = "#/components/responses/"
)

// pending lists operations the tests do not exercise yet, with the work package that
// does.
var pending = map[string]string{
	"GET /ws":                        "AP-10",
	"PUT /credentials/{name}/secret": "later",
}

var (
	openapiOnce sync.Once
	openapiDoc  map[string]any
	validator   *schema.Validator

	coverMu sync.Mutex
	covered = map[string]bool{}
)

func contract(t *testing.T) (map[string]any, *schema.Validator) {
	t.Helper()
	openapiOnce.Do(func() {
		if err := json.Unmarshal(apifs.OpenAPI, &openapiDoc); err != nil {
			t.Fatalf("api/openapi.json: %v", err)
		}
		v, err := schema.New()
		if err != nil {
			t.Fatal(err)
		}
		validator = v
	})
	return openapiDoc, validator
}

func asMap(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

type operation struct {
	method   string
	template string
	def      map[string]any
}

func (o operation) key() string { return o.method + " " + o.template }

// escapedParam matches a path parameter after regexp.QuoteMeta escaped its braces.
var escapedParam = regexp.MustCompile(`\\\{.*?\\\}`)

// findOperation looks the operation up by method and path (relative to the server URL).
func findOperation(doc map[string]any, method, path string) (operation, bool) {
	paths := asMap(doc["paths"])
	templates := make([]string, 0, len(paths))
	for tpl := range paths {
		templates = append(templates, tpl)
	}
	// literal templates first, so /checks/refresh wins over /checks/{id}
	sort.Slice(templates, func(i, j int) bool {
		li, lj := !strings.Contains(templates[i], "{"), !strings.Contains(templates[j], "{")
		if li != lj {
			return li
		}
		return templates[i] < templates[j]
	})
	for _, tpl := range templates {
		re := regexp.MustCompile("^" + escapedParam.ReplaceAllString(regexp.QuoteMeta(tpl), "[^/]+") + "$")
		if !re.MatchString(path) {
			continue
		}
		if op := asMap(asMap(paths[tpl])[strings.ToLower(method)]); op != nil {
			return operation{method: method, template: tpl, def: op}, true
		}
	}
	return operation{}, false
}

// resolveResponse follows a $ref to components/responses.
func resolveResponse(doc map[string]any, r map[string]any) map[string]any {
	if ref, ok := r["$ref"].(string); ok {
		name := strings.TrimPrefix(ref, responsePrefix)
		return asMap(asMap(asMap(doc["components"])["responses"])[name])
	}
	return r
}

// checkContract validates one response against the contract and records the
// operation as covered.
func checkContract(t *testing.T, req *http.Request, res response) {
	t.Helper()
	doc, v := contract(t)
	path := strings.TrimPrefix(req.URL.Path, "/api/v1")
	op, ok := findOperation(doc, req.Method, path)
	if !ok {
		return // not part of the contract, e.g. the UI or an unknown route
	}
	responses := asMap(op.def["responses"])
	raw := asMap(responses[strconv.Itoa(res.code)])
	if raw == nil {
		raw = asMap(responses["default"])
	}
	if raw == nil {
		t.Errorf("%s: no response for %d", op.key(), res.code)
		return
	}
	def := resolveResponse(doc, raw)
	content := asMap(def["content"])
	mediaType, _, _ := mime.ParseMediaType(res.header.Get("Content-Type"))
	if len(content) == 0 {
		if len(res.body) > 0 {
			t.Errorf("%s %d: the contract says there is no body, got %q", op.key(), res.code, res.body)
		}
	} else {
		media := asMap(content[mediaType])
		if media == nil {
			t.Errorf("%s %d: content type %q is not in the contract\n%s", op.key(), res.code, mediaType, res.body)
			return
		}
		if ref, _ := asMap(media["schema"])["$ref"].(string); ref != "" {
			if err := v.ValidateJSON(schema.OpenAPITarget+"."+strings.TrimPrefix(ref, componentPrefix), res.body); err != nil {
				t.Errorf("%s %d: %v\n%s", op.key(), res.code, err, res.body)
			}
		} else if strings.HasSuffix(mediaType, "json") {
			var decoded any
			if err := json.Unmarshal(res.body, &decoded); err != nil {
				t.Errorf("%s %d: %v", op.key(), res.code, err)
			}
		}
	}
	for name, h := range asMap(def["headers"]) {
		if asMap(h)["required"] == true && res.header.Get(name) == "" {
			t.Errorf("%s %d: header %s is missing", op.key(), res.code, name)
		}
	}
	coverMu.Lock()
	covered[op.key()] = true
	coverMu.Unlock()
}

// TestEveryResponseBodyIsARef checks that every JSON response body of the contract
// refers to a component. Bodies that are no data (raw terminal output, an embedded
// schema document) are described inline.
func TestEveryResponseBodyIsARef(t *testing.T) {
	doc, _ := contract(t)
	inline := map[string]bool{"application/octet-stream": true, "application/schema+json": true}
	for tpl, item := range asMap(doc["paths"]) {
		for method, o := range asMap(item) {
			for status, raw := range asMap(asMap(o)["responses"]) {
				resp := resolveResponse(doc, asMap(raw))
				if resp == nil {
					t.Errorf("%s %s %s: unresolved response", method, tpl, status)
					continue
				}
				for mediaType, media := range asMap(resp["content"]) {
					if inline[mediaType] {
						continue
					}
					ref, _ := asMap(asMap(media)["schema"])["$ref"].(string)
					if !strings.HasPrefix(ref, componentPrefix) {
						t.Errorf("%s %s %s %s: the body schema must be a $ref to a component", method, tpl, status, mediaType)
					}
				}
			}
		}
	}
}

// TestEveryOperationHasADefaultProblem and the request header on modifying operations.
func TestOperationRules(t *testing.T) {
	doc, _ := contract(t)
	for tpl, item := range asMap(doc["paths"]) {
		for method, o := range asMap(item) {
			op := asMap(o)
			responses := asMap(op["responses"])
			if ref, _ := asMap(responses["default"])["$ref"].(string); ref != responsePrefix+"Problem" {
				t.Errorf("%s %s: the default response must be Problem", method, tpl)
			}
			if !isMutating(strings.ToUpper(method)) {
				continue
			}
			found := false
			params, _ := op["parameters"].([]any)
			for _, p := range params {
				if ref, _ := asMap(p)["$ref"].(string); ref == "#/components/parameters/DevBoxRequest" {
					found = true
				}
			}
			if !found {
				t.Errorf("%s %s: modifying operations need the DevBoxRequest parameter", method, tpl)
			}
		}
	}
}

func TestFindOperation(t *testing.T) {
	doc, _ := contract(t)
	tests := []struct {
		method, path, want string
	}{
		{"POST", "/checks/refresh", "POST /checks/refresh"},
		{"GET", "/checks/demo.state", "GET /checks/{id}"},
		{"GET", "/sessions/s_01J0000000000000000000/log", "GET /sessions/{id}/log"},
		{"GET", "/plugins/demo/messages/de", "GET /plugins/{id}/messages/{lang}"},
		{"GET", "/settings/demo.mode/explain", "GET /settings/{key}/explain"},
	}
	for _, tt := range tests {
		op, ok := findOperation(doc, tt.method, tt.path)
		if !ok || op.key() != tt.want {
			t.Errorf("%s %s: got %q (%v), want %q", tt.method, tt.path, op.key(), ok, tt.want)
		}
	}
}
