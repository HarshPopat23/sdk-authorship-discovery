# client-go mapping records: three operations, one consumer

This follow-up to the Pod List proof tests Colin's scaling concern: can another resource and operation be supported through mapping data, without another Kubernetes-specific conditional in the analyser?

The answer demonstrated here is a small, manually authored rule table consumed by reusable Go type and argument checks. It supports **Pod List, ConfigMap List and ConfigMap Get**. This is an isolated research demo, not a production profiler integration or full SDK coverage.

## Start with the input and output

`fixture/workload.go` contains:

```go
clientset.CoreV1().Pods("demo").List(ctx, metav1.ListOptions{})
clientset.CoreV1().ConfigMaps("demo").List(ctx, metav1.ListOptions{})
clientset.CoreV1().ConfigMaps("demo").Get(ctx, "settings", metav1.GetOptions{})
```

The analyser type-checks this source without executing it. Its initializer deliberately panics if run. Tests target `.` only; `go test ./...` would execute that initializer.

| Mapping rule | SDK receiver and method | OpenAPI operation | Condition operation |
| --- | --- | --- | --- |
| `pods-list` | `PodInterface.List` | `listCoreV1NamespacedPod` | `list / pods` |
| `configmaps-list` | `ConfigMapInterface.List` | `listCoreV1NamespacedConfigMap` | `list / configmaps` |
| `configmaps-get` | `ConfigMapInterface.Get` | `readCoreV1NamespacedConfigMap` | `get / configmaps` |

All three operations use `kind: kubernetes.api`, `interface.type: api`, `apiGroup: ""`, `apiVersion: v1` and `scope: namespaced`. Neither the namespace `demo` nor object name `settings` is a field in this extension's operation shape, so neither is emitted.

Outputs committed for review:

- [results/trace.json](results/trace.json): each source position, matched rule ID, actual Go method selections, intermediate receiver surfaces and emitted Condition; mapping byte digest and tested SDK version.
- [results/profile.json](results/profile.json) and [results/profile.yaml](results/profile.yaml): the same profile in both formats, with one Condition per supported call.
- [results/authoring-evidence.json](results/authoring-evidence.json): SDK file hashes, constructor resource, request method/path, OpenAPI ID and expected Condition for each reviewed record.

## What changed

[runtimeconditions.sdk-mapping.yaml](runtimeconditions.sdk-mapping.yaml) holds six rules: a `CoreV1` producer, two resource producers, and three terminal operations. The analyser contains no Pod/ConfigMap/method-specific dispatch. Removing any terminal record removes only its matching Condition; adding it back restores coverage without changing the consumer. `TestRecordsControlCoverage` checks this for all three operations.

This follows the **compact direct-write direction** in [the existing authorship plan](../../docs/planned-sdk-mapping-authorship.md), which supersedes required service inventories and semantic bridges. No service-mapping download or lookup is required. The Go-specific selector and argument forms below are an experimental subset for this pilot, not a finalized shared schema or an implementation of the plan's entire Python/S3 vocabulary. No extra design layer or registry is introduced.

### The small rule contract

- `match.method.receiver`: exact Go package path, named receiver type and pointer requirement, checked against `go/types.Selection.Recv()`. `name` and `argumentCount` must match too. Import aliases and variable names do not affect identity.
- `root: true`: start a direct receiver chain. Here the root is the concrete `(*kubernetes.Clientset).CoreV1` call; it produces a surface and emits no Condition.
- `receiverSurface`: require the immediately nested receiver call to produce this named surface. `produces.surface` labels an intermediate receiver for the next rule. No variable-flow engine or persistent state machine is implemented.
- `arguments`: zero-based positions with `nonEmptyString` (a statically known non-empty Go string constant) or `emptyComposite` (an empty literal of the exact named Go type). Namespace checks guard namespaced routes; empty ListOptions/GetOptions constrain this proof.
- `condition.writes`: constant writes to `kind`, `interface.type`, and append-only `interface.operations[]`. These are the only write targets implemented; arbitrary field paths, argument writes and Condition identity extraction are outside this slice.

Each rule produces a surface **or** a Condition. Unknown YAML fields, multiple documents, duplicate IDs/selectors, missing surfaces, unsupported constraints/targets and incomplete writes are errors. Unsupported application patterns and unmapped methods quietly yield no match. An explicitly missing or structurally invalid mapping fails before writing outputs.

