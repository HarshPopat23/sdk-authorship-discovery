"""Launch the pinned real Microcks runtime, import inputs, invoke SDKs, verify spans."""
import argparse
import json
import os
from pathlib import Path
import secrets
import socket
import subprocess
import tempfile
import time
import urllib.error
import urllib.request

from evidence import Rejected, verify_observation, HTTP_METHODS, event_attributes
from urllib.parse import urlsplit
import prepare
import setup_runtime

ROOT = Path(__file__).resolve().parent


def request(base, path, data=None, headers=None):
    req = urllib.request.Request(base + path, data=data, headers=headers or {})
    try:
        with urllib.request.urlopen(req, timeout=20) as response:
            return response.status, response.read()
    except urllib.error.HTTPError as error:
        return error.code, error.read()


def upload(base, name):
    boundary = 'rc-' + secrets.token_hex(16)
    body = (f'--{boundary}\r\nContent-Disposition: form-data; name="file"; filename="{name}"\r\nContent-Type: application/json\r\n\r\n').encode()
    body += (ROOT / '.cache' / name).read_bytes() + f'\r\n--{boundary}--\r\n'.encode()
    status, _ = request(base, '/api/artifact/upload', body, {'Content-Type': f'multipart/form-data; boundary={boundary}'})
    assert status == 201, f'import failed: {name}, HTTP {status}'


def trace(base, trace_id):
    deadline = time.monotonic() + 5
    while time.monotonic() < deadline:
        status, raw = request(base, f'/api/traces/{trace_id}/spans')
        assert status in (200, 404), f'trace API HTTP {status}'
        if status == 200:
            spans = json.loads(raw)
            if any(any(event.get('name') == 'invocation_received' for event in span.get('events', [])) for span in spans):
                return spans
        time.sleep(.05)
    raise AssertionError('missing completed invocation trace')


def rejected(call):
    try:
        call()
    except Rejected as error:
        return str(error)
    raise AssertionError('unsupported evidence accepted')


def normalized_spans(spans):
    # Deliberate allowlist: exclude bodies, addresses, timestamps and raw error text.
    keep = {'service.name', 'service.version', 'operation.name', 'operation.method'}
    result = []
    for span in spans:
        invocations = [event for event in span.get('events', []) if event.get('name') == 'invocation_received']
        if not invocations:
            continue
        if len(invocations) != 1:
            raise Rejected('ambiguous invocation events')
        event = event_attributes(invocations[0]['attributes'])
        uri = urlsplit(event['uri.full'])
        result.append({'traceId': span['traceId'], 'spanId': span['spanId'],
                       'attributes': {key: value for key, value in span.get('attributes', {}).items() if key in keep},
                       'invocation': {'method': event['http.method'], 'path': uri.path, 'hasQuery': bool(uri.query)},
                       'events': [event['name'] for event in span.get('events', [])]})
    return result


def semantic(value):
    if isinstance(value, dict):
        return {key: semantic(item) for key, item in value.items() if key not in ('traceId', 'spanId')}
    if isinstance(value, list):
        return [semantic(item) for item in value]
    return value


