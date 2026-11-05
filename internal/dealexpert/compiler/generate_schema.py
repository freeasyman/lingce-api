#!/usr/bin/env python3
"""Build the v2 compiler output contract from the unchanged display body v1.1."""
import copy
import json
from pathlib import Path

BASE = Path(__file__).resolve().parent
s = json.loads((BASE.parent / 'deal_card_v1_1.schema.json').read_text())
s['$id'] = 'https://lingce.local/schema/deal-card-v2.json'
p = s['properties']
p['schema_version'] = {'const': 'deal-card-v2'}
p['rules_version'] = {'const': 'deal-compiler-v2.0.0'}
p['input_snapshot_id'] = {'type': 'string', 'pattern': '^sha256:[a-f0-9]{64}$'}
p['status'] = {'enum': ['analyzed', 'needs_review']}
refs = {'type': 'array', 'uniqueItems': True, 'items': {'type': 'string', 'pattern': '^ev_[1-9][0-9]*$'}}
p['contact_evidence_refs'] = copy.deepcopy(refs)
p['business_facts'] = {
    'type': 'array', 'items': {
        'type': 'object', 'additionalProperties': False,
        'required': ['type', 'summary', 'source_record_id', 'occurred_at', 'evidence_refs'],
        'properties': {
            'type': {'enum': ['\u9884\u7ea6', '\u5b9a\u91d1', '\u4ed8\u6b3e', '\u9000\u6b3e']},
            'summary': {'type': 'string', 'minLength': 1},
            'source_record_id': {'type': 'integer', 'minimum': 1},
            'occurred_at': {'anyOf': [{'type': 'string', 'format': 'date-time'}, {'type': 'null'}]},
            'evidence_refs': {**copy.deepcopy(refs), 'minItems': 1},
        },
    },
}
s['required'] = list(p)
defs = s['$defs']
d = defs['decision']
d['properties']['condition_evidence_refs'] = copy.deepcopy(refs)
d['required'].append('condition_evidence_refs')
e = defs['evidence']
for key, spec in {
    'source_kind': {'enum': ['transcript', 'business_fact']},
    'timing_basis': {'enum': ['asr', 'aligned', 'unknown']},
    'role_basis': {'enum': ['cleaned_transcription', 'human_verified', 'unknown', 'system_verified']},
}.items():
    e['properties'][key] = spec
    e['required'].append(key)
e['properties']['source_ref']['pattern'] = '^[sf][1-9][0-9]*$'
e['properties']['used_for']['uniqueItems'] = True
a = defs['action']
a['properties']['suggested_content'] = {'anyOf': [a['properties']['suggested_content'], {'type': 'null'}]}
a['properties']['due_provenance'] = {'enum': ['customer_source', 'system_suggestion']}
a['properties']['timing_evidence_refs'] = copy.deepcopy(refs)
a['required'] += ['due_provenance', 'timing_evidence_refs']
p['clinical_conflict']['properties']['assessment'] = {'enum': ['no_unresolved_issue_observed', 'unresolved', 'unknown']}
p['clinical_conflict']['required'].append('assessment')
# A review card must be renderable even when there are zero evidence sources.
b = p['brief']['properties']
b['situation'] = {'type': 'string', 'minLength': 1}
b['next_step'] = {'type': 'string', 'minLength': 1}
b['evidence_refs'] = copy.deepcopy(refs)
(BASE / 'card.schema.json').write_text(json.dumps(s, ensure_ascii=False, indent=2) + '\n')
print('generated deal-card-v2 schema')
