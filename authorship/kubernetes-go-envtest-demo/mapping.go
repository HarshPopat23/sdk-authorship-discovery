package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"sort"
	"strings"
)

const swaggerSHA256 = "dcede2063da1d7ad62ecb5af8adb6d7fabd0b52385a7fa0048afb491dac90450"
const extensionID = "https://runtimeconditions.io/extensions/kubernetes/api/0.1.0/runtimeconditions.extension.yaml"
const corePackage = "k8s.io/client-go/kubernetes/typed/core/v1"
const metaPackage = "k8s.io/apimachinery/pkg/apis/meta/v1"

type Request struct {
	Method         string `json:"method"`
	Path           string `json:"path"`
	HasQuery       bool   `json:"hasQuery,omitempty"`
	Status         int    `json:"status"`
	TransportError bool   `json:"transportError,omitempty"`
}
type Identity struct {
	Receiver      TypeRef `json:"receiver"`
	Method        string  `json:"method"`
	ArgumentCount int     `json:"argumentCount"`
	Options       TypeRef `json:"options"`
	Getter        string  `json:"getter"`
}
type Observation struct {
	Case     string    `json:"case"`
	SDK      Identity  `json:"sdk"`
	Requests []Request `json:"requests"`
	SDKError bool      `json:"sdkError"`
}
type Operation struct {
	ID     string `json:"operationId"`
	Action string `json:"x-kubernetes-action"`
	GVK    struct {
		Group   string `json:"group"`
		Version string `json:"version"`
	} `json:"x-kubernetes-group-version-kind"`
}
type Document struct {
	Paths map[string]map[string]json.RawMessage `json:"paths"`
}
type Match struct {
	Path        string `json:"path"`
	Method      string `json:"method"`
	OperationID string `json:"operationId"`
	Action      string `json:"action"`
	Resource    string `json:"resource"`
}

func loadDocument(path string) (Document, error) {
	b, e := os.ReadFile(path)
	if e != nil {
		return Document{}, e
	}
	if fmt.Sprintf("%x", sha256.Sum256(b)) != swaggerSHA256 {
		return Document{}, fmt.Errorf("OpenAPI input does not match pinned Kubernetes 1.36.2 bytes")
	}
	var d Document
	e = json.Unmarshal(b, &d)
	return d, e
}
func matchesPath(template, path string) bool {
	if !strings.HasPrefix(path, "/") || strings.HasSuffix(path, "/") {
		return false
	}
	a, b := strings.Split(strings.TrimPrefix(template, "/"), "/"), strings.Split(strings.TrimPrefix(path, "/"), "/")
	if len(a) != len(b) {
		return false
	}
	for i, v := range a {
		if strings.HasPrefix(v, "{") && strings.HasSuffix(v, "}") {
			if b[i] == "" {
				return false
			}
		} else if v != b[i] {
			return false
		}
	}
	return true
}
func matchRequest(d Document, r Request) (Match, error) {
	if r.TransportError || r.Status != 200 {
		return Match{}, fmt.Errorf("request did not complete successfully")
	}
	if r.HasQuery {
		return Match{}, fmt.Errorf("non-empty query is outside this empty-options proof")
	}
	paths := []string{}
	for path, item := range d.Paths {
		if _, ok := item[strings.ToLower(r.Method)]; ok && matchesPath(path, r.Path) {
			paths = append(paths, path)
		}
	}
	sort.Strings(paths)
	if len(paths) != 1 {
		return Match{}, fmt.Errorf("expected one OpenAPI match, got %d for %s %s", len(paths), r.Method, r.Path)
	}
	path := paths[0]
	var op Operation
	if e := json.Unmarshal(d.Paths[path][strings.ToLower(r.Method)], &op); e != nil {
		return Match{}, e
	}
	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	if r.Method != "GET" || len(parts) < 5 || len(parts) > 6 || strings.Join(parts[:4], "/") != "api/v1/namespaces/{namespace}" || strings.ContainsAny(parts[4], "{}") || op.ID == "" || op.GVK.Group != "" || op.GVK.Version != "v1" {
		return Match{}, fmt.Errorf("unsupported core-v1 namespaced operation")
	}
	if (len(parts) == 5 && op.Action != "list") || (len(parts) == 6 && (parts[5] != "{name}" || op.Action != "get")) {
		return Match{}, fmt.Errorf("unsupported action or object route")
	}
	return Match{path, r.Method, op.ID, op.Action, parts[4]}, nil
}
func classify(d Document, o Observation) (Match, error) {
	if o.SDKError {
		return Match{}, fmt.Errorf("SDK call failed")
	}
	if len(o.Requests) != 1 {
		return Match{}, fmt.Errorf("expected one request for the isolated SDK call, got %d", len(o.Requests))
	}
	m, e := matchRequest(d, o.Requests[0])
	if e != nil {
		return m, e
	}
	expectedOptions := "ListOptions"
	count := 2
	if m.Action == "get" {
		expectedOptions = "GetOptions"
		count = 3
	}
	if o.SDK.Receiver.Package != corePackage || o.SDK.Receiver.Pointer || o.SDK.Getter == "" || o.SDK.Method != map[string]string{"list": "List", "get": "Get"}[m.Action] || o.SDK.ArgumentCount != count || o.SDK.Options != (TypeRef{Package: metaPackage, Name: expectedOptions}) {
		return Match{}, fmt.Errorf("SDK signature and observed operation disagree")
	}
	return m, nil
}

