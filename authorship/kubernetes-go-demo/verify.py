"""Offline author checks, record-driven generation, and schema negative controls.

The small source-fragment adapter below checks the reviewed routes for this pilot;
it is not a general SDK-to-OpenAPI mapping generator.
"""
from pathlib import Path
import copy
import hashlib
import json
import re
import subprocess

import yaml
from jsonschema import Draft202012Validator

ROOT = Path(__file__).resolve().parent
SWAGGER_SHA = "dcede2063da1d7ad62ecb5af8adb6d7fabd0b52385a7fa0048afb491dac90450"
# Independent, reviewed source joins for the three supported fixtures. The runtime
# consumer never imports this table or looks up a method by its spelling alone.
OPERATIONS = [
    ("pods-list", "pod.go", "Pods", "newPods", "pods", "list", "listCoreV1NamespacedPod"),
    ("configmaps-list", "configmap.go", "ConfigMaps", "newConfigMaps", "configmaps", "list", "listCoreV1NamespacedConfigMap"),
    ("configmaps-get", "configmap.go", "ConfigMaps", "newConfigMaps", "configmaps", "get", "readCoreV1NamespacedConfigMap"),
]


def digest(data):
    return hashlib.sha256(data).hexdigest()


def condition(rule):
    writes = {w["target"]: w["value"] for w in rule["condition"]["writes"]}
    assert len(writes) == 3
    return {"kind": writes["kind"], "interface": {
        "type": writes["interface.type"], "operations": [writes["interface.operations[]"]]}}


def check_authoring(mapping, swagger, sdk_root):
    """Check exact pinned SDK source fragments, OpenAPI and authored writes."""
    files = {}

    def source(name):
        data = (sdk_root / name).read_bytes()
        files[name] = digest(data)
        return data.decode()

    clientset = source("kubernetes/clientset.go")
    assert "func (c *Clientset) CoreV1() corev1.CoreV1Interface {\n\treturn c.coreV1\n}" in clientset
    core = source("kubernetes/typed/core/v1/core_client.go")
    assert 'gv := corev1.SchemeGroupVersion\n\tconfig.GroupVersion = &gv\n\tconfig.APIPath = "/api"' in core
    gentype = source("gentype/type.go")
    assert 'resource:       resource,' in gentype and 'namespace:      namespace,' in gentype
    assert 'typeClient := NewClient[T](resource, client, parameterCodec, namespace, emptyObjectCreator, options...)' in gentype
    # Limit checks to each method's body, rather than unrelated occurrences.
    list_body = gentype.split('func (l *alsoLister[T, L]) List(', 1)[1].split('\n}', 1)[0]
    get_body = gentype.split('func (c *Client[T]) Get(', 1)[1].split('\n}', 1)[0]
    assert 'l.client.client.Get().' in list_body
    assert 'NamespaceIfScoped(l.client.namespace, l.client.namespace != "").' in list_body
    assert 'Resource(l.client.resource).' in list_body and 'VersionedParams(&opts,' in list_body
    assert 'c.client.Get().' in get_body
    assert 'NamespaceIfScoped(c.namespace, c.namespace != "").' in get_body
    assert 'Resource(c.resource).' in get_body and 'Name(name).' in get_body
    rules = {r['id']: r for r in mapping['rules']}
    evidence = []
    for rule_id, file, getter, constructor, resource, verb, operation_id in OPERATIONS:
        text = source('kubernetes/typed/core/v1/' + file)
        assert f'return {constructor}(c, namespace)' in core
        body = text.split(f'func {constructor}(', 1)[1].split('\n}', 1)[0]
        match = re.search(r'gentype.NewClientWithListAndApply\[.*?\]\(\s*"([^\"]+)",\s*c.RESTClient\(\),\s*scheme.ParameterCodec,\s*namespace,', body, re.S)
        assert match and match.group(1) == resource
        # The helper embeds the same generic Client and lister implementations.
        assert '*gentype.ClientWithListAndApply[' in text
        assert 'alsoLister[T, L]{typeClient, emptyListCreator},' in gentype
        path = f'/api/v1/namespaces/{{namespace}}/{match.group(1)}'
        if verb == 'get':
            path += '/{name}'
        op = swagger['paths'][path]['get']
        assert op['operationId'] == operation_id
        assert op['x-kubernetes-action'] == verb
        assert op['x-kubernetes-group-version-kind']['group'] == ''
        assert op['x-kubernetes-group-version-kind']['version'] == 'v1'
        expected = {'verb': verb, 'apiGroup': '', 'apiVersion': 'v1',
                    'resource': resource, 'scope': 'namespaced'}
        c = condition(rules[rule_id])
        assert c == {'kind': 'kubernetes.api', 'interface': {'type': 'api', 'operations': [expected]}}
        evidence.append({'rule': rule_id, 'sdkGetter': getter,
                         'constructorResource': match.group(1), 'httpMethod': 'GET',
                         'path': path, 'operationId': op['operationId'],
                         'condition': c})
    return {'sourceSha256': files, 'operations': evidence,
            'scope': 'reviewed fragments and route composition for client-go v0.36.2 only; not symbolic execution'}


