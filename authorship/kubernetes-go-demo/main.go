// An isolated, deliberately narrow source-to-operation research demo.
package main

import (
	"crypto/sha256"
	"encoding/json"
	"flag"
	"fmt"
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"golang.org/x/tools/go/packages"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type Call struct{ Position, Receiver, Method, Namespace string }

func named(t types.Type, pkg, name string) bool {
	if p, ok := t.(*types.Pointer); ok {
		t = p.Elem()
	}
	n, ok := t.(*types.Named)
	return ok && n.Obj().Pkg() != nil && n.Obj().Pkg().Path() == pkg && n.Obj().Name() == name
}
func selector(call *ast.CallExpr, info *types.Info, method, pkg, recv string) (*ast.SelectorExpr, bool) {
	s, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return nil, false
	}
	sel := info.Selections[s]
	if sel == nil || sel.Obj().Name() != method || !named(sel.Recv(), pkg, recv) {
		return nil, false
	}
	return s, true
}
func identify(call *ast.CallExpr, info *types.Info, fs *token.FileSet) (Call, bool) {
	s, ok := selector(call, info, "List", "k8s.io/client-go/kubernetes/typed/core/v1", "PodInterface")
	if !ok || len(call.Args) != 2 {
		return Call{}, false
	}
	opts, ok := call.Args[1].(*ast.CompositeLit)
	if !ok || len(opts.Elts) != 0 || !named(info.TypeOf(opts), "k8s.io/apimachinery/pkg/apis/meta/v1", "ListOptions") {
		return Call{}, false
	}
	pods, ok := s.X.(*ast.CallExpr)
	if !ok || len(pods.Args) != 1 {
		return Call{}, false
	}
	ps, ok := selector(pods, info, "Pods", "k8s.io/client-go/kubernetes/typed/core/v1", "CoreV1Interface")
	if !ok {
		return Call{}, false
	}
	core, ok := ps.X.(*ast.CallExpr)
	if !ok || len(core.Args) != 0 {
		return Call{}, false
	}
	cs, ok := selector(core, info, "CoreV1", "k8s.io/client-go/kubernetes", "Clientset")
	if !ok {
		return Call{}, false
	}
	if _, ok := info.TypeOf(cs.X).(*types.Pointer); !ok {
		return Call{}, false
	}
	v := info.Types[pods.Args[0]].Value
	if v == nil || v.Kind() != constant.String {
		return Call{}, false
	}
	ns := constant.StringVal(v)
	if ns == "" {
		return Call{}, false
	}
	return Call{filepath.Base(fs.Position(call.Pos()).Filename) + ":" + strconv.Itoa(fs.Position(call.Pos()).Line), types.TypeString(info.TypeOf(cs.X), nil), "PodInterface.List", ns}, true
}
func analyze(dir string) ([]Call, string, error) {
	cfg := &packages.Config{Dir: dir, Mode: packages.NeedName | packages.NeedFiles | packages.NeedCompiledGoFiles | packages.NeedSyntax | packages.NeedTypes | packages.NeedTypesInfo | packages.NeedImports | packages.NeedDeps | packages.NeedModule}
	pkgs, err := packages.Load(cfg, ".")
	if err != nil {
		return nil, "", err
	}
	if packages.PrintErrors(pkgs) > 0 {
		return nil, "", fmt.Errorf("type loading failed")
	}
	var out []Call
	root := ""
	bad := false
	packages.Visit(pkgs, nil, func(p *packages.Package) {
		if p.Module != nil && p.Module.Path == "k8s.io/client-go" {
			if p.Module.Replace != nil || p.Module.Version != "v0.36.2" {
				bad = true
			}
			root = p.Module.Dir
		}
	})
	if bad || root == "" {
		return nil, "", fmt.Errorf("requires unreplaced client-go v0.36.2")
	}
	for _, p := range pkgs {
		for _, f := range p.Syntax {
			ast.Inspect(f, func(n ast.Node) bool {
				if c, ok := n.(*ast.CallExpr); ok {
					if v, yes := identify(c, p.TypesInfo, p.Fset); yes {
						out = append(out, v)
					}
				}
				return true
			})
		}
	}
	return out, root, nil
}