`metadata.version` versions the mapping, independently from the SDK. The demo's validation selection is explicitly limited to client-go **v0.36.2**, with module replacements rejected. That local check is not an application dependency lock or a claim of compatibility with other releases.

The consumer validates the rule structure; `verify.py` validates emitted Condition semantics against the extension and source evidence. The standalone `go run .` command does not itself implement a general extension-schema validator. Run the verification command when changing mapping data.

## Keep authorship evidence separate from consumption

At generation time, `main.go` uses the explicit local mapping and the application's compiler-resolved types. It does not read Swagger, inspect SDK implementation source to discover mappings, invoke acquisition, or execute the application.

At **author verification** time, `verify.py` checks the pinned local SDK source: `Clientset.CoreV1`, the resource getters/constructors, generic client composition, and the generic Get/List request implementations. It extracts `pods`/`configmaps` from the selected constructor and checks GET, namespace scoping, resource state, and `Name(name)` for Get. A reviewed core-v1 route rule yields:

- `GET /api/v1/namespaces/{namespace}/pods`
- `GET /api/v1/namespaces/{namespace}/configmaps`
- `GET /api/v1/namespaces/{namespace}/configmaps/{name}`

Those paths are joined to the complete authoritative Swagger. The operation ID, action and group/version must agree with the authored writes and generated result. These are narrow source-fragment checks and reviewed route composition, **not** symbolic execution or an automatic SDK mapping generator. They may fail after a harmless SDK refactor and intentionally require review then.

Schema validity alone cannot establish this join: the extension permits custom resources. The verifier includes a schema-valid wrong-resource record that must fail the authoring check, as well as five schema negative controls.

## Reproduce

Prerequisites: Go **1.26.5** and Python 3 with venv support. Use the Go programming toolchain. From `authorship/kubernetes-go-demo`:

```sh
python3 -m venv .venv
.venv/bin/python -m pip install -r requirements.txt
go mod download all
.venv/bin/python setup_inputs.py
```

That is the explicit network-enabled acquisition phase. The generation/verification phase uses the already-cached toolchain, modules and inputs:

```sh
export GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off GOWORK=off
export GOFLAGS=-mod=readonly CGO_ENABLED=0 GOPACKAGESDRIVER=off

go test -count=1 -v .
go vet .
go mod verify
.venv/bin/python verify.py
```

`verify.py` runs the consumer twice and requires byte-identical JSON output. It writes the YAML view and authoring evidence after source and schema checks. To run only the consumer with another explicit mapping:

```sh
go run . --project fixture --mapping runtimeconditions.sdk-mapping.yaml --out results
```

Go tooling runs as a subprocess for type loading; no cluster, kubeconfig, credentials or application execution is needed. Environment controls disable automatic Go downloads; they are not process-level network isolation.

## Pins and limits

- [client-go v0.36.2](https://github.com/kubernetes/client-go/tree/877f5359348b5df85619f2aa379abc5bd74bca2d).
- [Kubernetes v1.36.2 Swagger](https://github.com/kubernetes/kubernetes/blob/24e2b02af5543d7910c2bb074c7264df5a8f0467/api/openapi-spec/swagger.json), SHA-256 `dcede2063da1d7ad62ecb5af8adb6d7fabd0b52385a7fa0048afb491dac90450`.
- [Kubernetes extension 0.1.0 source](https://github.com/runtimeconditions/extensions/tree/f35107c0aaf9dfb16ea0632c6e5a4cc23217d668/catalog/kubernetes/api).
- [Core schema 0.4.0](https://github.com/runtimeconditions/spec/blob/eb4fad15b2a3546d885dd69b0dd661edbf6ee1af/schema/runtimeconditions.profile.schema.yaml).

`inputs.sha256.json` pins retained schemas and the acquired Swagger. Source hashes in the optional evidence output support review; they are not another required maintained inventory. The extension is a reviewed local release; its ID is not assumed to be a downloadable URL.

Only direct concrete-clientset chains, constant non-empty namespace/object names and empty options are supported. Variable receivers, wrappers, interface injection, dynamic clients, CRDs, watches, all-namespace access, non-empty options and interprocedural analysis remain out of scope. Static calls represent potential demand; this does not prove reachability, request success, transport identity or authorization. Each matching source call emits a separate Condition; aggregation is deferred.

This proves data-driven coverage across two resources and two operation kinds. It does **not** prove full-SDK scalability. Automated authoring, versioned publication, broader generic analysis and production profiler integration remain future work.
