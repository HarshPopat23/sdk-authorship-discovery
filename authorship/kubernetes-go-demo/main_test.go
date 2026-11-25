package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTypedIdentification(t *testing.T) {
	source, e := os.ReadFile("fixture/workload.go")
	if e != nil {
		t.Fatal(e)
	}
	cases := []struct {
		name, old, new string
		count          int
	}{
		{"official", "", "", 1},
		{"import_alias", `"k8s.io/client-go/kubernetes"`, `renamed "k8s.io/client-go/kubernetes"`, 1},
		{"variable_name", "clientset", "other", 1},
		{"empty_namespace", `"demo"`, `""`, 0},
		{"unknown_namespace", `Pods("demo")`, `Pods(ctx.Value("namespace").(string))`, 0},
		{"watch_options", "metav1.ListOptions{}", "metav1.ListOptions{Watch:true}", 0},
		{"fake_interface", "*kubernetes.Clientset", "kubernetes.Interface", 0},
		{"local_lookalike", "*kubernetes.Clientset", "*LocalClient", 0},
		{"other_resource", `.Pods("demo")`, `.ConfigMaps("demo")`, 0},
		{"constructor_only", `_, _ = clientset.CoreV1().Pods("demo").List(ctx, metav1.ListOptions{})`, `_ = clientset.CoreV1(); _ = ctx; _ = metav1.ListOptions{}`, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d := t.TempDir()
			for _, f := range []string{"go.mod", "go.sum"} {
				b, e := os.ReadFile(f)
				if e != nil {
					t.Fatal(e)
				}
				if e = os.WriteFile(filepath.Join(d, f), b, 0600); e != nil {
					t.Fatal(e)
				}
			}
			s := string(source)
			if c.old != "" {
				s = strings.ReplaceAll(s, c.old, c.new)
			}
			if c.name == "local_lookalike" {
				s += `
var _ *kubernetes.Clientset
type LocalClient struct{}
func (*LocalClient) CoreV1() *LocalClient { return nil }
func (*LocalClient) Pods(string) *LocalClient { return nil }
func (*LocalClient) List(context.Context, metav1.ListOptions) (int,error) { return 0,nil }
`
			}
			if c.name == "import_alias" {
				s = strings.ReplaceAll(s, "*kubernetes.Clientset", "*renamed.Clientset")
			}
			if e := os.WriteFile(filepath.Join(d, "workload.go"), []byte(s), 0600); e != nil {
				t.Fatal(e)
			}
			calls, root, e := analyze(d)
			if e != nil {
				t.Fatal(e)
			}
			if len(calls) != c.count {
				t.Fatalf("got %d calls, want %d", len(calls), c.count)
			}
			if c.count == 1 {
				p, _, e := endpoint(root)
				if e != nil || p != "/api/v1/namespaces/{namespace}/pods" {
					t.Fatalf("endpoint %s: %v", p, e)
				}
			}
		})
	}
}
func TestEndpointRejectsMissingSource(t *testing.T) {
	if _, _, e := endpoint(t.TempDir()); e == nil {
		t.Fatal("missing source accepted")
	}
}
