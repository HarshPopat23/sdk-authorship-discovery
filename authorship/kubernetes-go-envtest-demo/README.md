# Observe client-go calls against a real Kubernetes test API

A focused behavioural authoring experiment for [SDK mapping issue #8](https://github.com/runtimeconditions/sdk-authorship-discovery/issues/8). It connects two deliberately invoked SDK methods to their observed requests, joins those requests to authoritative OpenAPI, and emits the compact mapping format already consumed by the merged Go demo.

This is a **client transport observation baseline**, not eBPF wire tracing. It does not reverse-engineer SDK implementation files or introduce service inventories/semantic bridges. Ordinary application profiling remains static: only this isolated authoring test executes its SDK fixtures.

## The proof

[observe.go](observe.go) contains the two ordinary, typed SDK calls:

```go
clientset.CoreV1().Pods("demo").List(ctx, metav1.ListOptions{})
clientset.CoreV1().ConfigMaps("demo").Get(ctx, "settings", metav1.GetOptions{})
```

| Controlled SDK invocation | Observed HTTP request | OpenAPI operation | Condition operation |
| --- | --- | --- | --- |
| `PodInterface.List` | `GET /api/v1/namespaces/demo/pods` | `listCoreV1NamespacedPod` | `list`, `pods`, `namespaced` |
| `ConfigMapInterface.Get` | `GET /api/v1/namespaces/demo/configmaps/settings` | `readCoreV1NamespacedConfigMap` | `get`, `configmaps`, `namespaced` |

All operation objects use core group `""` and API version `v1`. Namespace/object values are fixture inputs, not fields added to the existing extension. The generated Profile omits those concrete values.

1. Start **real envtest binaries**: Kubernetes API server and etcd. No fake client, existing cluster or skip fallback is used. Check the actual server version is `v1.36.2`.
2. Use an **unmeasured setup client** to create namespace `demo` and ConfigMap `settings` with a recognizable fixture value. No Pods need to run; Pod List returns the empty list in this fresh namespace.
3. For each fixture, build a new measured client. `rest.Config.WrapTransport` captures request method, escaped path, response status, query presence and transport-failure presence. The normal authenticated HTTPS request still reaches the real API server.
4. Correlate the isolated invocation with its one request. Each fixture has a deliberate case label and explicit SDK interface/getter/method registration. Reflection verifies the public signature/getter relationship; it does **not** automatically discover which method arbitrary application code invoked. Review the fixture code alongside its label.
5. Match HTTP method and concrete path against the complete pinned OpenAPI document. Require exactly one route, the supported core-v1 namespaced shape, and a signature/action consistent with List or Get. GET alone cannot distinguish listing from object retrieval.
6. Generate two terminal records plus the three receiver-producer records required by the existing static consumer. The receiver-chain/empty-options skeleton is an authored recipe; operation ID/action/resource come from the observed route joined to OpenAPI, not a per-operation endpoint table.
7. Separately invoke a missing ConfigMap Get. Retain the real HTTP 404 as negative evidence and reject it for mapping generation.
8. Validate the generated Profile with core/extension schemas and replay the mapping through the **unchanged merged static Go consumer** against its existing fixture. The fixture's panic initializer is not executed.

## Outputs

- [results/observations.json](results/observations.json): two successful observations, their OpenAPI matches, exact selected versions, and the separately rejected 404 call.
- [results/runtimeconditions.sdk-mapping.yaml](results/runtimeconditions.sdk-mapping.yaml): the generated compact mapping, not a replacement service-description layer.
- [results/profile.yaml](results/profile.yaml): extension-defined Conditions for Pod List and ConfigMap Get.

The observer never records request headers, bodies, credentials, server address/port, raw query values or raw transport/SDK errors. Paths contain only the deliberately chosen fixture namespace/object names. Transient addresses, timestamps, UIDs and resource versions are excluded so repeated live runs produce byte-identical review artifacts. Optional control-plane debug logs are separate, ignored local diagnostics.

## Reproduce

Tested on Linux amd64 with Go **1.26.5** and Python **3.12.14**. A Linux arm64 archive is also pinned, but that architecture was not exercised here. Windows users can use a suitable Linux environment; native Windows execution is not claimed.

From this directory, acquire dependencies and inputs explicitly:

```sh
python3 -m venv .venv
.venv/bin/python -m pip install -r ../kubernetes-go-demo/requirements.txt
go mod download all
(cd ../kubernetes-go-demo && go mod download all)
.venv/bin/python ../kubernetes-go-demo/setup_inputs.py
export KUBEBUILDER_ASSETS="$(.venv/bin/python setup_envtest.py)"
```

`setup_envtest.py` verifies the release archive's SHA-512 before extraction. It prints the assets directory. The authoritative Swagger and immutable core/extension schemas reuse the existing Go demo inputs and their integrity manifest; no duplicated schema release is maintained here.

Run using cached inputs; no generation step invokes acquisition:

```sh
export GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off GOWORK=off
export GOFLAGS='-mod=readonly -buildvcs=false'
export CGO_ENABLED=0 GOPACKAGESDRIVER=off

go test -count=1 .
go test -tags=integration -run TestEnvtest -count=1 -v .
go vet -tags=integration .
go mod verify
.venv/bin/python verify.py
```

`go test .` runs unit controls without starting a server. The integration command **fails** if assets are absent or the live API cannot start; it never silently skips. `verify.py` performs two fresh live runs, checks their snapshots, validates schemas, exercises static replay and rejects a changed authoritative input without replacing prior output. These tests still use local loopback networking; this is not a claim of process-level network isolation.

Regenerate the review outputs deliberately:

```sh
go run . --out results
```

For startup diagnosis only, set `ENVTEST_DEBUG=1`. The control plane advertises/binds loopback explicitly, so it does not need a default host network route. The test stops its control plane before returning; cleanup is also attempted after partial startup and cleanup failures are reported.

## Versions and sources

- [client-go v0.36.2](https://github.com/kubernetes/client-go/tree/v0.36.2), selected with `k8s.io/api` and `k8s.io/apimachinery` v0.36.2 in go.mod/go.sum.
- [controller-runtime v0.24.1](https://github.com/kubernetes-sigs/controller-runtime/tree/v0.24.1), providing envtest lifecycle management.
- [Kubernetes envtest v1.36.2 assets](https://github.com/kubernetes-sigs/controller-tools/releases/tag/envtest-v1.36.2); SHA-512 values in setup_envtest.py come from the upstream [release index](https://github.com/kubernetes-sigs/controller-tools/blob/HEAD/envtest-releases.yaml).
- [Kubernetes v1.36.2 OpenAPI source](https://github.com/kubernetes/kubernetes/blob/24e2b02af5543d7910c2bb074c7264df5a8f0467/api/openapi-spec/swagger.json), SHA-256 `dcede2063da1d7ad62ecb5af8adb6d7fabd0b52385a7fa0048afb491dac90450`.
- [Existing extension and core pins](../kubernetes-go-demo/inputs.sha256.json), reused without modification.
- [Sveltos envtest helper](https://github.com/projectsveltos/addon-controller/blob/main/internal/test/helpers/testenv.go), a reference for the environment choice, not a dependency.

## Boundaries and next experiment

This proves two controlled methods with empty options and known input values. Fixtures, case labels, receiver-chain recipes and extension writes are still authored. It does not enumerate every SDK method, automatically attribute arbitrary concurrent calls, infer all branches/options, or prove SDK-wide compatibility.

Exactly one successful HTTP 200 request is required per successful SDK fixture. Unknown/ambiguous routes, SDK/transport errors, non-empty queries, retries/fan-out and unsupported operations fail closed. A failed request may still carry useful endpoint evidence, but this baseline deliberately does not publish a mapping from it. This does not let the static profiler predict failed requests: a record learned from a successful Get can still describe potential demand for another Get whose object is missing. Unit tests exercise these boundaries with independent positive controls; the missing-object 404 is also tested live.

Dynamic clients, CRDs, watches, streaming, wrappers and production profiler integration remain out of scope. The copied receiver-matching contract accepts non-empty static namespace/name values; this generalization follows the OpenAPI route/extension vocabulary, not exhaustive runtime testing of all inputs. API calls succeeding here do not establish production authorization, connectivity or reachability.

A subsequent eBPF experiment can compare whether it recovers these same method/path observations and correlation under HTTPS. This baseline does not establish TLS visibility or make any eBPF capability claim. Microcks feasibility is independent of this local Kubernetes proof.
