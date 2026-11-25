"""Explicit network-enabled acquisition; never called by generation."""
from pathlib import Path
import urllib.request,json,hashlib
p=Path(__file__).resolve().parent
urls={
'swagger.json':'https://raw.githubusercontent.com/kubernetes/kubernetes/24e2b02af5543d7910c2bb074c7264df5a8f0467/api/openapi-spec/swagger.json',
'service-mapping.yaml':'https://raw.githubusercontent.com/runtimeconditions/extensions/f35107c0aaf9dfb16ea0632c6e5a4cc23217d668/catalog/kubernetes/api/model/generated/kubernetes-service-mapping.yaml'}
for name,url in urls.items():
 target=p/'inputs'/name
 expected=json.loads((p/'inputs.sha256.json').read_text())[name]
 if target.exists() and hashlib.sha256(target.read_bytes()).hexdigest()==expected:
  print('Verified cached',name);continue
 data=urllib.request.urlopen(url,timeout=60).read()
 assert hashlib.sha256(data).hexdigest()==json.loads((p/'inputs.sha256.json').read_text())[name],name
 (p/'inputs'/name).write_bytes(data)
 print('Verified',name)
