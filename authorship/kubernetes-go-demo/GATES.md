# Gates: client-go mapping demo

OWNS: authorship/kubernetes-go-demo/**

Scope: real Go type identification, source-derived endpoint join, profile and negative validation.

- [x] G1: type-aware fixture and negative tests pass without workload execution
  CHECK: go test .
  EXPECT: /^ok[ \t]+example\.test\/rc-client-go-demo/m
  EVIDENCE: automatic-evidence=v1; definition-sha256=79d1a3bab2ff4e078e036ed6c7d2205d2b7c84fc93d8f5dd293913d59dc41e5d; exit=0; EXPECT=matched; output-sha256=da3ad16430f21bd9bfd3bc1e329aa5519d1953615d866c109cdf586bea9fe424; output-bytes=45; shell=/bin/sh; cwd=/workspace/scratch/a2754ffda951/sdk-demo/authorship/kubernetes-go-demo; path=2744278e5d73/14 entries
- [x] G2: generated profile and trace match OpenAPI and extension with negative controls
  CHECK: python3 verify.py
  EXPECT: DEMO VERIFIED
  EVIDENCE: automatic-evidence=v1; definition-sha256=9252977d55a3922a7b612bb3c79545d18783e2357a68bd669506c8decac17fc9; exit=0; EXPECT=matched; output-sha256=bd10c60be90cfe341db2d1a0b805d77146c7bb1b92c2cb2f716677b6a85b21db; output-bytes=130; shell=/bin/sh; cwd=/workspace/scratch/a2754ffda951/sdk-demo/authorship/kubernetes-go-demo; path=2744278e5d73/14 entries
- [x] G3: reproduction and limitations accurately describe evidence
  EVIDENCE: README and VERIFICATION reviewed against real runs and source limitations
