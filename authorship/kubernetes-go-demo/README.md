# client-go Pod List: a focused SDK-to-OpenAPI demo

This is an isolated research demo, not a production profiler integration or general Kubernetes SDK detector. It reuses the Kubernetes Python investigation's separation of SDK surface evidence from language-neutral service semantics.

## What it demonstrates

`fixture/workload.go` contains an ordinary `*kubernetes.Clientset` call:

```go
clientset.CoreV1().Pods("demo").List(ctx, metav1.ListOptions{})
```

The fixture's initializer panics if executed. The analyser loads and type-checks it with `go/packages`; it never runs or imports its compiled application code. Tests target `.` only, **not** `./...` (which would execute the fixture's panic initializer).

The analyser checks actual `go/types.Selection` receiver types at each chain step, module version and absence of module replacement, a constant non-empty namespace, and empty options of the official `metav1.ListOptions` type. Renamed imports/variables work. Interface-injected clients, other resources, unknown/empty namespaces and non-empty options do not match. It does not equate every `List` function with a Kubernetes call.

## The difficult join, made inspectable

1. Real typed application selections identify `Clientset.CoreV1` → `CoreV1Interface.Pods` → `PodInterface.List`.
2. The **authoring adapter** reads four exact SDK source files from the compiler-resolved client-go module. It verifies implementation fragments: CoreV1 returns the stored client; Pods returns newPods; newPods invokes the generic constructor; the generic List method uses GET, namespace scoping, resource state and options.
3. The resource argument is extracted from the selected constructor (`"pods"`), rather than inferred by pluralizing a kind or assigning meaning to the word List.
4. A deliberately Kubernetes-specific route rule combines the reviewed `/api/v1` namespaced route with that resource. This rule is authored, not discovered by a general-purpose symbolic executor. Its scope is only the pinned client-go release and this public method.
5. The analyser reads **the complete authoritative Swagger** and looks up that path and HTTP method. It obtains `listCoreV1NamespacedPod` from the document; that operation ID is not hardcoded in the analyser.
6. `verify.py` joins the exact endpoint and ID to the existing language-neutral service mapping, copies its Condition template, then validates the profile core and extension schema.

This separation exposes what was automated and what was reviewed. Source-fragment checks are a narrow fail-closed authoring proof, **not** a general data-flow analysis or automatic generation of every SDK mapping. Do not extrapolate to other methods or releases.

## Reproduce

Prerequisites: Go **1.26.5**, Python 3 with venv support, and the explicit dependency setup below. Install the Go programming toolchain; do not assume a system executable named `go` is the compiler.

From `authorship/kubernetes-go-demo`:

```sh
python3 -m venv .venv
.venv/bin/python -m pip install -r requirements.txt
go mod download all
.venv/bin/python setup_inputs.py
```

Acquisition above may use the network. The following phase uses local inputs and disables automatic Go module and toolchain downloads:

```sh
export GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off GOWORK=off
export GOFLAGS=-mod=readonly CGO_ENABLED=0
# Ignore any user-installed packages driver.
export GOPACKAGESDRIVER=off

go test -v .
go vet .
go mod verify
.venv/bin/python verify.py
```

Compiler/type loading may run Go tooling; this is not a promise to execute no subprocesses. Dependencies and compiler must already be available. No Kubernetes cluster, kubeconfig, credentials or workload execution is required.

Outputs:

- `results/trace.json`: selected typed call, inferred endpoint, OpenAPI ID and SDK source evidence used by the authoring proof.
- `results/profile.yaml`: generated portable requirement. It omits the concrete namespace and all credential/cluster values.

The trace's absolute paths are avoided for the application position. SDK source locations and hashes are retained for inspection; the module is covered by `go.sum`/`go mod verify`.

## Pins and ownership

- client-go v0.36.2: https://github.com/kubernetes/client-go/tree/877f5359348b5df85619f2aa379abc5bd74bca2d
- Kubernetes v1.36.2 Swagger: https://github.com/kubernetes/kubernetes/blob/24e2b02af5543d7910c2bb074c7264df5a8f0467/api/openapi-spec/swagger.json
- OpenAPI byte SHA-256: `dcede2063da1d7ad62ecb5af8adb6d7fabd0b52385a7fa0048afb491dac90450`
- Extension and service mapping: https://github.com/runtimeconditions/extensions/tree/f35107c0aaf9dfb16ea0632c6e5a4cc23217d668/catalog/kubernetes/api
- Core schema 0.4.0: https://github.com/runtimeconditions/spec/blob/eb4fad15b2a3546d885dd69b0dd661edbf6ee1af/schema/runtimeconditions.profile.schema.yaml

`inputs.sha256.json` records retained-file hashes; the verifier also checks the independently recorded authoritative OpenAPI byte digest and matching extension semantic identity. These are pinned research inputs, not a new registry or a new maintained service inventory. The existing extension is a local reviewed release; this demo does not assume its URL is publicly downloadable.

## Limits

Only the direct concrete-clientset chain, constant non-empty namespace and empty options are supported. A function parameter is sufficient; this does not prove initialization, execution reachability, runtime transport identity, successful requests or authorization. Static source represents potential demand.

Dynamic clients, CRDs, wrappers, informers, watches, all-namespace access, non-empty options, interface injection and interprocedural flow are excluded. Unsupported input produces no match; this one-call harness reports an error if it cannot identify exactly one supported call. That harness behaviour is not a proposal to change production missing-mapping diagnostics.

The schema permits custom resources: changing `pods` to `configmaps` can remain schema-valid. `verify.py` includes that control to show why an authoritative source join is needed in addition to schema checks. No RBAC or deployment configuration is generated. No existing profiler code is changed.
