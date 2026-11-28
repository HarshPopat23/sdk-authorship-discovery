# Verify client-go operations with real Microcks traces

A focused follow-up to [issue #8](https://github.com/runtimeconditions/sdk-authorship-discovery/issues/8#issuecomment-7226499577) and the merged [envtest baseline](../kubernetes-go-envtest-demo/). This runs two explicitly authored client-go fixtures against **Microcks 1.16.0**, then independently reads Microcks' recorded operation spans. It does not generate mapping tables, change the existing profiler, or implement eBPF.

## What this proves

| Authored SDK invocation | Observed request, excluding Microcks' routing prefix | Authoritative operation ID |
| --- | --- | --- |
| `clientset.CoreV1().Pods("demo").List(ctx, metav1.ListOptions{})` | `GET /api/v1/namespaces/demo/pods` | `listCoreV1NamespacedPod` |
| `clientset.CoreV1().ConfigMaps("demo").Get(ctx, "settings", metav1.GetOptions{})` | `GET /api/v1/namespaces/demo/configmaps/settings` | `readCoreV1NamespacedConfigMap` |

Both examples use empty options. A context deadline bounds calls; setting `rest.Config.Timeout` would add a `timeout` query parameter to Get, so the fixture deliberately avoids it. `demo` and `settings` are mock example inputs, not objects created in a real Kubernetes cluster. Microcks returns fixture responses without storing Kubernetes resources.

[main.go](main.go) sets `rest.Config.Host` to the imported service's `/rest/<service>/v1.36.2` prefix. Its `WrapTransport` clones the request, attaches a fresh cryptographically random 32-hex-character `X-Trace-Id`, and retains only method, escaped path, status and query/transport-error booleans. It never saves headers, credentials, bodies, raw query values or SDK error messages.

[verify.py](verify.py) queries `/api/traces/<traceId>/spans` independently over an administrative HTTP connection. It requires exactly one SDK request and one server operation/invocation span, checks trace isolation, service/version, method and concrete request path, and joins the server's `operation.name` (`GET <path-template>`) to exactly one operation in the authoritative OpenAPI input. **Microcks supplies a method/path operation name, not the Kubernetes `operationId`; the ID and Kubernetes extensions are read from OpenAPI after this verified join.** No expected operation ID is sent in the trace header.

Import, readiness and trace-read traffic use separate administrative requests without the fixture trace ID. No request is redirected through a pretend Microcks server.

## Pinned inputs and mock examples

- Go **1.26.5**, client-go/api/apimachinery **v0.36.2**; `go.sum` locks dependencies.
- Kubernetes **v1.36.2**, revision `24e2b02af5543d7910c2bb074c7264df5a8f0467`, [`api/openapi-spec/v3/api__v1_openapi.json`](https://github.com/kubernetes/kubernetes/blob/24e2b02af5543d7910c2bb074c7264df5a8f0467/api/openapi-spec/v3/api__v1_openapi.json).
- Source SHA-256: `a3cbb14c08e7832e80776e717a5658a178dccb3562cc5c1238193634b6985ff8` (2,128,765 bytes, 113 paths, 248 HTTP operations).
- Microcks **1.16.0**, source revision `48110d9f8dec60792f5875952d2ffd31068db8a5`, Linux/amd64 image [`quay.io/microcks/microcks-uber@sha256:9866d4737e2081a6349516fad8ca5cf04d2b85d2bd3c4382f64b3d244ecbb649`](https://quay.io/repository/microcks/microcks-uber). The image contains its Temurin 25.0.5 JRE.

`prepare.py` checks the source digest before deriving three **complete core-v1 documents**, retaining every path, operation, schema, parameter definition and Kubernetes extension:

| Import | Deliberate changes from authoritative source | Live result |
| --- | --- | --- |
| `rc-kubernetes-sparse` | Only `info.title`/`info.version` for isolated service identity | Both calls return 404; invocation traces exist but contain `operation_not_found`, so no operation mapping is accepted. |
| `rc-kubernetes-core` | Same identity change; named `fixture` examples on the two routes' path parameters (`namespace=demo`, `name=settings` where applicable) and HTTP-200 JSON responses | Both calls match the expected operations, return 200, decode successfully and satisfy fixture-content assertions. |
| `rc-kubernetes-malformed` | Copy of the example-enriched document with its two response example values replaced by deliberately invalid JSON text `not-json` | Both calls still match the expected operations and return 200, but SDK decoding fails. Request matching remains independently observable. |

Microcks' importer needs **path-parameter examples with the same example name as the response**, not only response examples, to register these request/response pairs. The full un-enriched core-v1 import succeeds but is insufficient for these concrete namespaced requests. We do not silently remove optional query parameters or rewrite paths to make matching work.

The positive PodList response has `apiVersion`, `kind`, empty metadata and an empty `items` array. The ConfigMap response has name/namespace and `data.source=microcks-fixture`. These are authored mock fixtures, not upstream Kubernetes examples. The malformed response is an explicit negative control, not a claim that Kubernetes publishes invalid examples.

The runner raises upload limits to 10 MiB per file/12 MiB per request: the first trial hit Microcks' default multipart limit. All three imports must contain exactly the same operation-name set as OpenAPI. This establishes handling of the **complete core-v1 file**, not every Kubernetes API group or a combined cluster-wide specification.

## Reproduce

Tested on Linux amd64 with Python **3.12.14** and Go **1.26.5**. Allow space for the image layers/JRE/application and memory for Java (768 MiB heap) plus Go compilation. A container daemon and a Kubernetes cluster are not required.

From this directory, explicitly acquire dependencies and pinned inputs:

```sh
python3 -m venv .venv
. .venv/bin/activate
python -m pip install -r requirements.txt
go mod download all
python prepare.py
python setup_runtime.py
```

`setup_runtime.py` downloads only the JRE and application layers from the pinned OCI image, verifies the OCI manifest digest, the selected layers' membership and each layer SHA-256, then safely extracts the two allowed prefixes. It does not rebuild or patch Microcks. The runner uses the image's JRE and application classes directly; image startup scripts/AOT caches are not used. This small daemon-free harness is pinned to **Linux amd64**. Layer verification is repeated against extracted files before live testing.

Then run checks with network acquisition disabled for Go:

```sh
export GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off GOWORK=off
export GOFLAGS='-mod=readonly -buildvcs=false' CGO_ENABLED=0
go test -count=1 ./...
python -m unittest -v test_evidence.py
python prepare.py --check
go vet ./...
go mod verify
python verify.py
```

The final command starts a fresh real Microcks process with an embedded in-memory database on a local ephemeral port, imports all three documents, builds/runs the SDK fixtures, checks positive and negative evidence, and shuts the server down on success or failure. There is no skip/fake-server fallback. External OTLP exporters are disabled; Microcks' local span storage stays enabled.

Success prints `LIVE MICROCKS VERIFIED` and writes `.cache/evidence.json`. It also compares the live semantic result with the reviewed [results/evidence.json](results/evidence.json), ignoring only random trace/span identifiers. To intentionally replace the review snapshot after inspecting a changed experiment:

```sh
python verify.py --out results/evidence.json
```

Full runtime diagnostics stay in ignored `.cache/microcks.log`. Published evidence is allowlisted: operation attributes, event names, controlled request paths and trace/span IDs. Raw trace event payloads, bodies, addresses and timestamps are not retained. The IDs are real correlation evidence from the recorded run and will differ on reproduction.

## Failure controls and limits

- Missing trace IDs produce a real trace-API 404 and are rejected.
- A real request to an unknown resource produces 404 and no operation match; rejected.
- Two operation spans for one invocation are rejected by an explicit **synthetic duplication of live evidence**; this does not claim Microcks generated ambiguous spans.
- Unit controls also reject cross-trace data, mismatched service/version/method/path, missing/duplicate invocation events, malformed event attributes, multiple requests, queries, transport failure, missing operation IDs and ambiguous/unmatched OpenAPI templates. Each mutation first passes the same validator with an unmodified positive control.
- SDK response success is recorded independently from request matching. A matched route with an SDK decode failure can prove routing; it cannot establish a valid service response or successful operation.
- Fixture attribution remains authored. This does not automatically enumerate/call SDK methods, support all options or resolve wrappers, dynamic clients, CRDs, watches, retries/fan-out or subresources.
- This is an **SDK-authoring test**, not application execution during normal profile generation. It does not resolve mappings, publish a registry artifact or generate a new RC Profile. The earlier envtest/static-replay proof remains unchanged.

The next experiment can generate ordinary typed-client candidate mappings using `go/types` and OpenAPI, and use this harness as independent behavioural verification. Example/argument generation and handwritten expansion methods remain open scaling questions. eBPF is outside this proof.

## Source references

The pinned Microcks sources verify the exact APIs and serialization used here:

- [`XTraceIdTextMapPropagator`](https://github.com/microcks/microcks/blob/48110d9f8dec60792f5875952d2ffd31068db8a5/webapp/src/main/java/io/github/microcks/config/XTraceIdTextMapPropagator.java): nonzero 32-hex IDs and sampled context propagation.
- [`TracingController`](https://github.com/microcks/microcks/blob/48110d9f8dec60792f5875952d2ffd31068db8a5/webapp/src/main/java/io/github/microcks/web/TracingController.java): span lookup returns 404 when empty.
- [`RestController`](https://github.com/microcks/microcks/blob/48110d9f8dec60792f5875952d2ffd31068db8a5/webapp/src/main/java/io/github/microcks/web/RestController.java): invocation events, operation selection and service/operation attributes.
- [`OpenAPIImporter`](https://github.com/microcks/microcks/blob/48110d9f8dec60792f5875952d2ffd31068db8a5/webapp/src/main/java/io/github/microcks/util/openapi/OpenAPIImporter.java): named path/response examples and resource-path registration.

See [VERIFICATION.md](VERIFICATION.md) for the verification report. The scoped Actions workflow repeats the proof; a local pass does not imply hosted CI has already passed.
