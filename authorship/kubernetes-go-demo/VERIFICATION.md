# Verification report

Verified on Linux x86-64 with Go 1.26.5 and Python 3.12.14, 25 November 2026. See go.mod/go.sum for the actual selected Go graph (x/tools v0.40.0).

Passed:
- Real Go type loading and identification: official call, renamed import, renamed variable.
- No supported match for empty/unknown namespace, Watch options, interface-injected receiver, a local type with the same method chain, ConfigMap receiver, constructor-only source.
- Missing SDK implementation source rejected.
- Offline generation with GOPROXY=off, GOSUMDB=off, GOTOOLCHAIN=local and already downloaded dependencies.
- Exact pinned Swagger byte integrity; retained schema/mapping hashes; extension semantic identity.
- SDK constructor resource extraction and generic request source checks; lookup of OpenAPI ID by endpoint.
- Exact Condition comparison, core and extension validation; five schema negative controls.
- Well-shaped wrong-resource control accepted by schema, distinguished by exact semantic comparison.
- go vet and go mod verify.

Initial failed setup: offline package loading before all dependency metadata was cached. Resolved through explicit `go mod download all`; no implicit acquisition was added to generation.

Not claimed: production profiler integration; generic SDK symbolic evaluation; arbitrary client-go methods or versions; runtime request execution; cluster permission validation; process-level network isolation. Go network lookup is disabled through environment controls, not a packet-capture or OS sandbox proof.

The workload initializer panics if executed. The tool and tests load source/types only; tests deliberately target the analyser package rather than execute the workload package.

Reproduction review caught one extra newline in a previously retained service-mapping copy. Corrected the retained bytes/hash to the pinned upstream artifact before final acquisition and schema rechecks.
