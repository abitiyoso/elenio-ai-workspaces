"""Static schema and semantic checks for bundled Domain Pack fixtures.

This is a contract-fixture test, not a production policy or security validator.
"""
import json
from pathlib import Path
from jsonschema import Draft202012Validator
ROOT=Path(__file__).resolve().parents[1]
SCHEMA=json.loads((ROOT/'contracts/workspace_definition.schema.json').read_text())
Draft202012Validator.check_schema(SCHEMA)
VALID_VIEWS={'overview','artifact_list','tasks','approvals','documents','ai_activity','timeline','activity','extension'}
VALID_EXECUTORS={'agent_invocation','flowcase_action','artifact_command','registered_extension'}
packs=[]
for path in sorted((ROOT/'domain-packs').glob('*.json')):
 pack=json.loads(path.read_text());packs.append(pack)
 errs=list(Draft202012Validator(SCHEMA).iter_errors(pack))
 assert not errs, f'{path.name} schema: {errs}'
 nav=pack['navigation']; actions=pack['actions']; types=pack['artifact_types']
 navkeys=[x['key'] for x in nav]; actionkeys=[x['key'] for x in actions]; typekeys=[x['key'] for x in types]
 assert len(set(navkeys))==len(navkeys),(path,'duplicate nav key')
 assert len(set(actionkeys))==len(actionkeys),(path,'duplicate action key')
 assert len(set(typekeys))==len(typekeys),(path,'duplicate artifact type')
 for n in nav:
  assert n['view'] in VALID_VIEWS
  if n['view']=='artifact_list':assert n.get('artifact_type') in typekeys
  if n['view']=='extension':assert n.get('extension_ref'),'Extension requires explicit registered reference'
 for t in types:
  fieldkeys=[f['key'] for f in t['fields']]
  assert len(set(fieldkeys))==len(fieldkeys)
  for f in t['fields']:
   if f['widget']=='select':assert f.get('options'),'Select must have options'
 for a in actions:
  assert a['executor'] in VALID_EXECUTORS
  if a['executor']=='agent_invocation':
   assert a.get('agent_key') in pack['bindings'].get('agent_keys',[])
   if 'output_artifact_type' in a:assert a['output_artifact_type'] in typekeys
  if a['executor']=='flowcase_action':assert a.get('command_key')
 assert len(pack['bindings']['flowcase_workflow_key'])>2
 print('PASS',path.name,len(nav),'tabs',len(types),'artifact types')
assert len({(p['pack_key'],p['version']) for p in packs})==len(packs)
cases=json.loads((ROOT/'samples/demo_cases.json').read_text())['cases']
key_to_pack={(p['pack_key'],p['version']):p for p in packs}
for c in cases:
 assert (c['pack_key'],c['pack_version']) in key_to_pack
 pack=key_to_pack[(c['pack_key'],c['pack_version'])]
 validtypes={t['key'] for t in pack['artifact_types']}
 for a in c['artifacts']:
  assert a['type'] in validtypes
  fields={f['key'] for t in pack['artifact_types'] if t['key']==a['type'] for f in t['fields']}
  assert set(a['data']).issubset(fields)
 print('PASS case',c['id'],'using',c['pack_key'])
print('STATIC VALIDATION PASSED (not a live integration test)')
