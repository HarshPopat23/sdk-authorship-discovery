# Verification report

Prepared 28 November 2026. Base: upstream `main` at `a6ca3807b493e58be8f71a1494bef59198a52d8a` (merged envtest PR #9). Changes are confined to this demo and its scoped workflow. The prior static and envtest demos are unchanged.

## Measured results

The real Microcks 1.16.0 application and bundled Java runtime were extracted from the pinned Linux/amd64 image layers, verified by SHA-256 and run directly. This environment has no Docker/Podman daemon. Tests do not substitute a fake Microcks process or a hand-written trace endpoint.

- The complete Kubernetes v1.36.2 core OpenAPI v3 file (113 paths, 248 HTTP operations, 2,128,765 bytes) imports successfully in all three fixture variants. The actual imported operation-name sets are checked against the source, not a manually enumerated operation table.
- Positive Pod List: HTTP 200; one request with no query; independent span identifies `GET /api/v1/namespaces/{namespace}/pods`; joined ID `listCoreV1NamespacedPod`; client-go decodes and verifies the empty list.
- Positive ConfigMap Get: HTTP 200; one request with no query; independent span identifies `GET /api/v1/namespaces/{namespace}/configmaps/{name}`; joined ID `readCoreV1NamespacedConfigMap`; client-go decodes and verifies name, namespace and fixture data.
- The source without added examples imports, and both SDK requests leave traces, but both return 404 with `operation_not_found`. No operation match is accepted from those traces.
- With deliberately malformed response examples, both routes still match and return 200, while both SDK calls fail decoding. These are successful routing observations with failed SDK-response results, never reported as successful calls.
- Missing trace lookup returns 404; unknown-resource request returns 404 without an operation match; synthetic duplicate operation evidence is rejected.
- Unit controls exercise the same validator first with valid evidence, then mutations for missing/cross/ambiguous traces, service/version/method/path disagreement, malformed/duplicate/missing invocation attributes, queries, transport failures, multiple requests and unmatched/ambiguous OpenAPI routes. Changed authoritative input is rejected before deriving imports. An independent preservation check removes only the documented example/identity edits and compares each complete derivative with the original document; unrelated schema changes are detected. A corrupted pinned OCI manifest is rejected before runtime reuse.
- Recorder unit tests verify trace-header propagation, request cloning, status/query-presence capture and transport-error presence without saving raw error text.
- The live report matches the reviewed snapshot after removing only random trace/span IDs. Setup/import/trace-read traffic is separate from per-call trace IDs.

Local verification passed all four runnable acceptance gates. The final rerun rechecks those same commands after source review.

The acceptance ledger is defined before implementation and is kept locally. It contains four runnable gates and two manual scope/publication gates. Final local gate results are recorded after source review and re-verification; publication status is reported separately to the user.

## Commands

From this module with Go 1.26.5 and Python 3.12.14/zstandard 0.25.0, after explicit acquisition:

```sh
go test -count=1 ./...
python -m unittest -v test_evidence.py
python prepare.py --check
python verify.py
go vet ./...
go mod verify
```

The repository's existing workflow-contract tests, actionlint on the added workflow, Python syntax compilation, Go formatting and whitespace checks are also run locally. The new workflow repeats pinned acquisition, unit controls, integrity/static checks and the real Microcks experiment. Hosted GitHub Actions results remain separate.

## Failures investigated

The initial multipart upload returned HTTP 413; explicitly increasing the isolated server's file/request limits resolved it. Response examples alone produced no resource-path matches because the importer also requires matching named path-parameter examples. Adding only documented examples resolved routing while preserving authoritative operation definitions.

An early client configuration used `rest.Config.Timeout`, which adds a `timeout` query parameter to Get even with empty GetOptions. A context deadline now bounds calls without changing the request query. Typed client decoding does not retain TypeMeta consistently, so response assertions check the actual typed fixture fields instead of expecting returned Kind/APIVersion fields.

Some initial shell commands used the wrong working directory and were corrected before verification. These failed trial commands are not counted as passing acceptance checks. Live servers are launched inside the verification process's network namespace and are cleaned up in a finally block.

## Coverage limits

Only two deliberately invoked methods with fixed namespace/name and empty options are covered. All 248 core-file operations are imported, but only the two exercised ones have behavioural evidence. Other API groups and a combined cluster-wide specification have not been tested. The sparse-spec finding is version-specific; another Microcks version may behave differently.

Ambiguity is tested by synthetic evidence/template mutation, not by claiming the server emitted two conflicting operation spans. A trace establishes an observed mock route, not Kubernetes authorization, resource existence, stateful behaviour, production reachability or full SDK coverage. No generated mapping/Condition/Profile or eBPF support is added here. Existing merged mapping/schema/static-profile proofs remain available in their own demos.

The daemon-free extraction path is verified on Linux amd64 only. Running image classes directly bypasses its entrypoint/AOT cache; container startup behaviour is not claimed. Full logs are ignored local diagnostics; the committed report retains only allowlisted fixture evidence.
