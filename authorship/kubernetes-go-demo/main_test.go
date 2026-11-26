package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func mapping(t *testing.T) Mapping {
	t.Helper()
	m, _, err := readMapping("runtimeconditions.sdk-mapping.yaml")
	if err != nil {
		t.Fatal(err)
	}
	return m
}
func workload(t *testing.T, source string) string {
	t.Helper()
	d := t.TempDir()
	for _, name := range []string{"go.mod", "go.sum"} {
		b, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(filepath.Join(d, name), b, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(d, "workload.go"), []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	return d
}
func TestTypedIdentification(t *testing.T) {
	source, err := os.ReadFile("fixture/workload.go")
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name, old, new string
		count          int
	}{
		{"official", "", "", 3},
		{"producers_only", "", "", 0},
		{"import_alias", `"k8s.io/client-go/kubernetes"`, `renamed "k8s.io/client-go/kubernetes"`, 3},
		{"variable_name", "clientset", "other", 3},
		{"empty_namespace", `"demo"`, `""`, 0},
		{"unknown_namespace", `"demo"`, `ctx.Value("namespace").(string)`, 0},
		{"nonempty_list_options", "metav1.ListOptions{}", "metav1.ListOptions{Watch:true}", 1},
		{"nonempty_get_options", "metav1.GetOptions{}", `metav1.GetOptions{ResourceVersion:"1"}`, 2},
		{"empty_object_name", `"settings"`, `""`, 2},
		{"interface_root", "*kubernetes.Clientset", "kubernetes.Interface", 0},
		{"local_lookalike", "*kubernetes.Clientset", "*LocalClient", 0},
		{"unmapped_resource", `.Pods("demo")`, `.Secrets("demo")`, 2},
		{"unmapped_method", `.Get(ctx, "settings", metav1.GetOptions{})`, `.Watch(ctx, metav1.ListOptions{})`, 2},
		{"variable_receiver", `_, _ = clientset.CoreV1().Pods("demo").List(ctx, metav1.ListOptions{})`, `pods := clientset.CoreV1().Pods("demo"); _, _ = pods.List(ctx, metav1.ListOptions{})`, 2},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := string(source)
			if c.name == "producers_only" {
				s = strings.Split(s, "func Inspect")[0] + `func Inspect(ctx context.Context, clientset *kubernetes.Clientset) {
_ = clientset.CoreV1().Pods("demo")
_ = clientset.CoreV1().ConfigMaps("demo")
_ = ctx
_ = metav1.ListOptions{}
}`
			}
			if c.old != "" {
				s = strings.ReplaceAll(s, c.old, c.new)
			}
			if c.name == "import_alias" {
				s = strings.ReplaceAll(s, "*kubernetes.Clientset", "*renamed.Clientset")
			}
			if c.name == "local_lookalike" {
				s += `
var _ *kubernetes.Clientset
type LocalClient struct{}
func (*LocalClient) CoreV1() *LocalClient {return nil}
func (*LocalClient) Pods(string) *LocalClient {return nil}
func (*LocalClient) ConfigMaps(string) *LocalClient {return nil}
func (*LocalClient) List(context.Context, metav1.ListOptions) (int,error) {return 0,nil}
func (*LocalClient) Get(context.Context,string,metav1.GetOptions) (int,error) {return 0,nil}
`
			}
			calls, err := analyze(workload(t, s), mapping(t))
			if err != nil {
				t.Fatal(err)
			}
			if len(calls) != c.count {
				t.Fatalf("got %d calls, want %d", len(calls), c.count)
			}
		})
	}
}
func TestRecordsControlCoverage(t *testing.T) {
	// Every terminal can be removed and restored without changing the analyser.
	for _, id := range []string{"pods-list", "configmaps-list", "configmaps-get"} {
		t.Run(id, func(t *testing.T) {
			m := mapping(t)
			var removed Rule
			var rules []Rule
			for _, r := range m.Rules {
				if r.ID == id {
					removed = r
				} else {
					rules = append(rules, r)
				}
			}
			m.Rules = rules
			if err := validateMapping(m); err != nil {
				t.Fatal(err)
			}
			calls, err := analyze("fixture", m)
			if err != nil {
				t.Fatal(err)
			}
			if len(calls) != 2 {
				t.Fatalf("removal got %d calls", len(calls))
			}
			for _, c := range calls {
				if c.Rule == id {
					t.Fatal("removed record still matched")
				}
			}
			m.Rules = append(m.Rules, removed)
			calls, err = analyze("fixture", m)
			if err != nil {
				t.Fatal(err)
			}
			if len(calls) != 3 {
				t.Fatalf("restoration got %d calls", len(calls))
			}
		})
	}
}
func TestInvalidMappings(t *testing.T) {
	cases := []struct {
		name string
		edit func(*Mapping)
	}{
		{"metadata", func(m *Mapping) { m.SDK.Language = "python" }},
		{"duplicate_id", func(m *Mapping) { m.Rules = append(m.Rules, m.Rules[0]) }},
		{"ambiguous_selector", func(m *Mapping) { r := m.Rules[2]; r.ID = "duplicate-selector"; m.Rules = append(m.Rules, r) }},
		{"unknown_surface", func(m *Mapping) { m.Rules[2].Match.Method.ReceiverSurface = "missing" }},
		{"missing_argument_type", func(m *Mapping) { m.Rules[2].Match.Method.Arguments[0].Type = nil }},
		{"invalid_position", func(m *Mapping) { m.Rules[2].Match.Method.Arguments[0].Position = 99 }},
		{"unsupported_constraint", func(m *Mapping) { m.Rules[2].Match.Method.Arguments[0].Kind = "execute" }},
		{"unknown_target", func(m *Mapping) { m.Rules[2].Condition.Writes[0].Target = "arbitrary.path" }},
		{"incomplete_writes", func(m *Mapping) { m.Rules[2].Condition.Writes = m.Rules[2].Condition.Writes[:1] }},
		{"two_outputs", func(m *Mapping) { m.Rules[2].Produces = &Produces{Surface: "extra"} }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := mapping(t)
			c.edit(&m)
			if validateMapping(m) == nil {
				t.Fatal("invalid mapping accepted")
			}
		})
	}
	b, err := os.ReadFile("runtimeconditions.sdk-mapping.yaml")
	if err != nil {
		t.Fatal(err)
	}
	for _, extra := range []string{"\nunknownField: true\n", "\n---\nkind: another\n"} {
		f := filepath.Join(t.TempDir(), "invalid.yaml")
		if err = os.WriteFile(f, append(b, []byte(extra)...), 0600); err != nil {
			t.Fatal(err)
		}
		if _, _, err = readMapping(f); err == nil {
			t.Fatal("invalid YAML document accepted")
		}
	}
}
func TestMissingMappingFailsBeforeOutputs(t *testing.T) {
	out := filepath.Join(t.TempDir(), "out")
	if err := run("fixture", "does-not-exist.yaml", out); err == nil {
		t.Fatal("missing explicit mapping accepted")
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Fatal("failed mapping wrote output")
	}
}

func TestReplacedSDKRejected(t *testing.T) {
	source, err := os.ReadFile("fixture/workload.go")
	if err != nil {
		t.Fatal(err)
	}
	d := workload(t, string(source))
	data, err := os.ReadFile(filepath.Join(d, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	// Use the already verified cached SDK source, so type checking still succeeds.
	cache, err := exec.Command("go", "env", "GOMODCACHE").Output()
	if err != nil {
		t.Fatal(err)
	}
	replacement := filepath.Join(strings.TrimSpace(string(cache)), "k8s.io/client-go@v0.36.2")
	data = append(data, []byte("\nreplace k8s.io/client-go => "+strconv.Quote(replacement)+"\n")...)
	if err = os.WriteFile(filepath.Join(d, "go.mod"), data, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = analyze(d, mapping(t)); err == nil || !strings.Contains(err.Error(), "replacement") {
		t.Fatalf("expected explicit SDK selection rejection, got %v", err)
	}
}
