"""Live reproduction, schema checks and static consumption of observed mappings."""
from pathlib import Path
import copy
import hashlib
import json
import subprocess
import tempfile

import yaml
from jsonschema import Draft202012Validator

ROOT = Path(__file__).resolve().parent
STATIC = ROOT.parent / 'kubernetes-go-demo'
FILES = ['observations.json', 'profile.yaml', 'runtimeconditions.sdk-mapping.yaml']


def main():
    selected = subprocess.check_output(['go', 'list', '-m', '-f',
                                        '{{.Path}} {{.Version}}{{if .Replace}} replaced{{end}}',
                                        'k8s.io/client-go', 'sigs.k8s.io/controller-runtime'], cwd=ROOT, text=True)
    assert selected.splitlines() == ['k8s.io/client-go v0.36.2', 'sigs.k8s.io/controller-runtime v0.24.1']
    # Reuse the immutable schema/OpenAPI inputs of the existing proof.
    for name, expected in json.loads((STATIC / 'inputs.sha256.json').read_text()).items():
        assert hashlib.sha256((STATIC / 'inputs' / name).read_bytes()).hexdigest() == expected, name
    extension = yaml.safe_load((STATIC / 'inputs/extension.yaml').read_text())
    core = Draft202012Validator(yaml.safe_load((STATIC / 'inputs/core.schema.yaml').read_text()))
    condition_schema = Draft202012Validator(extension['spec']['schemas'][0]['schema'])
    with tempfile.TemporaryDirectory() as tmp:
        out = Path(tmp) / 'live'
        command = ['go', 'run', '.', '--out', str(out)]
        subprocess.run(command, cwd=ROOT, check=True)
        first = {name: (out / name).read_bytes() for name in FILES}
        subprocess.run(command, cwd=ROOT, check=True)
        assert first == {name: (out / name).read_bytes() for name in FILES}, 'live output drift'
        assert first == {name: (ROOT / 'results' / name).read_bytes() for name in FILES}, 'snapshot drift'
        evidence = json.loads(first['observations.json'])
        profile = yaml.safe_load(first['profile.yaml'])
        mapping = yaml.safe_load(first['runtimeconditions.sdk-mapping.yaml'])
        assert evidence['kubernetes'] == 'v1.36.2'
        assert evidence['clientGo'] == 'v0.36.2' and evidence['controllerRuntime'] == 'v0.24.1'
        assert evidence['setupTrafficExcluded'] is True
        expected = [
            ('pods-list', 'PodInterface', 'List', '/api/v1/namespaces/demo/pods',
             '/api/v1/namespaces/{namespace}/pods', 'listCoreV1NamespacedPod', 'pods', 'list'),
            ('configmaps-get', 'ConfigMapInterface', 'Get', '/api/v1/namespaces/demo/configmaps/settings',
             '/api/v1/namespaces/{namespace}/configmaps/{name}', 'readCoreV1NamespacedConfigMap', 'configmaps', 'get'),
        ]
        assert len(evidence['observations']) == len(evidence['matches']) == len(profile['conditions']) == 2
        for observed, matched, condition, row in zip(evidence['observations'], evidence['matches'], profile['conditions'], expected):
            case, receiver, method, path, template, operation, resource, verb = row
            assert observed['case'] == case and observed['sdkError'] is False
            assert observed['sdk']['receiver']['name'] == receiver and observed['sdk']['method'] == method
            assert observed['requests'] == [{'method': 'GET', 'path': path, 'status': 200}]
            assert matched == {'path': template, 'method': 'GET', 'operationId': operation, 'action': verb, 'resource': resource}
            assert condition == {'kind': 'kubernetes.api', 'interface': {'type': 'api', 'operations': [{
                'verb': verb, 'apiGroup': '', 'apiVersion': 'v1', 'resource': resource, 'scope': 'namespaced'}]}}
            condition_schema.validate(condition)
        assert evidence['failedCall']['sdkError'] is True
        assert evidence['failedCall']['requests'][0]['status'] == 404
        assert evidence['failedCallRejection'] == 'SDK call failed'
        assert len(mapping['rules']) == 5 and mapping['extension']['id'] == extension['metadata']['id']
        assert [r['id'] for r in mapping['rules'] if 'condition' in r] == ['pods-list', 'configmaps-get']
        core.validate(profile)
        # Exact endpoint semantics need more than schema shape alone.
        wrong = copy.deepcopy(profile['conditions'][0])
        wrong['interface']['operations'][0]['resource'] = 'secrets'
        condition_schema.validate(wrong)
        assert wrong != profile['conditions'][0]
        for key, value in [('verb', 'LIST'), ('scope', 'demo'), ('namespace', 'demo')]:
            bad = copy.deepcopy(profile['conditions'][0]); bad['interface']['operations'][0][key] = value
            assert list(condition_schema.iter_errors(bad))
        bad = copy.deepcopy(profile); del bad['workload']; assert list(core.iter_errors(bad))
        for data in first.values():
            for secret in [b'Authorization', b'127.0.0.1', b'client-key', b'client-certificate', b'Bearer ']:
                assert secret not in data
        # This is the merged, unchanged static consumer, not the live observer.
        replay = Path(tmp) / 'static'
        subprocess.run(['go', 'run', '.', '--project', 'fixture', '--mapping', str(out / 'runtimeconditions.sdk-mapping.yaml'),
                        '--out', str(replay)], cwd=STATIC, check=True)
        replay_profile = json.loads((replay / 'profile.json').read_text())
        assert replay_profile['conditions'] == profile['conditions']
        assert replay_profile['extensions'] == profile['extensions']
        assert [c['rule'] for c in json.loads((replay / 'trace.json').read_text())['calls']] == ['pods-list', 'configmaps-get']
        # A bad authoritative input fails before touching the previous output.
        invalid = Path(tmp) / 'wrong-swagger.json'; invalid.write_text('{}')
        failed = subprocess.run(['go', 'run', '.', '--swagger', str(invalid), '--out', str(out)], cwd=ROOT,
                                text=True, capture_output=True)
        assert failed.returncode != 0 and 'pinned Kubernetes' in failed.stderr
        assert first == {name: (out / name).read_bytes() for name in FILES}
    print('BEHAVIOURAL PROOF VERIFIED: two reproducible live SDK calls; real 404 rejected; setup excluded; OpenAPI joins; schema negatives; unchanged static consumer replay')


if __name__ == '__main__':
    main()
