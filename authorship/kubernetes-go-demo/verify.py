"""Offline input integrity, actual typed analyser, semantic join and schema controls."""
from pathlib import Path
import copy,hashlib,json,subprocess,yaml
from jsonschema import Draft202012Validator
p=Path(__file__).resolve().parent
for name,digest in json.loads((p/'inputs.sha256.json').read_text()).items():
 assert hashlib.sha256((p/'inputs'/name).read_bytes()).hexdigest()==digest,name
assert hashlib.sha256((p/'inputs/swagger.json').read_bytes()).hexdigest()=='dcede2063da1d7ad62ecb5af8adb6d7fabd0b52385a7fa0048afb491dac90450'
subprocess.run(['go','run','.'],cwd=p,check=True)
t=json.loads((p/'results/trace.json').read_text())
m=yaml.safe_load((p/'inputs/service-mapping.yaml').read_text());ext=yaml.safe_load((p/'inputs/extension.yaml').read_text())
assert m['extension']['id']==ext['metadata']['id']
assert m['extension']['semanticSha256']==ext['metadata']['semanticSha256']
rows=[o for o in m['operations'] if o['endpoint']==t['endpoint']]
assert len(rows)==1 and rows[0]['name']==t['operationId']
entry=rows[0]['conditions'][0]
c={'kind':entry['kind'],'interface':{'type':entry['interfaceType'],'operations':[entry['operation']]}}
assert c['interface']['operations']==[{'verb':'list','apiGroup':'','apiVersion':'v1','resource':'pods','scope':'namespaced'}]
profile={'apiVersion':'runtimeconditions.io/v1alpha1','kind':'RuntimeConditionsProfile','metadata':{'name':'client-go-pod-list'},'workload':{'uri':'https://example.test/client-go-pod-list','version':'dev'},'extensions':[ext['metadata']['id']+':0.1.0'],'conditions':[c]}
core=Draft202012Validator(yaml.safe_load((p/'inputs/core.schema.yaml').read_text()))
schema=Draft202012Validator(ext['spec']['schemas'][0]['schema'])
core.validate(profile);schema.validate(c)
for key,value in [('verb','LIST'),('scope','demo'),('namespace','demo')]:
 bad=copy.deepcopy(c);bad['interface']['operations'][0][key]=value;assert list(schema.iter_errors(bad))
bad=copy.deepcopy(c);del bad['interface']['operations'][0]['apiGroup'];assert list(schema.iter_errors(bad))
bad=copy.deepcopy(profile);del bad['workload'];assert list(core.iter_errors(bad))
wrong=copy.deepcopy(c);wrong['interface']['operations'][0]['resource']='configmaps';schema.validate(wrong);assert wrong!=c
assert 'demo' not in yaml.safe_dump(profile)
(p/'results/profile.yaml').write_text(yaml.safe_dump(profile,sort_keys=False))
print('DEMO VERIFIED: typed SDK call -> source endpoint -> OpenAPI -> service mapping -> validated profile; five schema negatives passed')
