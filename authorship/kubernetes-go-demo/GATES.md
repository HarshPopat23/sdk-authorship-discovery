# Acceptance checks

Run from this directory after the explicit acquisition in [README.md](README.md).
Machine-specific approval/evidence state belongs in untracked local files.

| Check | Command | Required outcome |
| --- | --- | --- |
| Typed matching and data-driven coverage | `go test -count=1 -v .` | Official operations and alias cases match; unsupported patterns do not; each terminal record controls its own coverage; invalid explicit rules fail. |
| Authoring and generated profiles | `python3 verify.py` | Three SDK/OpenAPI joins, schema checks, five schema negatives, wrong-record control and deterministic output; prints `RECORDS VERIFIED`. |
| Go static/dependency checks | `go vet .` then `go mod verify` | Zero exit; all modules verified. |
| Scope and documentation review | Review diff and README | Authored experimental rules, source-check limits and deferred full-SDK coverage stated accurately; only this demo changes. |

See [VERIFICATION.md](VERIFICATION.md) for measured results and limitations.