// These are the same compact experimental fields consumed by the merged Go demo.
type TypeRef struct {
	Package string `yaml:"package" json:"package"`
	Name    string `yaml:"name" json:"name"`
	Pointer bool   `yaml:"pointer,omitempty" json:"pointer,omitempty"`
}
type Argument struct {
	Position int      `yaml:"position"`
	Kind     string   `yaml:"kind"`
	Type     *TypeRef `yaml:"type,omitempty"`
}
type Method struct {
	Receiver        TypeRef    `yaml:"receiver"`
	Name            string     `yaml:"name"`
	ReceiverSurface string     `yaml:"receiverSurface,omitempty"`
	Root            bool       `yaml:"root,omitempty"`
	ArgumentCount   int        `yaml:"argumentCount"`
	Arguments       []Argument `yaml:"arguments,omitempty"`
}
type Write struct {
	Target string `yaml:"target"`
	Value  any    `yaml:"value"`
}
type Rule struct {
	ID    string `yaml:"id"`
	Match struct {
		Method Method `yaml:"method"`
	} `yaml:"match"`
	Produces *struct {
		Surface string `yaml:"surface"`
	} `yaml:"produces,omitempty"`
	Condition *struct {
		Writes []Write `yaml:"writes"`
	} `yaml:"condition,omitempty"`
}
type Mapping struct {
	APIVersion string `yaml:"apiVersion"`
	Kind       string `yaml:"kind"`
	Metadata   struct {
		Name    string `yaml:"name"`
		Version string `yaml:"version"`
	} `yaml:"metadata"`
	SDK struct {
		Ecosystem string `yaml:"ecosystem"`
		Package   string `yaml:"package"`
		Language  string `yaml:"language"`
	} `yaml:"sdk"`
	Extension struct {
		ID string `yaml:"id"`
	} `yaml:"extension"`
	Rules []Rule `yaml:"rules"`
}