def main():
    for name, expected in json.loads((ROOT / 'inputs.sha256.json').read_text()).items():
        assert digest((ROOT / 'inputs' / name).read_bytes()) == expected, name
    assert digest((ROOT / 'inputs/swagger.json').read_bytes()) == SWAGGER_SHA
    mapping = yaml.safe_load((ROOT / 'runtimeconditions.sdk-mapping.yaml').read_text())
    extension = yaml.safe_load((ROOT / 'inputs/extension.yaml').read_text())
    assert mapping['extension']['id'] == extension['metadata']['id']
    core = Draft202012Validator(yaml.safe_load((ROOT / 'inputs/core.schema.yaml').read_text()))
    schema = Draft202012Validator(extension['spec']['schemas'][0]['schema'])
    cache = subprocess.check_output(['go', 'env', 'GOMODCACHE'], text=True).strip()
    swagger = json.loads((ROOT / 'inputs/swagger.json').read_text())
    evidence = check_authoring(mapping, swagger, Path(cache) / 'k8s.io/client-go@v0.36.2')
    # A schema-valid but semantically incorrect record must fail the source join.
    wrong = copy.deepcopy(mapping)
    next(r for r in wrong['rules'] if r['id'] == 'pods-list')['condition']['writes'][2]['value']['resource'] = 'secrets'
    schema.validate(condition(next(r for r in wrong['rules'] if r['id'] == 'pods-list')))
    try:
        check_authoring(wrong, swagger, Path(cache) / 'k8s.io/client-go@v0.36.2')
    except AssertionError:
        pass
    else:
        raise AssertionError('semantically wrong mapping accepted')
    subprocess.run(['go', 'run', '.'], cwd=ROOT, check=True)
    first = {name: (ROOT / 'results' / name).read_bytes() for name in ['trace.json', 'profile.json']}
    subprocess.run(['go', 'run', '.'], cwd=ROOT, check=True)
    assert all((ROOT / 'results' / name).read_bytes() == data for name, data in first.items())
    trace = json.loads(first['trace.json'])
    profile = json.loads(first['profile.json'])
    assert trace['mappingSha256'] == digest((ROOT / 'runtimeconditions.sdk-mapping.yaml').read_bytes())
    assert trace['sdkVersion'] == 'v0.36.2'
    assert [c['rule'] for c in trace['calls']] == [row[0] for row in OPERATIONS]
    assert len(profile['conditions']) == 3
    for call, authored, generated in zip(trace['calls'], evidence['operations'], profile['conditions']):
        assert call['condition'] == generated == authored['condition']
        assert len(call['steps']) == 3 and call['steps'][0]['rule'] == 'core-client'
        assert call['steps'][0]['selection'] == '(*k8s.io/client-go/kubernetes.Clientset).CoreV1'
        assert call['steps'][1]['selection'].startswith('(k8s.io/client-go/kubernetes/typed/core/v1.')
        assert call['steps'][2]['rule'] == authored['rule']
        schema.validate(generated)
    core.validate(profile)
    c = profile['conditions'][0]
    for key, value in [('verb', 'LIST'), ('scope', 'demo'), ('namespace', 'demo')]:
        bad = copy.deepcopy(c)
        bad['interface']['operations'][0][key] = value
        assert list(schema.iter_errors(bad))
    bad = copy.deepcopy(c)
    del bad['interface']['operations'][0]['apiGroup']
    assert list(schema.iter_errors(bad))
    bad = copy.deepcopy(profile)
    del bad['workload']
    assert list(core.iter_errors(bad))
    assert '"demo"' not in json.dumps(profile) and '"settings"' not in json.dumps(profile)
    (ROOT / 'results/profile.yaml').write_text(yaml.safe_dump(profile, sort_keys=False))
    (ROOT / 'results/authoring-evidence.json').write_text(json.dumps(evidence, indent=2) + '\n')
    print('RECORDS VERIFIED: 3 typed operations; SDK/OpenAPI joins; direct writes; core/extension schemas; 5 schema negatives; wrong-record control; deterministic output')


if __name__ == '__main__':
    main()
