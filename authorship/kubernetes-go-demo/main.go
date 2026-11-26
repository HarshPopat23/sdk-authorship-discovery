// An isolated consumer for a compact, experimental subset of SDK mapping rules.
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"flag"
	"fmt"
	"go/ast"
	"go/constant"
	"go/types"
	"io"
	"os"
	"path/filepath"
	"strconv"

	"golang.org/x/tools/go/packages"
	"gopkg.in/yaml.v3"
)

type TypeRef struct {
	Package string `yaml:"package"`
	Name    string `yaml:"name"`
	Pointer bool   `yaml:"pointer,omitempty"`
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
type Match struct {
	Method Method `yaml:"method"`
}
type Produces struct {
	Surface string `yaml:"surface"`
}
type Write struct {
	Target string `yaml:"target"`
	Value  any    `yaml:"value"`
}
type Condition struct {
	Writes []Write `yaml:"writes"`
}
type Rule struct {
	ID        string     `yaml:"id"`
	Match     Match      `yaml:"match"`
	Produces  *Produces  `yaml:"produces,omitempty"`
	Condition *Condition `yaml:"condition,omitempty"`
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

// Exact tested SDK coordinate belongs to the local validation selection, not a second dependency lock.
const validatedVersion = "v0.36.2"

func readMapping(path string) (Mapping, []byte, error) {
	b, e := os.ReadFile(path)
	if e != nil {
		return Mapping{}, nil, e
	}
	var m Mapping
	d := yaml.NewDecoder(bytes.NewReader(b))
	d.KnownFields(true)
	if e = d.Decode(&m); e != nil {
		return m, nil, e
	}
	var extra any
	if e = d.Decode(&extra); e != io.EOF {
		return m, nil, fmt.Errorf("expected one mapping document")
	}
	return m, b, validateMapping(m)
}
func validateMapping(m Mapping) error {
	if m.APIVersion != "runtimeconditions.io/sdk-mapping/v1alpha1" || m.Kind != "RuntimeConditionsSDKMapping" || m.Metadata.Name == "" || m.Metadata.Version == "" || m.SDK.Ecosystem != "go" || m.SDK.Language != "go" || m.SDK.Package == "" || m.Extension.ID == "" {
		return fmt.Errorf("invalid mapping metadata")
	}
	ids, surfaces, selectors := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, r := range m.Rules {
		x := r.Match.Method
		if r.ID == "" || ids[r.ID] || x.Receiver.Package == "" || x.Receiver.Name == "" || x.Name == "" || x.ArgumentCount < 0 {
			return fmt.Errorf("invalid or duplicate rule %q", r.ID)
		}
		ids[r.ID] = true
		if (r.Produces == nil) == (r.Condition == nil) {
			return fmt.Errorf("rule %s needs exactly one output", r.ID)
		}
		if x.Root {
			if x.ReceiverSurface != "" || r.Produces == nil {
				return fmt.Errorf("invalid root %s", r.ID)
			}
		} else if x.ReceiverSurface == "" {
			return fmt.Errorf("missing receiver surface %s", r.ID)
		}
		key := fmt.Sprintf("%v/%s/%s", x.Receiver, x.Name, x.ReceiverSurface)
		if selectors[key] {
			return fmt.Errorf("ambiguous selector %s", r.ID)
		}
		selectors[key] = true
		seen := map[int]bool{}
		for _, a := range x.Arguments {
			if a.Position < 0 || a.Position >= x.ArgumentCount || seen[a.Position] {
				return fmt.Errorf("invalid argument position %s", r.ID)
			}
			seen[a.Position] = true
			switch a.Kind {
			case "nonEmptyString":
				if a.Type != nil {
					return fmt.Errorf("unexpected argument type")
				}
			case "emptyComposite":
				if a.Type == nil || a.Type.Name == "" || a.Type.Package == "" {
					return fmt.Errorf("missing argument type")
				}
			default:
				return fmt.Errorf("unsupported argument constraint %q", a.Kind)
			}
		}
		if r.Produces != nil {
			s := r.Produces.Surface
			if s == "" || surfaces[s] {
				return fmt.Errorf("invalid produced surface %s", r.ID)
			}
			surfaces[s] = true
		}
		if r.Condition != nil {
			if _, e := applyWrites(r.Condition.Writes); e != nil {
				return fmt.Errorf("%s: %w", r.ID, e)
			}
		}
	}
	for _, r := range m.Rules {
		if s := r.Match.Method.ReceiverSurface; s != "" && !surfaces[s] {
			return fmt.Errorf("unknown surface %s", s)
		}
	}
	return nil
}

// Only the constant-write subset needed by this pilot; extension validity is checked by verify.py.
func applyWrites(writes []Write) (map[string]any, error) {
	out := map[string]any{}
	iface := map[string]any{}
	seen := map[string]bool{}
	var ops []any
	for _, w := range writes {
		switch w.Target {
		case "kind", "interface.type":
			v, ok := w.Value.(string)
			if !ok || v == "" || seen[w.Target] {
				return nil, fmt.Errorf("invalid scalar write %s", w.Target)
			}
			seen[w.Target] = true
			if w.Target == "kind" {
				out["kind"] = v
			} else {
				iface["type"] = v
			}
		case "interface.operations[]":
			v, ok := w.Value.(map[string]any)
			if !ok || len(v) == 0 {
				return nil, fmt.Errorf("expected operation object")
			}
			ops = append(ops, v)
		default:
			return nil, fmt.Errorf("unsupported write target %s", w.Target)
		}
	}
	if !seen["kind"] || !seen["interface.type"] || len(ops) == 0 {
		return nil, fmt.Errorf("incomplete condition writes")
	}
	iface["operations"] = ops
	out["interface"] = iface
	return out, nil
}
func matchesType(t types.Type, r TypeRef) bool {
	if r.Pointer {
		p, ok := t.(*types.Pointer)
		if !ok {
			return false
		}
		t = p.Elem()
	} else if _, ok := t.(*types.Pointer); ok {
		return false
	}
	n, ok := types.Unalias(t).(*types.Named)
	return ok && n.Obj().Pkg() != nil && n.Obj().Pkg().Path() == r.Package && n.Obj().Name() == r.Name
}

type Step struct {
	Rule      string `json:"rule"`
	Selection string `json:"selection"`
	Surface   string `json:"surface,omitempty"`
}
type Call struct {
	Position  string         `json:"position"`
	Rule      string         `json:"rule"`
	Steps     []Step         `json:"steps"`
	Condition map[string]any `json:"condition"`
}
type evaluation struct {
	surface   string
	steps     []Step
	condition map[string]any
	rule      string
}

func evaluate(call *ast.CallExpr, info *types.Info, rules []Rule) (evaluation, bool) {
	s, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return evaluation{}, false
	}
	sel := info.Selections[s]
	if sel == nil {
		return evaluation{}, false
	}
	for _, r := range rules {
		x := r.Match.Method
		if sel.Obj().Name() != x.Name || !matchesType(sel.Recv(), x.Receiver) || len(call.Args) != x.ArgumentCount {
			continue
		}
		good := true
		for _, a := range x.Arguments {
			arg := call.Args[a.Position]
			switch a.Kind {
			case "nonEmptyString":
				v := info.Types[arg].Value
				good = v != nil && v.Kind() == constant.String && constant.StringVal(v) != ""
			case "emptyComposite":
				lit, ok := arg.(*ast.CompositeLit)
				good = ok && len(lit.Elts) == 0 && matchesType(info.TypeOf(lit), *a.Type)
			}
			if !good {
				break
			}
		}
		if !good {
			continue
		}
		var steps []Step
		if !x.Root {
			parent, ok := s.X.(*ast.CallExpr)
			if !ok {
				continue
			}
			prev, ok := evaluate(parent, info, rules)
			if !ok || prev.surface != x.ReceiverSurface {
				continue
			}
			steps = prev.steps
		}
		step := Step{Rule: r.ID, Selection: sel.Obj().(*types.Func).FullName()}
		v := evaluation{rule: r.ID}
		if r.Produces != nil {
			v.surface = r.Produces.Surface
			step.Surface = v.surface
		} else {
			v.condition, _ = applyWrites(r.Condition.Writes)
		}
		v.steps = append(steps, step)
		return v, true
	}
	return evaluation{}, false
}
func analyze(dir string, m Mapping) ([]Call, error) {
	cfg := &packages.Config{Dir: dir, Mode: packages.NeedName | packages.NeedFiles | packages.NeedCompiledGoFiles | packages.NeedSyntax | packages.NeedTypes | packages.NeedTypesInfo | packages.NeedImports | packages.NeedDeps | packages.NeedModule}
	ps, e := packages.Load(cfg, ".")
	if e != nil {
		return nil, e
	}
	if packages.PrintErrors(ps) > 0 {
		return nil, fmt.Errorf("type loading failed")
	}
	bad, found := false, false
	packages.Visit(ps, nil, func(p *packages.Package) {
		if p.Module != nil && p.Module.Path == m.SDK.Package {
			found = true
			if p.Module.Replace != nil || p.Module.Version != validatedVersion {
				bad = true
			}
		}
	})
	if bad {
		return nil, fmt.Errorf("mapping not validated for selected SDK version or replacement")
	}
	out := []Call{}
	if !found {
		return out, nil
	}
	for _, p := range ps {
		for _, f := range p.Syntax {
			ast.Inspect(f, func(n ast.Node) bool {
				if c, ok := n.(*ast.CallExpr); ok {
					if v, yes := evaluate(c, p.TypesInfo, m.Rules); yes && v.condition != nil {
						pos := p.Fset.Position(c.Pos())
						out = append(out, Call{filepath.Base(pos.Filename) + ":" + strconv.Itoa(pos.Line), v.rule, v.steps, v.condition})
					}
				}
				return true
			})
		}
	}
	return out, nil
}
func run(dir, mapping, out string) error {
	m, b, e := readMapping(mapping)
	if e != nil {
		return e
	}
	calls, e := analyze(dir, m)
	if e != nil {
		return e
	}
	conditions := []any{}
	for _, c := range calls {
		conditions = append(conditions, c.Condition)
	}
	profile := map[string]any{"apiVersion": "runtimeconditions.io/v1alpha1", "kind": "RuntimeConditionsProfile", "metadata": map[string]string{"name": "client-go-mapping-records"}, "workload": map[string]string{"uri": "https://example.test/client-go-mapping-records", "version": "dev"}, "extensions": []string{m.Extension.ID}, "conditions": conditions}
	trace := map[string]any{"mappingSha256": fmt.Sprintf("%x", sha256.Sum256(b)), "sdkVersion": validatedVersion, "calls": calls, "limitations": "authored rules; direct chains only; potential demand, not runtime execution"}
	if e = os.MkdirAll(out, 0755); e != nil {
		return e
	}
	for name, v := range map[string]any{"trace.json": trace, "profile.json": profile} {
		b, e := json.MarshalIndent(v, "", "  ")
		if e != nil {
			return e
		}
		if e = os.WriteFile(filepath.Join(out, name), append(b, '\n'), 0644); e != nil {
			return e
		}
	}
	return nil
}
func main() {
	dir := flag.String("project", "fixture", "workload directory")
	mapping := flag.String("mapping", "runtimeconditions.sdk-mapping.yaml", "explicit local mapping")
	out := flag.String("out", "results", "output directory")
	flag.Parse()
	if e := run(*dir, *mapping, *out); e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}
