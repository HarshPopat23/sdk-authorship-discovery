"""Explicit acquisition of checksum-pinned envtest assets; never run by the demo."""
from pathlib import Path
import hashlib
import os
import platform
import tarfile
import urllib.request

ROOT = Path(__file__).resolve().parent
VERSION = '1.36.2'
# SHA-512 from controller-tools' envtest release index, not a locally calculated trust value.
ASSETS = {
    'amd64': 'ea743186c8a799f5cf8faf16969f86189d003cb7d130e0ac4b58789f1e5748dcf30ebe91c837a10d5ac415383da3e10b9e64d65785c938c23e739781cfb76f08',
    'arm64': '2d72ee985a8e262a3c57dc9f7f0fd891f6a8c7bf7ebaa2db6dc6d8eac8ae28181afe51c1f368b67756cdb40b10de9b205609e1726e5f27f7c6d824dd9c6649ac',
}

def main():
    arch = {'x86_64': 'amd64', 'aarch64': 'arm64'}.get(platform.machine())
    if platform.system() != 'Linux' or arch not in ASSETS:
        raise SystemExit('This focused setup supports Linux amd64/arm64 only.')
    name = f'envtest-v{VERSION}-linux-{arch}.tar.gz'
    archive = ROOT / '.cache' / name
    archive.parent.mkdir(parents=True, exist_ok=True)
    if not archive.exists() or hashlib.sha512(archive.read_bytes()).hexdigest() != ASSETS[arch]:
        url = f'https://github.com/kubernetes-sigs/controller-tools/releases/download/envtest-v{VERSION}/{name}'
        with urllib.request.urlopen(url, timeout=120) as response:
            data = response.read()
        if hashlib.sha512(data).hexdigest() != ASSETS[arch]:
            raise SystemExit('envtest archive checksum mismatch')
        archive.write_bytes(data)
    target = ROOT / '.cache' / f'assets-{VERSION}-{arch}'
    target.mkdir(parents=True, exist_ok=True)
    with tarfile.open(archive) as source:
        source.extractall(target, filter='data')
    assets = target / 'controller-tools' / 'envtest'
    for name in ['kube-apiserver', 'etcd', 'kubectl']:
        if not (assets / name).is_file() or not os.access(assets / name, os.X_OK):
            raise SystemExit(f'missing executable asset: {name}')
    print(assets)

if __name__ == '__main__':
    main()
