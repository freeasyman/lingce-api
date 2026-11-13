#!/usr/bin/env python3
"""Check a small real-recording rc7 shadow batch against pre-registered expectations.

This script is additive: it does not modify the frozen synthetic oracle
(check-statement-cases-v2.py) and it never calls a model. It reads the private
packet saved by cmd/deal-v2-shadow (frozen input, raw attempts, compiled card)
and evaluates:

  * generic contract invariants for an unresolved target binding,
  * evidence quote/speaker fidelity against the exact frozen input,
  * per-record expectations that were written before the model call.

A record passes only when every check passes. `needs_review` is reported as the
audit status, never converted into a semantic pass by itself.
"""
import argparse
import base64
import hashlib
import json
import re
from pathlib import Path

CLINICAL_TEMPLATE = "录音中存在待专业核实的事项；原文陈述不代表已确认诊断、疗效或治疗适配。"
PERSONAL_KEYS = ("need_summary", "concerns", "trust", "decision_state", "need_state", "condition")
QUOTE_KEYS = {
    "quote", "condition_due_quote", "due_quote", "customer_window",
    "evidence_refs", "condition_evidence_refs", "need_evidence_refs",
    "source_evidence_refs", "timing_evidence_refs", "contact_evidence_refs",
    "response_evidence_refs", "input_snapshot_id", "schema_version",
    "rules_version", "standard_version", "binding_record_id",
}
SKIP_SUBTREES = {"unknowns", "evidence_refs"}


def sha256(path):
    return hashlib.sha256(Path(path).read_bytes()).hexdigest()


def load_input(packet):
    """Recover the exact frozen input object sent to the model."""
    for event in packet.get("events") or []:
        if event.get("Type") != "request" or not event.get("Payload"):
            continue
        request = json.loads(base64.b64decode(event["Payload"]).decode("utf-8"))
        for message in request.get("messages") or []:
            content = message.get("content") or ""
            marker = "请分析以下冻结输入："
            if marker in content:
                return json.loads(content.split(marker, 1)[1].lstrip("\n"))
    raise ValueError("frozen input not found in request events")


def decode_card(packet):
    compilation = packet.get("compilation") or {}
    raw = compilation.get("Card")
    if not raw:
        return None
    return json.loads(base64.b64decode(raw).decode("utf-8"))


def response_meta(packet):
    meta = {"attempts": 0, "input_tokens": 0, "output_tokens": 0,
            "total_tokens": 0, "input_cost": 0.0, "output_cost": 0.0,
            "total_cost": 0.0, "latency_ms": 0}
    for event in packet.get("events") or []:
        if event.get("Type") != "response" or not event.get("Payload"):
            continue
        meta["attempts"] += 1
        try:
            payload = json.loads(base64.b64decode(event["Payload"]).decode("utf-8"))
        except Exception:
            continue
        usage = payload.get("usage") or {}
        cost = payload.get("cost") or {}
        meta["input_tokens"] += int(usage.get("input_tokens") or 0)
        meta["output_tokens"] += int(usage.get("output_tokens") or 0)
        meta["total_tokens"] += int(usage.get("total_tokens") or 0)
        meta["input_cost"] += float(cost.get("input_cost") or 0)
        meta["output_cost"] += float(cost.get("output_cost") or 0)
        meta["total_cost"] += float(cost.get("total_cost") or 0)
        meta["latency_ms"] += int(payload.get("latency_ms") or 0)
    return meta


def claim_text(card):
    """Model-authored/displayed claim text: summaries, decisions, actions, quality.

    Original quotes and the `unknowns` list are excluded here so that a quoted
    source line or an explicit uncertainty cannot be mistaken for a claim.
    """
    out = []

    def walk(value, key=None):
        if isinstance(value, dict):
            for k, v in value.items():
                if k in QUOTE_KEYS or k == "unknowns":
                    continue
                walk(v, k)
        elif isinstance(value, list):
            for v in value:
                walk(v, key)
        elif isinstance(value, str):
            out.append(value)

    for key, value in (card or {}).items():
        if key in SKIP_SUBTREES:
            continue
        walk(value, key)
    return "\n".join(out)


def quote_text(card):
    """Every displayed quote (source text shown to the user)."""
    out = []

    def walk(value, key=None):
        if isinstance(value, dict):
            for k, v in value.items():
                if k in {"quote", "condition_due_quote", "due_quote"} and isinstance(v, str):
                    out.append(v)
                else:
                    walk(v, k)
        elif isinstance(value, list):
            for v in value:
                walk(v, key)

    walk(card)
    return "\n".join(out)


def flatten_refs(card):
    refs = set()

    def walk(value):
        if isinstance(value, dict):
            for k, v in value.items():
                if k.endswith("evidence_refs") and isinstance(v, list):
                    for item in v:
                        if isinstance(item, str):
                            refs.add(item)
                walk(v)
        elif isinstance(value, list):
            for v in value:
                walk(v)

    walk(card)
    return refs


def normalize(text):
    return re.sub(r"\s+", "", text or "")