// Authoring proof: exact pinned SDK implementation, not arbitrary SDK inference.
func endpoint(root string) (string, map[string]string, error) {
	files := map[string]string{}
	for _, p := range []string{"kubernetes/clientset.go", "kubernetes/typed/core/v1/core_client.go", "kubernetes/typed/core/v1/pod.go", "gentype/type.go"} {
		b, e := os.ReadFile(filepath.Join(root, p))
		if e != nil {
			return "", nil, e
		}
		files[p] = string(b)
	}
	checks := map[string][]string{
		"kubernetes/clientset.go":                 {"func (c *Clientset) CoreV1() corev1.CoreV1Interface", "return c.coreV1"},
		"kubernetes/typed/core/v1/core_client.go": {"return newPods(c, namespace)", `config.APIPath = "/api"`},
		"kubernetes/typed/core/v1/pod.go":         {"gentype.NewClientWithListAndApply", `"pods",`},
		"gentype/type.go":                         {"func (l *alsoLister[T, L]) List", "l.client.client.Get()", `NamespaceIfScoped(l.client.namespace, l.client.namespace != "")`, "Resource(l.client.resource)", "VersionedParams(&opts, l.client.parameterCodec)"}}
	for p, frags := range checks {
		for _, s := range frags {
			if !strings.Contains(files[p], s) {
				return "", nil, fmt.Errorf("SDK source evidence changed: %s: %s", p, s)
			}
		}
	}
	// Read the resource argument from the selected generated constructor, not the application method name.
	pod := files["kubernetes/typed/core/v1/pod.go"]
	i := strings.Index(pod, "gentype.NewClientWithListAndApply")
	arg := pod[i:]
	i = strings.Index(arg, "(\n")
	if i < 0 {
		return "", nil, fmt.Errorf("constructor shape changed")
	}
	arg = strings.TrimSpace(arg[i+2:])
	end := strings.Index(arg, ",")
	resource, e := strconv.Unquote(arg[:end])
	if e != nil {
		return "", nil, e
	}
	return "/api/v1/namespaces/{namespace}/" + resource, files, nil
}
func run(dir, swagger, out string) error {
	calls, root, e := analyze(dir)
	if e != nil {
		return e
	}
	if len(calls) != 1 {
		return fmt.Errorf("expected exactly one supported call, found %d", len(calls))
	}
	path, files, e := endpoint(root)
	if e != nil {
		return e
	}
	b, e := os.ReadFile(swagger)
	if e != nil {
		return e
	}
	var doc struct {
		Paths map[string]map[string]json.RawMessage `json:"paths"`
	}
	if e = json.Unmarshal(b, &doc); e != nil {
		return e
	}
	var op struct {
		ID     string `json:"operationId"`
		Action string `json:"x-kubernetes-action"`
	}
	if e = json.Unmarshal(doc.Paths[path]["get"], &op); e != nil {
		return e
	}
	if op.ID == "" || op.Action != "list" {
		return fmt.Errorf("no authoritative list operation")
	}
	trace := map[string]any{"call": calls[0], "endpoint": map[string]string{"path": path, "method": "get"}, "operationId": op.ID, "action": op.Action, "sdkVersion": "v0.36.2", "limitations": "source potential demand, not runtime reachability or successful authorization", "sdkEvidence": sourceEvidence(files)}
	if e = os.MkdirAll(out, 0755); e != nil {
		return e
	}
	b, e = json.MarshalIndent(trace, "", "  ")
	if e != nil {
		return e
	}
	return os.WriteFile(filepath.Join(out, "trace.json"), append(b, '\n'), 0644)
}
func sourceEvidence(files map[string]string) map[string]any {
	out := map[string]any{}
	for path, source := range files {
		out[path] = map[string]string{"sha256": fmt.Sprintf("%x", sha256.Sum256([]byte(source))), "url": "https://github.com/kubernetes/client-go/blob/877f5359348b5df85619f2aa379abc5bd74bca2d/" + path}
	}
	return out
}
func main() {
	dir := flag.String("project", "fixture", "workload directory")
	spec := flag.String("openapi", "inputs/swagger.json", "pinned OpenAPI")
	out := flag.String("out", "results", "output directory")
	flag.Parse()
	if e := run(*dir, *spec, *out); e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}