def run(output):
    document = prepare.prepare(check=True)
    runtime = setup_runtime.prepare(check=True)
    go = os.environ.get('GO', 'go')
    version = subprocess.check_output([go, 'version'], text=True)
    assert 'go1.26.5 ' in version, 'use Go 1.26.5'
    for name in ('k8s.io/client-go', 'k8s.io/api', 'k8s.io/apimachinery'):
        module = json.loads(subprocess.check_output([go, 'list', '-m', '-json', name], cwd=ROOT, text=True))
        assert module['Version'] == 'v0.36.2' and 'Replace' not in module
    with tempfile.TemporaryDirectory(prefix='rc-microcks-') as temporary:
        binary = Path(temporary) / 'invoke'
        subprocess.run([go, 'build', '-mod=readonly', '-o', str(binary), '.'], cwd=ROOT, check=True)
        with socket.socket() as sock:
            sock.bind(('127.0.0.1', 0))
            port = sock.getsockname()[1]
        base = f'http://127.0.0.1:{port}'
        log_path = ROOT / '.cache' / 'microcks.log'
        with log_path.open('w') as log:
            command = [str(runtime / 'opt/java/openjdk/25-jre/bin/java'), '-Xmx768m', '-cp',
                       str(runtime / 'deployments/application/app.jar'), 'io.github.microcks.MicrocksApplication',
                       '--server.address=127.0.0.1', f'--server.port={port}', '--grpc.server.port=0',
                       '--spring.servlet.multipart.max-file-size=10MB', '--spring.servlet.multipart.max-request-size=12MB',
                       '--management.otlp.logs.export.enabled=false', '--management.tracing.sampling.probability=1.0']
            env = os.environ | {'SPRING_PROFILES_ACTIVE': 'uber', 'OTEL_LOGS_EXPORTER': 'none',
                               'OTEL_TRACES_EXPORTER': 'none', 'OTEL_METRICS_EXPORTER': 'none'}
            process = subprocess.Popen(command, cwd=temporary, stdout=log, stderr=log, env=env)
            try:
                deadline = time.monotonic() + 45
                while time.monotonic() < deadline:
                    if process.poll() is not None:
                        raise AssertionError(f'Microcks exited; see {log_path}')
                    try:
                        status, raw = request(base, '/api/services')
                        if status == 200:
                            assert json.loads(raw) == [], 'requires fresh, isolated Microcks'
                            break
                    except urllib.error.URLError:
                        pass
                    time.sleep(.1)
                else:
                    raise AssertionError(f'Microcks startup timed out; see {log_path}')
                for name in ('sparse.json', 'examples.json', 'malformed.json'):
                    upload(base, name)
                status, raw = request(base, '/api/services')
                assert status == 200
                services = json.loads(raw)
                expected = {method.upper() + ' ' + path for path, item in document['paths'].items() for method in item if method in HTTP_METHODS}
                assert len(services) == 3
                for service in services:
                    assert service['version'] == prepare.VERSION
                    imported = [operation['name'] for operation in service['operations']]
                    assert len(imported) == len(expected) and set(imported) == expected
                observations, rejected_controls = [], []
                seen_traces = set()
                positive = None
                for service in ('rc-kubernetes-core', 'rc-kubernetes-sparse', 'rc-kubernetes-malformed'):
                    for case, (_, expected_id) in zip(('pod-list', 'configmap-get'), prepare.CASES):
                        invocation = json.loads(subprocess.check_output([str(binary), '--host', base, '--service', service, '--case', case], text=True, timeout=20))
                        assert invocation['traceId'] not in seen_traces
                        seen_traces.add(invocation['traceId'])
                        spans = trace(base, invocation['traceId'])
                        if service == 'rc-kubernetes-sparse':
                            reason = rejected(lambda: verify_observation(invocation, spans, document, service))
                            assert len(invocation['requests']) == 1 and invocation['requests'][0]['status'] == 404
                            assert not invocation['sdkSuccess'] and not invocation['responseVerified']
                            assert any(event.get('name') == 'operation_not_found' for span in spans for event in span.get('events', []))
                            rejected_controls.append({'control': 'sparse-' + case, 'requestMatched': False,
                                                      'invocation': invocation, 'spans': normalized_spans(spans), 'reason': reason})
                        else:
                            try:
                                result = verify_observation(invocation, spans, document, service)
                            except Rejected as error:
                                raise AssertionError(f'{service}/{case}: {invocation}') from error
                            assert result['operationId'] == expected_id and result['request']['status'] == 200
                            assert invocation['sdkSuccess'] == (service == 'rc-kubernetes-core')
                            assert invocation['responseVerified'] == (service == 'rc-kubernetes-core')
                            result['service'] = service
                            result['spans'] = normalized_spans(spans)
                            observations.append(result)
                            if positive is None:
                                positive = (invocation, spans)
                missing = secrets.token_hex(16)
                assert missing not in seen_traces
                assert request(base, f'/api/traces/{missing}/spans')[0] == 404
                rejected_controls.append({'control': 'missing-trace', 'httpStatus': 404,
                                          'reason': rejected(lambda: verify_observation(positive[0], [], document, 'rc-kubernetes-core'))})
                unknown = '/rest/rc-kubernetes-core/v1.36.2/api/v1/namespaces/demo/not-a-resource'
                trace_id = secrets.token_hex(16)
                status, _ = request(base, unknown, headers={'X-Trace-Id': trace_id})
                assert status == 404
                spans = trace(base, trace_id)
                invocation = {'case': 'unmatched-control', 'sdkMethod': 'raw HTTP control (not an SDK fixture)', 'traceId': trace_id,
                              'requests': [{'method': 'GET', 'path': unknown, 'status': status, 'transportError': False, 'hasQuery': False}],
                              'sdkSuccess': False, 'responseVerified': False}
                rejected_controls.append({'control': 'unmatched-request', 'invocation': invocation, 'spans': normalized_spans(spans),
                                          'reason': rejected(lambda: verify_observation(invocation, spans, document, 'rc-kubernetes-core'))})
                duplicate = positive[1] + [span for span in positive[1] if 'operation.name' in span.get('attributes', {})]
                rejected_controls.append({'control': 'ambiguous-operation-span',
                                          'reason': rejected(lambda: verify_observation(positive[0], duplicate, document, 'rc-kubernetes-core'))})
                result = {'microcks': {'version': '1.16.0', 'image': setup_runtime.IMAGE},
                          'clientGo': 'v0.36.2', 'openapi': {'url': prepare.URL, 'sha256': prepare.SHA256,
                          'pathCount': len(document['paths']), 'operationCount': len(expected)},
                          'observations': observations, 'rejectedControls': rejected_controls}
                snapshot = ROOT / 'results' / 'evidence.json'
                if output.resolve() != snapshot.resolve():
                    assert semantic(result) == semantic(json.loads(snapshot.read_bytes())), 'live semantics differ from reviewed snapshot'
                output.parent.mkdir(exist_ok=True, parents=True)
                output.write_bytes(prepare.encode(result))
            finally:
                process.terminate()
                try:
                    process.wait(timeout=15)
                except subprocess.TimeoutExpired:
                    process.kill()
                    process.wait(timeout=5)
    print('LIVE MICROCKS VERIFIED')


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--out', type=Path, default=ROOT / '.cache' / 'evidence.json')
    run(parser.parse_args().out)