def check(record_id, card, frozen, expectations, meta):
    checks = []

    def add(check_id, ok, detail):
        checks.append({"id": check_id, "ok": bool(ok), "detail": detail})

    context = (frozen.get("input") or {}).get("context") or {}
    sources = {s["id"]: s for s in (frozen.get("input") or {}).get("transcript_sources") or []}

    # Generic invariants for unresolved target binding.
    add("input_unresolved_binding",
        context.get("target_binding") == "unresolved" and context.get("identity_confirmed") is False,
        f"target_binding={context.get('target_binding')} identity_confirmed={context.get('identity_confirmed')}")

    binding = card.get("target_binding") or {}
    add("card_binding_unresolved",
        binding.get("state") == "unresolved" and binding.get("target_customer_id") is None,
        f"state={binding.get('state')} target_customer_id={binding.get('target_customer_id')}")

    decision = card.get("decision") or {}
    personal_empty = (
        decision.get("need_summary") is None
        and decision.get("concerns") == []
        and decision.get("trust") == []
        and decision.get("decision_state") == "无法判断"
        and decision.get("need_state") == "无法判断"
        and decision.get("condition") is None
        and decision.get("evidence_refs") == []
    )
    add("personal_decision_withheld", personal_empty,
        json.dumps({k: decision.get(k) for k in PERSONAL_KEYS}, ensure_ascii=False))

    schemes_ok = True
    scheme_detail = []
    if not card.get("scheme_decisions"):
        schemes_ok = False
        scheme_detail.append("scheme_decisions empty")
    for index, scheme in enumerate(card.get("scheme_decisions") or []):
        ok = (scheme.get("decision_state") == "无法判断"
              and scheme.get("need_summary") is None
              and scheme.get("concerns") == []
              and scheme.get("trust") == []
              and scheme.get("condition") is None)
        if not ok:
            schemes_ok = False
            scheme_detail.append(f"scheme[{index}] state={scheme.get('decision_state')} "
                                 f"need={scheme.get('need_summary')!r} concerns={scheme.get('concerns')}")
    add("scheme_decisions_withheld", schemes_ok, "; ".join(scheme_detail) or "all schemes withheld")

    action = card.get("primary_action")
    sales = bool(action) and action.get("type") == "sales_follow_up"
    add("no_sales_action_without_binding", not sales,
        "none" if not action else f"type={action.get('type')} template={action.get('template')}")

    add("contact_policy_guarded", card.get("contact_policy") in {"review", "pause_clinical"},
        str(card.get("contact_policy")))
    # v2: recording that the customer agreed to add WeChat is a contact fact, not
    # a sales authorization. Only an authorization claim (or a sales action) is
    # forbidden while the server has not verified identity/contact authorization.
    claims_text = claim_text(card)
    auth_claims = [p for p in ("已授权", "获得授权", "同意联系", "允许联系", "明确授权") if p in claims_text]
    add("no_contact_authorization_claim", not auth_claims,
        f"authorization_claims={auth_claims}")

    quality = card.get("quality") or {}
    dims = quality.get("dimensions") or {}
    ungraded = all((d or {}).get("score") is None and (d or {}).get("assessment") == "insufficient_evidence"
                   for d in dims.values())
    add("quality_scores_withheld", bool(dims) and ungraded,
        f"{len(dims)} dimensions, scored={[k for k, d in dims.items() if (d or {}).get('score') is not None]}")

    clinical = card.get("clinical_conflict") or {}
    if clinical.get("active"):
        add("clinical_conflict_template", clinical.get("reason") == CLINICAL_TEMPLATE
            and clinical.get("assessment") == "unresolved",
            f"assessment={clinical.get('assessment')} reason={clinical.get('reason')!r}")
    else:
        add("clinical_conflict_template", True, "inactive")

    # Evidence fidelity against the exact frozen input.
    evidence = card.get("evidence_refs") or []
    by_id = {}
    evidence_ok = True
    evidence_detail = []
    for item in evidence:
        ev_id = item.get("id")
        if ev_id in by_id:
            evidence_ok = False
            evidence_detail.append(f"duplicate evidence id {ev_id}")
        by_id[ev_id] = item
        source = sources.get(item.get("source_ref"))
        quote = item.get("quote") or ""
        if source is None:
            evidence_ok = False
            evidence_detail.append(f"{ev_id}: unknown source_ref {item.get('source_ref')}")
            continue
        exact = quote.strip() == (source.get("text") or "").strip()
        normalized = normalize(quote) and normalize(quote) in normalize(source.get("text"))
        if not exact and not normalized:
            evidence_ok = False
            evidence_detail.append(f"{ev_id}: quote not found in {source.get('id')}")
        if item.get("speaker") != source.get("speaker"):
            evidence_ok = False
            evidence_detail.append(f"{ev_id}: speaker {item.get('speaker')} != {source.get('speaker')}")
    add("evidence_verbatim_and_speaker", evidence_ok, "; ".join(evidence_detail[:6]) or f"{len(evidence)} evidence items verified")

    referenced = flatten_refs(card)
    dangling = sorted(ref for ref in referenced if ref not in by_id)
    add("evidence_refs_resolve", not dangling, f"dangling={dangling}")

    # Per-record pre-registered expectations.
    claims = claim_text(card)
    retention = claims + "\n" + quote_text(card)
    for expected in expectations.get("preserve") or []:
        hits = [word for word in expected.get("any_of") or [] if word in retention]
        add("preserve:" + expected["id"], bool(hits),
            f"hits={hits} desc={expected['desc']}")

    for forbidden in expectations.get("forbid") or []:
        hits = [pattern for pattern in forbidden.get("patterns") or [] if pattern in claims]
        add("forbid:" + forbidden["id"], not hits,
            f"hits={hits} desc={forbidden['desc']}")

    return checks


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--batch-dir", type=Path, required=True)
    parser.add_argument("--expectations", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--expected-bundle", default="")
    args = parser.parse_args()

    expectations = json.loads(args.expectations.read_text())
    summary_path = args.batch_dir / "summary.json"
    summary = json.loads(summary_path.read_text()) if summary_path.exists() else {}
    bundle_id = ((summary.get("batch") or {}).get("bundle_id")) or ""

    records = []
    total_cost = 0.0
    total_tokens = 0
    all_pass = True
    for record_id in expectations.get("records") or {}:
        packet_path = args.batch_dir / f"private-analysis-{record_id}.json"
        if not packet_path.exists():
            records.append({"analysis_id": record_id, "semantic_pass": False,
                            "checks": [{"id": "packet_present", "ok": False, "detail": "missing packet"}]})
            all_pass = False
            continue
        packet = json.loads(packet_path.read_text())
        result_path = args.batch_dir / f"analysis-{record_id}.json"
        result = json.loads(result_path.read_text()) if result_path.exists() else {}
        card = decode_card(packet)
        if card is None:
            records.append({"analysis_id": record_id, "semantic_pass": False,
                            "checks": [{"id": "card_present", "ok": False, "detail": "no compiled card"}]})
            all_pass = False
            continue
        frozen = load_input(packet)
        meta = response_meta(packet)
        checks = check(record_id, card, frozen, expectations["records"][record_id], meta)
        rules_ok = str(card.get("rules_version", "")).startswith("deal-compiler-v2.1.0")
        checks.append({"id": "rules_version_v21", "ok": rules_ok, "detail": str(card.get("rules_version"))})
        if args.expected_bundle:
            checks.append({"id": "bundle_id_expected", "ok": bundle_id == args.expected_bundle,
                           "detail": f"{bundle_id} vs {args.expected_bundle}"})
        failed = [c for c in checks if not c["ok"]]
        semantic_pass = not failed
        all_pass = all_pass and semantic_pass
        total_cost += meta["total_cost"]
        total_tokens += meta["total_tokens"]
        records.append({
            "analysis_id": int(record_id),
            "scene": expectations["records"][record_id].get("scene"),
            "audit_status": result.get("status"),
            "card_status": card.get("status"),
            "attempts": result.get("attempts"),
            "replay_equal": result.get("replay_equal"),
            "card_hash": result.get("card_hash"),
            "total_tokens": meta["total_tokens"] or result.get("total_tokens"),
            "input_tokens": meta["input_tokens"],
            "output_tokens": meta["output_tokens"],
            "latency_ms": meta["latency_ms"] or result.get("latency_ms"),
            "total_cost_cny": round(meta["total_cost"], 6),
            "semantic_pass": semantic_pass,
            "failed_checks": failed,
            "checks": checks,
            "unknowns": card.get("unknowns") or [],
            "diagnostic_codes": sorted({
                d.get("code") for d in (packet.get("compilation") or {}).get("Diagnostics") or [] if isinstance(d, dict)
            }),
        })

    report = {
        "script": "check-real-smallbatch-v2",
        "script_sha256": sha256(__file__),
        "expectations_sha256": sha256(args.expectations),
        "batch": (summary.get("batch") or {}).get("batch"),
        "rules_version": ((summary.get("batch") or {}).get("model") or {}).get("model_code"),
        "bundle_id": bundle_id,
        "expected_bundle": args.expected_bundle,
        "records": records,
        "summary": {
            "records": len(records),
            "semantic_pass": sum(1 for r in records if r["semantic_pass"]),
            "semantic_fail": sum(1 for r in records if not r["semantic_pass"]),
            "total_tokens": total_tokens,
            "total_cost_cny": round(total_cost, 6),
            "all_pass": all_pass,
        },
    }
    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_text(json.dumps(report, ensure_ascii=False, indent=2) + "\n")
    print(json.dumps(report["summary"], ensure_ascii=False))
    for record in records:
        print(f"analysis_id={record['analysis_id']} status={record.get('audit_status')} "
              f"semantic_pass={record['semantic_pass']} tokens={record.get('total_tokens')} "
              f"cost={record.get('total_cost_cny')}")
        for failure in record.get("failed_checks") or []:
            print(f"  FAIL {failure['id']}: {failure['detail']}")


if __name__ == "__main__":
    main()
