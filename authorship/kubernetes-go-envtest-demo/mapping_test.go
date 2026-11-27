package main

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func testDocument() Document {
	d := Document{Paths: map[string]map[string]json.RawMessage{}}
	for path, item := range map[string]struct{ action, id string }{
		"/api/v1/namespaces/{namespace}/pods":              {"list", "listCoreV1NamespacedPod"},
		"/api/v1/namespaces/{namespace}/configmaps/{name}": {"get", "readCoreV1NamespacedConfigMap"},
	} {
		op := Operation{ID: item.id, Action: item.action}
		op.GVK.Version = "v1"
		b, _ := json.Marshal(op)
		d.Paths[path] = map[string]json.RawMessage{"get": b}
	}
	return d
}
func testObservation(t *testing.T, i int) Observation {
	t.Helper()
	f := fixtures()[i]
	sdk, e := identity(f.Receiver, f.Getter, f.Method)
	if e != nil {
		t.Fatal(e)
	}
	path := "/api/v1/namespaces/demo/pods"
	if i == 1 {
		path = "/api/v1/namespaces/demo/configmaps/settings"
	}
	return Observation{Case: f.Name, SDK: sdk, Requests: []Request{{Method: "GET", Path: path, Status: 200}}}
}
func TestClassifyControls(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*Observation, *Document)
		bad    bool
	}{
		{"positive", func(*Observation, *Document) {}, false},
		{"unmatched", func(o *Observation, _ *Document) { o.Requests[0].Path = "/not-in-openapi" }, true},
		{"wrong_action", func(_ *Observation, d *Document) {
			op := Operation{ID: "wrong-action", Action: "get"}
			op.GVK.Version = "v1"
			b, _ := json.Marshal(op)
			d.Paths["/api/v1/namespaces/{namespace}/pods"]["get"] = b
		}, true},
		{"wrong_method", func(o *Observation, _ *Document) { o.Requests[0].Method = "POST" }, true},
		{"ambiguous", func(_ *Observation, d *Document) {
			d.Paths["/api/v1/namespaces/{namespace}/{resource}"] = d.Paths["/api/v1/namespaces/{namespace}/pods"]
		}, true},
		{"failed_status", func(o *Observation, _ *Document) { o.Requests[0].Status = 404 }, true},
		{"transport_error", func(o *Observation, _ *Document) { o.Requests[0].TransportError = true }, true},
		{"SDK_error_after_success_status", func(o *Observation, _ *Document) { o.SDKError = true }, true},
		{"watch_or_other_options", func(o *Observation, _ *Document) { o.Requests[0].HasQuery = true }, true},
		{"no_request", func(o *Observation, _ *Document) { o.Requests = nil }, true},
		{"retry_or_fanout", func(o *Observation, _ *Document) { o.Requests = append(o.Requests, o.Requests[0]) }, true},
		{"wrong_SDK_method", func(o *Observation, _ *Document) { o.SDK.Method = "Get" }, true},
		{"wrong_options_type", func(o *Observation, _ *Document) { o.SDK.Options.Package = "example.test/lookalike" }, true},
		{"trailing_slash", func(o *Observation, _ *Document) { o.Requests[0].Path += "/" }, true},
		{"unknown_GVK", func(_ *Observation, d *Document) {
			op := Operation{ID: "example", Action: "list"}
			op.GVK.Version = "v2"
			b, _ := json.Marshal(op)
			d.Paths["/api/v1/namespaces/{namespace}/pods"]["get"] = b
		}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			o := testObservation(t, 0)
			d := testDocument()
			tc.change(&o, &d)
			_, e := classify(d, o)
			if (e != nil) != tc.bad {
				t.Fatalf("unexpected rejection: %v", e)
			}
		})
	}
	get := testObservation(t, 1)
	m, e := classify(testDocument(), get)
	if e != nil || m.Action != "get" || m.Resource != "configmaps" || m.OperationID != "readCoreV1NamespacedConfigMap" {
		t.Fatalf("object Get misclassified: %+v %v", m, e)
	}
}
func TestMappingRecords(t *testing.T) {
	observations := []Observation{testObservation(t, 0), testObservation(t, 1)}
	m, matches, e := generateMapping(observations, testDocument())
	if e != nil {
		t.Fatal(e)
	}
	if len(m.Rules) != 5 || len(matches) != 2 || m.Extension.ID != extensionID {
		t.Fatalf("bad compact mapping: %+v", m)
	}
	for _, i := range []int{0, 1} {
		single, _, e := generateMapping(observations[i:i+1], testDocument())
		if e != nil || len(single.Rules) != 3 {
			t.Fatalf("single observed call: %v", e)
		}
	}
	if _, _, e = generateMapping([]Observation{observations[0], observations[0]}, testDocument()); e == nil {
		t.Fatal("duplicate case published")
	}
	duplicate := observations[0]
	duplicate.Case = "different-test-same-method"
	if _, _, e = generateMapping([]Observation{observations[0], duplicate}, testDocument()); e == nil {
		t.Fatal("duplicate method silently published")
	}
	if _, _, e = generateMapping(nil, testDocument()); e == nil {
		t.Fatal("empty evidence published")
	}
	bad := observations[1]
	bad.SDKError = true
	if _, _, e = generateMapping([]Observation{observations[0], bad}, testDocument()); e == nil {
		t.Fatal("partial mapping published despite failed evidence")
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestRecorderPrivacyAndFailures(t *testing.T) {
	u, _ := url.Parse("https://user:secret@127.0.0.1:6443/api/v1/namespaces/demo/pods?token=secret")
	req := &http.Request{Method: "GET", URL: u, Header: http.Header{"Authorization": []string{"secret"}}}
	for _, fail := range []bool{false, true} {
		calls := 0
		r := recorder{base: roundTripFunc(func(*http.Request) (*http.Response, error) {
			calls++
			if fail {
				return nil, errors.New("sensitive transport details")
			}
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("secret body"))}, nil
		})}
		if len(r.requests) != 0 {
			t.Fatal("pre-request evidence")
		}
		response, _ := r.RoundTrip(req)
		if response != nil {
			_ = response.Body.Close()
		}
		if calls != 1 || len(r.requests) != 1 || !r.requests[0].HasQuery || r.requests[0].TransportError != fail {
			t.Fatal("incomplete capture")
		}
		b, e := json.Marshal(r.requests)
		if e != nil {
			t.Fatal(e)
		}
		for _, private := range []string{"secret", "127.0.0.1", "Authorization", "sensitive"} {
			if strings.Contains(string(b), private) {
				t.Fatalf("private request data recorded: %s", b)
			}
		}
	}
}
func TestPinnedDocumentMissing(t *testing.T) {
	if _, e := loadDocument("does-not-exist.json"); e == nil {
		t.Fatal("missing authoritative input accepted")
	}
}
