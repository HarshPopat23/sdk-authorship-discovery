"""Extract the pinned Linux/amd64 Microcks image's JRE and application (no daemon).

Only regular files/directories/internal symlinks below two fixed prefixes are
extracted. Every source layer is verified before extraction or reuse.
"""
import argparse
import hashlib
import json
import os
import platform
from pathlib import Path
import tarfile
import urllib.request
import zstandard

ROOT = Path(__file__).resolve().parent / '.cache'
IMAGE = 'quay.io/microcks/microcks-uber@sha256:9866d4737e2081a6349516fad8ca5cf04d2b85d2bd3c4382f64b3d244ecbb649'
# Layers from that immutable OCI manifest: bundled Temurin JRE and application.
LAYERS = [
    ('e7105b33993a7c9c7f963656b6a87103d730b5e38282c80a9f5d78aa8c150635', 'zstd', 'opt/java/'),
    ('556402471c2f74bc7f79102d15c353130c67c41023b80b938052862fc3758996', 'gzip', 'deployments/application/'),
]


def prepare(check=False):
    if platform.system() != 'Linux' or platform.machine() not in ('x86_64', 'amd64'):
        raise RuntimeError('daemon-free runtime is pinned for Linux amd64 only')
    runtime = ROOT / 'runtime'
    ROOT.mkdir(exist_ok=True)
    manifest_path = ROOT / 'microcks-manifest.json'
    manifest_digest = IMAGE.split('@sha256:')[1]
    if not manifest_path.exists():
        if check:
            raise RuntimeError('missing pinned OCI manifest; run setup_runtime.py first')
        request = urllib.request.Request('https://quay.io/v2/microcks/microcks-uber/manifests/sha256:' + manifest_digest, headers={'Accept': 'application/vnd.oci.image.manifest.v1+json'})
        with urllib.request.urlopen(request, timeout=60) as response:
            manifest_bytes = response.read()
        if hashlib.sha256(manifest_bytes).hexdigest() != manifest_digest:
            raise RuntimeError('OCI manifest checksum mismatch')
        manifest_path.write_bytes(manifest_bytes)
    manifest_bytes = manifest_path.read_bytes()
    if hashlib.sha256(manifest_bytes).hexdigest() != manifest_digest:
        raise RuntimeError('cached OCI manifest checksum mismatch')
    manifest_layers = {layer['digest']: layer['mediaType'] for layer in json.loads(manifest_bytes)['layers']}
    for digest, compression, prefix in LAYERS:
        if not manifest_layers.get('sha256:' + digest, '').endswith('+' + compression):
            raise RuntimeError('selected layer does not belong to pinned image manifest')
        archive = ROOT / (digest + '.layer')
        if not archive.exists():
            if check:
                raise RuntimeError('missing runtime layers; run setup_runtime.py first')
            temporary = archive.with_suffix('.partial')
            try:
                with urllib.request.urlopen(f'https://quay.io/v2/microcks/microcks-uber/blobs/sha256:{digest}', timeout=120) as response, temporary.open('wb') as output:
                    while chunk := response.read(1024 * 1024):
                        output.write(chunk)
                with temporary.open('rb') as downloaded:
                    if hashlib.file_digest(downloaded, 'sha256').hexdigest() != digest:
                        raise RuntimeError('runtime layer checksum mismatch')
                temporary.replace(archive)
            finally:
                temporary.unlink(missing_ok=True)
        with archive.open('rb') as source:
            if hashlib.file_digest(source, 'sha256').hexdigest() != digest:
                raise RuntimeError('cached runtime layer checksum mismatch')
        with archive.open('rb') as source:
            data = zstandard.ZstdDecompressor().stream_reader(source) if compression == 'zstd' else source
            with tarfile.open(fileobj=data, mode='r|*') as tar:
                for member in tar:
                    if not member.name.startswith(prefix):
                        continue
                    if not (member.isfile() or member.isdir() or member.issym()):
                        raise RuntimeError('unsupported runtime archive entry')
                    target = runtime / member.name
                    if check:
                        if member.isfile():
                            with tar.extractfile(member) as expected, target.open('rb') as actual:
                                if hashlib.file_digest(expected, 'sha256').digest() != hashlib.file_digest(actual, 'sha256').digest():
                                    raise RuntimeError(f'extracted runtime differs: {member.name}')
                        elif member.issym():
                            if not target.is_symlink() or os.readlink(target) != member.linkname:
                                raise RuntimeError('extracted runtime symlink differs')
                    else:
                        tar.extract(member, runtime, filter='data')
    return runtime


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--check', action='store_true')
    print(prepare(parser.parse_args().check))
