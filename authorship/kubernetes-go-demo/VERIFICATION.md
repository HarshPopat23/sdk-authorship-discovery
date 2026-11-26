# Verification report

Verified on Linux x86-64, Go 1.26.5, Python 3.12.14, PyYAML 6.0.3 and jsonschema 4.26.0 on 26 November 2026. The selected Go module graph is in go.mod/go.sum; YAML v3 moved from indirect to direct use. All changes are confined to this demo.

## Passed

- `go test -count=1 .`: source/type analysis of the three real calls, import/variable renaming, and producer-only calls. Unsupported empty/unknown namespaces, non-empty options, empty object name, injected interface root, local lookalike receiver, unmapped resource/method and variable receiver behave as documented.
- Each of the three terminal records can be removed and restored independently. The other two calls still match; restoring the record restores three matches without any consumer edit.
- Invalid metadata, duplicate IDs/selectors, unknown surfaces, missing argument types, invalid positions, unsupported constraints/write targets, incomplete writes, dual outputs, unknown YAML fields, multiple documents and a missing explicit mapping are rejected. A missing mapping creates no output directory.
- A locally replaced client-go module still type-checks but is rejected by the explicit SDK validation selection.
- `python3 setup_inputs.py`: cached authoritative Swagger matches the pinned byte digest. The acquisition script no longer requires a service-mapping artifact.
- `python3 verify.py`: all three SDK constructor/request-to-OpenAPI joins agree with their rule writes and emitted Conditions. Profile core and each extension Condition validate. The generated JSON is identical across two consumer runs.
- Five schema negative controls reject an invalid verb, invalid scope, unexpected namespace field, missing apiGroup and missing workload.
- A wrong-resource record remains schema-valid but fails the independent authoring source join. This distinguishes semantic evidence from schema validity.
- `go vet .`, `go mod verify`, and `git diff --check` pass.
- Documentation/source review confirms the consumer has no Pod/ConfigMap-specific dispatch, does not read Swagger or SDK implementation files to discover mappings, and uses the explicit compact direct-write table.

Final acceptance: **4 met, 0 unmet, 0 abandoned**. The runnable gates were re-executed after review. The fourth gate is manual scope/documentation review, not an independent automated correctness proof.

## Failed and corrected during implementation

The first compile check caught an unused import left from the previous adapter. The first authoring check also caught an incorrect expected generic-constructor fragment; direct source inspection showed the actual helper composition, and the check was corrected to that implementation. Both were fixed before the successful final checks. No failed or blocked acceptance check remains.

## Limits of this evidence

Dependencies were explicitly acquired/cached before running with `GOTOOLCHAIN=local`, `GOPROXY=off`, `GOSUMDB=off`, `GOWORK=off`, `GOFLAGS=-mod=readonly`, `CGO_ENABLED=0` and `GOPACKAGESDRIVER=off`. Those controls are not a network-isolation or packet-capture proof.

The fixture's initializer panics if executed. Tests target the analyser package (`.`), and the workload is loaded for types rather than executed. No Kubernetes cluster or live request was used; authorization, runtime reachability, transport identity and request success were not tested.

This is a manually authored experimental Go mapping subset for one exact SDK release. Only the three direct chains with constant non-empty names and empty options are supported. Source-fragment verification and reviewed route composition are not general SDK symbolic analysis. The standalone consumer checks structural mapping validity; the separate verifier checks extension semantics. Automatic mapping generation, full SDK coverage, variable/interprocedural flow, dynamic clients, CRDs, complex wrappers, a registry and production profiler integration are outside this change, not blocked acceptance work.