func producer(id string, method Method, surface string) Rule {
	r := Rule{ID: id}
	r.Match.Method = method
	r.Produces = &struct {
		Surface string `yaml:"surface"`
	}{surface}
	return r
}
func condition(m Match) map[string]any {
	return map[string]any{"kind": "kubernetes.api", "interface": map[string]any{"type": "api", "operations": []any{map[string]any{"verb": m.Action, "apiGroup": "", "apiVersion": "v1", "resource": m.Resource, "scope": "namespaced"}}}}
}
func generateMapping(observations []Observation, d Document) (Mapping, []Match, error) {
	var out Mapping
	out.APIVersion = "runtimeconditions.io/sdk-mapping/v1alpha1"
	out.Kind = "RuntimeConditionsSDKMapping"
	out.Metadata.Name = "client-go.envtest.kubernetes-api"
	out.Metadata.Version = "0.1.0"
	out.SDK.Ecosystem = "go"
	out.SDK.Package = "k8s.io/client-go"
	out.SDK.Language = "go"
	out.Extension.ID = extensionID
	out.Rules = append(out.Rules, producer("core-client", Method{Receiver: TypeRef{Package: "k8s.io/client-go/kubernetes", Name: "Clientset", Pointer: true}, Name: "CoreV1", Root: true}, "core-client"))
	matches := []Match{}
	seen := map[string]bool{}
	producers := map[string]bool{}
	methods := map[string]bool{}
	for _, o := range observations {
		if o.Case == "" || seen[o.Case] {
			return Mapping{}, nil, fmt.Errorf("missing or duplicate case identity")
		}
		seen[o.Case] = true
		m, e := classify(d, o)
		if e != nil {
			return Mapping{}, nil, fmt.Errorf("%s: %w", o.Case, e)
		}
		key := o.SDK.Receiver.Package + "/" + o.SDK.Receiver.Name + "/" + o.SDK.Getter
		methodKey := key + "/" + o.SDK.Method
		if methods[methodKey] {
			return Mapping{}, nil, fmt.Errorf("duplicate SDK method observation")
		}
		methods[methodKey] = true
		surface := strings.ToLower(o.SDK.Getter) + "-client"
		if !producers[key] {
			producers[key] = true
			out.Rules = append(out.Rules, producer(surface, Method{Receiver: TypeRef{Package: corePackage, Name: "CoreV1Interface"}, Name: o.SDK.Getter, ReceiverSurface: "core-client", ArgumentCount: 1, Arguments: []Argument{{Position: 0, Kind: "nonEmptyString"}}}, surface))
		}
		args := []Argument{}
		if m.Action == "get" {
			args = append(args, Argument{Position: 1, Kind: "nonEmptyString"})
		}
		args = append(args, Argument{Position: o.SDK.ArgumentCount - 1, Kind: "emptyComposite", Type: &o.SDK.Options})
		r := Rule{ID: o.Case}
		r.Match.Method = Method{Receiver: o.SDK.Receiver, Name: o.SDK.Method, ReceiverSurface: surface, ArgumentCount: o.SDK.ArgumentCount, Arguments: args}
		op := condition(m)["interface"].(map[string]any)["operations"].([]any)[0]
		r.Condition = &struct {
			Writes []Write `yaml:"writes"`
		}{[]Write{{"kind", "kubernetes.api"}, {"interface.type", "api"}, {"interface.operations[]", op}}}
		out.Rules = append(out.Rules, r)
		matches = append(matches, m)
	}
	if len(observations) == 0 {
		return Mapping{}, nil, fmt.Errorf("no successful observations")
	}
	return out, matches, nil
}
func identity(receiver, getter reflect.Type, method string) (Identity, error) {
	m, ok := receiver.MethodByName(method)
	if !ok || receiver.Kind() != reflect.Interface || getter.Kind() != reflect.Interface || getter.NumMethod() != 1 || m.Type.NumIn() == 0 {
		return Identity{}, fmt.Errorf("unsupported fixture method")
	}
	g := getter.Method(0)
	if g.Type.NumIn() != 1 || g.Type.In(0).Kind() != reflect.String || g.Type.NumOut() != 1 || g.Type.Out(0) != receiver {
		return Identity{}, fmt.Errorf("fixture getter does not return receiver interface")
	}
	options := m.Type.In(m.Type.NumIn() - 1)
	return Identity{Receiver: TypeRef{Package: receiver.PkgPath(), Name: receiver.Name()}, Method: m.Name, ArgumentCount: m.Type.NumIn(), Options: TypeRef{Package: options.PkgPath(), Name: options.Name()}, Getter: g.Name}, nil
}
