#!/usr/bin/env python3
"""Additive P2-2/P2-3 checks for the rc10 real small batch.

These checks are NOT part of the frozen v2 expectation file and do not change
any existing expectation. They encode the two semantic defects named before the
rc10 run:

  P2-2 (record 24): a tentative/conditional customer statement must not be
  strengthened into "明确不考虑"/"明确拒绝" for surgery. The surgery scheme may
  be "暂缓"/"有意向但未决定"/"无法判断", and the original reluctance must still
  be preserved (不动刀/算了吧/先保守/暂不).

  P2-3 (record 36): a symptom observation must not appear as a customer need
  (recording_observations kind=need or a "希望改善<症状>" rewrite), while the
  symptom itself must remain in structured evidence (clinical_conflict or
  secondary_findings referencing source s2).

The script reads the same private packets as the v2 checker and calls no model.
"""
import argparse
import base64
import importlib.util
import json
from pathlib import Path

V2_PATH = Path(__file__).resolve().parent / "check-real-smallbatch-v2.py"

STRONG_REFUSAL = ("明确不考虑", "明确拒绝")
SYMPTOM_WORDS = ("漏尿", "小便多", "尿频")


def load_v2():
    spec = importlib.util.spec_from_file_location("frozen_v2_check", V2_PATH)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def decode_card(packet):
    raw = (packet.get("compilation") or {}).get("Card")
    if not raw:
        return None
    return json.loads(base64.b64decode(raw).decode("utf-8"))


def check_24(card, v2):
    claims = v2.claim_text(card)
    retention = claims + "\n" + v2.quote_text(card)
    intents = [o for o in card.get("recording_observations") or [] if o.get("kind") == "intent"]
    surgery = [o for o in intents if "手术" in str(o.get("target") or "")]
    checks = []

    def add(cid, ok, detail):
        checks.append({"id": cid, "ok": bool(ok), "detail": detail})

    hits = [w for w in STRONG_REFUSAL if w in claims]
    add("p2_2_no_strong_refusal_claim", not hits, f"hits={hits}")
    bad = [o for o in surgery if o.get("summary") in STRONG_REFUSAL]
    add("p2_2_surgery_state_not_strong_refusal", not bad,
        f"surgery_states={[(o.get('target'), o.get('summary')) for o in surgery]}")
    add("p2_2_surgery_scheme_observed", bool(surgery), f"count={len(surgery)}")
    avoided = [w for w in ("不动刀", "算了吧", "先保守", "暂不") if w in retention]
    add("p2_2_avoidance_preserved", bool(avoided), f"hits={avoided}")
    return checks


def check_36(card, v2):
    claims = v2.claim_text(card)
    checks = []

    def add(cid, ok, detail):
        checks.append({"id": cid, "ok": bool(ok), "detail": detail})

    needs = [o for o in card.get("recording_observations") or [] if o.get("kind") == "need"]
    symptom_needs = [o for o in needs if any(w in str(o.get("summary") or "") for w in SYMPTOM_WORDS)]
    add("p2_3_no_symptom_as_need_observation", not symptom_needs,
        f"need_observations={len(needs)} symptom_needs={[o.get('summary') for o in symptom_needs]}")
    rewrites = [f"希望改善{w}" for w in SYMPTOM_WORDS if f"希望改善{w}" in claims]
    add("p2_3_no_symptom_rewrite_as_need", not rewrites, f"hits={rewrites}")

    evidence = {e.get("id"): e for e in card.get("evidence_refs") or []}
    clinical_refs = (card.get("clinical_conflict") or {}).get("evidence_refs") or []
    secondary_refs = [r for f in card.get("secondary_findings") or [] for r in (f.get("evidence_refs") or [])]
    structured_refs = list(clinical_refs) + secondary_refs
    symptom_refs = [r for r in structured_refs if (evidence.get(r) or {}).get("source_ref") == "s2"]
    add("p2_3_symptom_preserved_in_structured_evidence", bool(symptom_refs),
        f"structured_refs_for_s2={symptom_refs} clinical_refs={clinical_refs} secondary_refs={secondary_refs}")
    return checks


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--batch-dir", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    args = parser.parse_args()
    v2 = load_v2()

    report = {"version": "rc10-p2-checks-v1", "batch_dir": str(args.batch_dir), "records": {}}
    all_ok = True
    for record_id, fn in (("24", check_24), ("36", check_36)):
        packet = json.loads((args.batch_dir / f"private-analysis-{record_id}.json").read_text())
        card = decode_card(packet)
        checks = fn(card, v2) if card else [{"id": "card_decodes", "ok": False, "detail": "no card"}]
        ok = all(c["ok"] for c in checks)
        all_ok = all_ok and ok
        report["records"][record_id] = {"pass": ok, "checks": checks}

    # Regression context only: 30/20 must not start producing strong-refusal
    # claims or symptom-need observations.
    report["regression_context"] = {}
    for record_id in ("30", "20"):
        packet = json.loads((args.batch_dir / f"private-analysis-{record_id}.json").read_text())
        card = decode_card(packet)
        claims = v2.claim_text(card)
        needs = [o.get("summary") for o in card.get("recording_observations") or [] if o.get("kind") == "need"]
        report["regression_context"][record_id] = {
            "strong_refusal_hits": [w for w in STRONG_REFUSAL if w in claims],
            "need_observations": len(needs),
        }
    report["all_pass"] = all_ok
    args.output.write_text(json.dumps(report, ensure_ascii=False, indent=2) + "\n")
    print(json.dumps({"all_pass": all_ok,
                      "records": {k: v["pass"] for k, v in report["records"].items()},
                      "regression_context": report["regression_context"]},
                     ensure_ascii=False))
    raise SystemExit(0 if all_ok else 1)


if __name__ == "__main__":
    main()
