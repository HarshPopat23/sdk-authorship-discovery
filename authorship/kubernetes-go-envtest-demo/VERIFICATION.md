# Verification report

Prepared on 27 November 2026 on Linux amd64 with Go 1.26.5, Python 3.12.14, PyYAML 6.0.3 and jsonschema 4.26.0. Base: upstream `main` at `e833e575cd2034b432a23b378f8f793c29c58bf6` (merged PR #7). The previous unpublished multi-mapping/AST-generator work is not included.

## Acceptance and observed results

- Unit tests exercise unmatched and ambiguous OpenAPI paths, wrong methods/actions/versions, query/options outside the slice, SDK errors even after a successful response status, failed HTTP statuses, transport failures, zero requests, retries/fan-out, duplicate fixture/method evidence and all-or-nothing mapping generation. Successful List and Get controls exercise the same classifier.
- Recorder tests confirm only method/path/status and boolean failure/query-presence metadata are retained. Header values, userinfo, host/port, query values, response bodies and raw errors are excluded.
- **Live integration ran against the real envtest Kubernetes v1.36.2 API server and etcd.** The test never uses a fake client or skips missing assets.
- Pod List produced exactly one GET to `/api/v1/namespaces/demo/pods`, HTTP 200, matching `listCoreV1NamespacedPod`.
- ConfigMap Get produced exactly one GET to `/api/v1/namespaces/demo/configmaps/settings`, HTTP 200, matching `readCoreV1NamespacedConfigMap`. The SDK decoded and checked the prepared fixture value.
- A separate missing-ConfigMap call produced one real GET with HTTP 404 and an SDK error. It was rejected for mapping generation and retained separately in the evidence.
- Setup and discovery use another client. The measured observations contain only the two intended GET requests; namespace/ConfigMap creation traffic is absent.
- `verify.py` checks the selected SDK/controller-runtime module versions and reuses the unchanged integrity-pinned Swagger, core schema and extension from the earlier Go demo. Two fresh live runs produce byte-identical results matching the review snapshots.
- Generated Conditions validate against core Profile schema 0.4.0 and the existing Kubernetes API extension 0.1.0. Invalid verb/scope/extra namespace and missing workload negatives fail. A schema-valid wrong-resource control demonstrates the need for the exact semantic comparison.
- The generated five-rule compact mapping is consumed by the unchanged merged static Go analyser against its existing fixture, producing the same two Conditions and extension ID. The fixture initializer would panic if executed; this replay does not execute it.
- A changed authoritative input fails before overwriting the previous generated output.
- Go vet, module integrity, repository workflow-contract tests, actionlint 1.7.12 on the new workflow, formatting and whitespace checks pass locally. The new Actions workflow repeats the unit/live/schema/static-replay proof; a local run is not a claim that hosted CI has already passed.

The acceptance ledger has four runnable checks plus two manual checks (scope/claims and publication). Runnable gates are re-executed after source review. Local approval/transcript state is not included in the commit.

## Failures encountered and resolved

An initial acquisition command was issued from the repository root rather than the new Go module and was rerun in the module directory. An initial run without `KUBEBUILDER_ASSETS` failed explicitly, as intended. The first live startup failed because the sandbox has no default host network route. Debug control-plane output identified that failure; configuring the API server's advertise/bind addresses to `127.0.0.1` resolved it. The successful integration runs used the same real binaries afterward. No fake-client or skipped-test substitute was introduced.

## Evidence limits

Only the two explicit fixture invocations with empty options and chosen namespace/object inputs are demonstrated. Public method/getter metadata and fixture correlation are authored, with signatures checked through reflection; this is not automatic method attribution or SDK-wide invocation generation. The mapping skeleton/extension-write recipe is authored, while request method/path and OpenAPI operation/action/resource come from observed traffic and its authoritative join.

This is pre-TLS client transport recording, not packet capture, eBPF tracing, TLS decryption or server-side audit correlation. The evidence cannot establish whether eBPF can recover equivalent information. Failures, queries, retries, multi-request methods, dynamic clients, CRDs, watches and wrappers are deliberately unsupported for publication. One observed execution is not a proof of every branch or production authorization/reachability.

The envtest control plane has no kubelet or workload scheduler/controller-manager; the proof creates API objects but runs no application Pods. Linux arm64 asset integrity is pinned but live execution on that architecture was not tested. Tests require loopback networking and explicit prior acquisition; Go download controls are not process-level network isolation.
