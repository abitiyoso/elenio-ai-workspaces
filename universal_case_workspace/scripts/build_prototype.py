"""Embed example domain packs and fixture data into self-contained HTML prototype.
No network or backend integrations. Does not modify production app code.
"""
import json
from pathlib import Path
ROOT=Path(__file__).resolve().parents[1]
TEMPLATE=ROOT/'prototype_source/base_template.html'
SCRIPT=ROOT/'prototype_source/prototype.js'
TARGET=ROOT/'Universal_Case_Workspace_Prototype.html'
packjson=[json.loads(p.read_text()) for p in sorted((ROOT/'domain-packs').glob('*.json'))]
casejson=json.loads((ROOT/'samples/demo_cases.json').read_text())
def as_script_json(value):
 return json.dumps(value,ensure_ascii=False).replace('<','\\u003c').replace('>','\\u003e').replace('&','\\u0026')
html=TEMPLATE.read_text().replace('__PACK_JSON__',as_script_json(packjson)).replace('__CASE_JSON__',as_script_json(casejson)).replace('__SCRIPT_JS__',SCRIPT.read_text())
TARGET.write_text(html)
print('Wrote',TARGET, 'bytes', TARGET.stat().st_size)
