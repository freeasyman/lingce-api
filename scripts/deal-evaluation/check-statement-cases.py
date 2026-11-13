#!/usr/bin/env python3
"""Check frozen synthetic expectations against saved real provider packets."""
import argparse
import base64
import json
from pathlib import Path

# Written before observing model outputs. These are requirements of the finite
# synthetic dialogue corpus, not a domain gold standard for real recordings.
EXPECTED = {
    1: dict(concerns=False, trust=False, need=False),
    2: dict(concerns=True, trust=False, need=False),
    3: dict(concerns=False, trust=False),
    4: dict(concerns=True, trust=False),
    5: dict(need=True, trust=True),
    6: dict(trust=False),
}


def check_packet(packet, case):
    compilation = packet.get('compilation')
    if not compilation:
        return dict(case=case, passed=False, errors=['no completed compilation'])
    card = json.loads(base64.b64decode(compilation['Card']))
    manifest = json.loads(packet['manifest_raw'])
    snapshot = json.loads(base64.b64decode(manifest['snapshot_bytes']))
    sources = {r['id']: r['text'] for r in snapshot['input']['transcript_sources']}
    evidence = {r['id']: r for r in card['evidence_refs']}
    errors = []
    decisions = card['scheme_decisions']
    # Check all personal and recording paths: do not hide a bad claim in a
    # secondary scheme or through unresolved-scope observations.
    all_concerns = [r for d in decisions for r in d['concerns']]
    all_trust = [r for d in decisions for r in d['trust']]
    all_needs = [d for d in decisions if d['need_summary'] is not None]
    values = dict(concerns=bool(all_concerns), trust=bool(all_trust), need=bool(all_needs))
    for key, wanted in EXPECTED[case].items():
        if values[key] != wanted:
            errors.append(f'{key}: expected {wanted}, got {values[key]}')
    rows = [(r['summary'], r['evidence_refs']) for r in all_concerns + all_trust]
    rows += [(d['need_summary'], d['need_evidence_refs']) for d in all_needs]
    for r in card['recording_observations']:
        if r['kind'] in ('need', 'concern', 'trust'):
            rows.append((r['summary'], r['evidence_refs']))
            field = {'concern': 'concerns', 'trust': 'trust', 'need': 'need'}[r['kind']]
            if EXPECTED[case].get(field) is False:
                errors.append(f'forbidden {field} escaped via recording observations')
    for text, refs in rows:
        exact = '\n'.join(sources[evidence[ref]['source_ref']] for ref in refs)
        if text != exact:
            errors.append('displayed statement differs from complete frozen utterances')
    if case == 2:
        if any('不害怕' not in r['summary'] for r in all_concerns):
            errors.append('negation context lost')
    if case == 4:
        if not any('我害怕手术' in r['summary'] for r in all_concerns):
            errors.append('explicit fear was dropped')
    # These cases have no contact consent, completed event or paid transaction.
    if card['business_facts'] or card['event_reports']:
        errors.append('invented completed/reported event')
    if card['primary_action'] and card['primary_action']['type'] == 'sales_follow_up':
        errors.append('sales action without contact permission')
    return dict(case=case, passed=not errors, errors=errors, observed=values)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('packets', type=Path)
    parser.add_argument('--output', required=True, type=Path)
    args = parser.parse_args()
    results = []
    for case in EXPECTED:
        path = args.packets / f'private-analysis-{case}.json'
        if not path.exists():
            results.append(dict(case=case, passed=False, errors=['missing packet']))
        else:
            results.append(check_packet(json.loads(path.read_text()), case))
    report = dict(status='PASS' if all(r['passed'] for r in results) else 'FAIL',
                  scope='Six independent synthetic dialogues; not real-corpus acceptance.',
                  expectations=EXPECTED, results=results)
    args.output.write_text(json.dumps(report, ensure_ascii=False, indent=2) + '\n')
    print(json.dumps(report, ensure_ascii=False, indent=2))
    raise SystemExit(0 if report['status'] == 'PASS' else 1)


if __name__ == '__main__':
    main()
