#!/usr/bin/env python3
"""Quantify rc11 defect classes on the 28-record real batch (additive, no model calls).

This script is independent of the pre-registered semantic checks: it reads the
same private packets, but reports the *defect classes* named before the run:

  * first-round structural pass (one request, one response, no error stage,
    compiled card present) and attempts per record;
  * P2-2: a tentative/conditional customer statement strengthened into
    "明确不考虑"/"明确拒绝" (any claim text or recording-level intent summary);
  * P2-3: a symptom observation written as a customer need ("kind=need" summary
    containing symptom words, or a "希望改善<症状>" rewrite);
  * timing recall: per-record pre-registered timing/schedule items (the
    "不建议当天查当天做" class) and whether the compiled card keeps them;
  * cost/tokens/latency from the raw response events (retries included);
  * diagnostic codes and failed v2 check ids, for new-failure triage.

Additive copy of check-rc10-batch05-quant.py: it reads the same private packet
shape, uses the v3 semantic oracle only for claim/quote text helpers, and keeps
every frozen expectation untouched. It never writes into a batch directory.
"""
import argparse
import base64
import hashlib
import importlib.util
import json
from pathlib import Path

V2_PATH = Path(__file__).resolve().parent / "check-real-smallbatch-v3.py"
STRONG_REFUSAL = ("明确不考虑", "明确拒绝")
SYMPTOM_WORDS = ("漏尿", "小便", "尿频", "尿急", "腰", "痛", "酸", "麻", "僵",
                 "胀", "花眼", "血脂", "血压", "干眼", "头晕", "乏力")
REWRITE_PREFIXES = ("希望改善", "想改善", "希望缓解", "想缓解")


def load_v2():
    spec = importlib.util.spec_from_file_location("frozen_v3_check", V2_PATH)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def decode_card(packet):
    raw = (packet.get("compilation") or {}).get("Card")
    if not raw:
        return None
    return json.loads(base64.b64decode(raw).decode("utf-8"))


def diagnostic_codes(packet):
    """Diagnostics are stored base64-encoded in the private packet."""
    raw = (packet.get("compilation") or {}).get("Diagnostics")
    if not raw:
        return []
    if isinstance(raw, str):
        try:
            raw = base64.b64decode(raw).decode("utf-8")
        except Exception:
            return []
    try:
        items = json.loads(raw)
    except Exception:
        return []
    return sorted({d.get("code") for d in items if isinstance(d, dict) and d.get("code")})


def response_meta(packet):
    meta = {"requests": 0, "responses": 0, "input_tokens": 0, "output_tokens": 0,
            "total_tokens": 0, "total_cost": 0.0, "latency_ms": 0,
            "error_stages": [], "error_messages": []}
    for event in packet.get("events") or []:
        kind = event.get("Type")
        if kind == "request":
            meta["requests"] += 1
        if kind == "response":
            meta["responses"] += 1
        if event.get("ErrorStage"):
            meta["error_stages"].append(str(event.get("ErrorStage")))
        if event.get("ErrorMessage"):
            meta["error_messages"].append(str(event.get("ErrorMessage"))[:300])
        if kind != "response" or not event.get("Payload"):
            continue
        try:
            payload = json.loads(base64.b64decode(event["Payload"]).decode("utf-8"))
        except Exception:
            continue
        usage = payload.get("usage") or {}
        cost = payload.get("cost") or {}
        meta["input_tokens"] += int(usage.get("input_tokens") or 0)
        meta["output_tokens"] += int(usage.get("output_tokens") or 0)
        meta["total_tokens"] += int(usage.get("total_tokens") or 0)
        meta["total_cost"] += float(cost.get("total_cost") or 0)
        meta["latency_ms"] += int(payload.get("latency_ms") or 0)
    return meta


def p2_2(card, semantic):
    claims = semantic.claim_text(card)
    hits = [word for word in STRONG_REFUSAL if word in claims]
    intents = [o for o in card.get("recording_observations") or []
               if o.get("kind") == "intent" and o.get("summary") in STRONG_REFUSAL]
    schemes = card.get("scheme_decisions") or []
    strong_schemes = [s for s in schemes if str(s.get("decision_state") or "") in STRONG_REFUSAL]
    return {
        "strong_refusal_claims": hits,
        "strong_refusal_intent_observations": [
            {"target": o.get("target"), "summary": o.get("summary")} for o in intents],
        "strong_refusal_scheme_states": [s.get("decision_state") for s in strong_schemes],
        "pass": not hits and not intents and not strong_schemes,
    }


def p2_3(card, semantic):
    claims = semantic.claim_text(card)
    needs = [o for o in card.get("recording_observations") or [] if o.get("kind") == "need"]
    symptom_needs = [o for o in needs
                     if any(word in str(o.get("summary") or "") for word in SYMPTOM_WORDS)]
    rewrites = [f"{prefix}{word}" for prefix in REWRITE_PREFIXES for word in SYMPTOM_WORDS
                if f"{prefix}{word}" in claims]
    need_summary = str((card.get("decision") or {}).get("need_summary") or "")
    need_summary_symptom = [w for w in SYMPTOM_WORDS if w in need_summary]
    return {
        "need_observations": len(needs),
        "symptom_need_observations": [o.get("summary") for o in symptom_needs],
        "symptom_rewrites": rewrites,
        "decision_need_summary_symptom_words": need_summary_symptom,
        "pass": not symptom_needs and not rewrites,
    }


def timing(card, semantic, timing_items):
    claims = semantic.claim_text(card)
    retention = claims + "\n" + semantic.quote_text(card)
    items = []
    for item in timing_items or []:
        hits = [word for word in item.get("any_of") or [] if word in retention]
        items.append({"id": item["id"], "desc": item.get("desc"), "hits": hits,
                      "ok": bool(hits)})
    return {"items": items, "pass": all(i["ok"] for i in items) if items else None}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--batch-dir", type=Path, required=True)
    parser.add_argument("--expectations", type=Path, required=True)
    parser.add_argument("--semantic-report", type=Path, default=None)
    parser.add_argument("--output", type=Path, required=True)
    args = parser.parse_args()

    semantic = load_v2()
    expectations = json.loads(args.expectations.read_text())
    summary = json.loads((args.batch_dir / "summary.json").read_text()) \
        if (args.batch_dir / "summary.json").exists() else {}
    results = {str(r["analysis_id"]): r for r in summary.get("results") or []}
    semantic_report = json.loads(args.semantic_report.read_text()) if args.semantic_report else {}
    semantic_records = {str(r["analysis_id"]): r for r in semantic_report.get("records") or []}

    records = {}
    totals = {"input_tokens": 0, "output_tokens": 0, "total_tokens": 0,
              "total_cost": 0.0, "latency_ms": 0, "requests": 0, "first_round": 0,
              "records": 0, "with_card": 0, "replay_equal": 0}
    failed_check_counts = {}
    diagnostic_counts = {}
    p2_2_pass = p2_3_pass = timing_pass = timing_total = 0

    for record_id, record_expectations in expectations["records"].items():
        packet_path = args.batch_dir / f"private-analysis-{record_id}.json"
        entry = {"scene": record_expectations.get("scene"), "packet_present": packet_path.exists()}
        if not packet_path.exists():
            records[record_id] = entry
            continue
        packet = json.loads(packet_path.read_text())
        card = decode_card(packet)
        meta = response_meta(packet)
        result = results.get(record_id, {})
        first_round = (meta["requests"] == 1 and meta["responses"] == 1
                       and not meta["error_stages"] and card is not None)
        entry.update({
            "status": result.get("status"),
            "card_status": (card or {}).get("status"),
            "rules_version": (card or {}).get("rules_version"),
            "attempts": result.get("attempts", meta["requests"]),
            "requests": meta["requests"],
            "responses": meta["responses"],
            "error_stages": meta["error_stages"],
            "error_messages": meta["error_messages"],
            "first_round_structural": first_round,
            "replay_equal": result.get("replay_equal"),
            "card_hash": result.get("card_hash"),
            "input_tokens": meta["input_tokens"],
            "output_tokens": meta["output_tokens"],
            "total_tokens": meta["total_tokens"],
            "cost_cny": round(meta["total_cost"], 6),
            "latency_ms": meta["latency_ms"],
            "diagnostic_codes": diagnostic_codes(packet),
            "semantic_pass": (semantic_records.get(record_id) or {}).get("semantic_pass"),
            "failed_checks": [c.get("id") for c in
                              (semantic_records.get(record_id) or {}).get("failed_checks") or []],
        })
        if card is not None:
            entry["p2_2"] = p2_2(card, semantic)
            entry["p2_3"] = p2_3(card, semantic)
            entry["timing"] = timing(card, semantic, record_expectations.get("timing"))
        else:
            entry["p2_2"] = {"pass": None}
            entry["p2_3"] = {"pass": None}
            entry["timing"] = {"pass": None, "items": []}

        totals["records"] += 1
        totals["requests"] += meta["requests"]
        for key in ("input_tokens", "output_tokens", "total_tokens", "latency_ms"):
            totals[key] += meta[key]
        totals["total_cost"] += meta["total_cost"]
        totals["first_round"] += 1 if first_round else 0
        totals["with_card"] += 1 if card is not None else 0
        totals["replay_equal"] += 1 if entry["replay_equal"] else 0
        if entry["p2_2"]["pass"]:
            p2_2_pass += 1
        if entry["p2_3"]["pass"]:
            p2_3_pass += 1
        if entry["timing"]["items"]:
            timing_total += 1
            timing_pass += 1 if entry["timing"]["pass"] else 0
        for check in entry["failed_checks"]:
            failed_check_counts[check] = failed_check_counts.get(check, 0) + 1
        for code in entry["diagnostic_codes"]:
            diagnostic_counts[code] = diagnostic_counts.get(code, 0) + 1
        records[record_id] = entry

    report = {
        "script": "check-rc11-batch08-quant",
        "script_sha256": hashlib.sha256(Path(__file__).read_bytes()).hexdigest(),
        "frozen_semantic_sha256": hashlib.sha256(V2_PATH.read_bytes()).hexdigest(),
        "expectations_sha256": hashlib.sha256(args.expectations.read_bytes()).hexdigest(),
        "batch": (summary.get("batch") or {}).get("batch"),
        "bundle_id": (summary.get("batch") or {}).get("bundle_id"),
        "legacy_unchanged": summary.get("legacy_unchanged"),
        "records": records,
        "summary": {
            "records": totals["records"],
            "with_card": totals["with_card"],
            "first_round_structural": totals["first_round"],
            "replay_equal": totals["replay_equal"],
            "provider_requests": totals["requests"],
            "input_tokens": totals["input_tokens"],
            "output_tokens": totals["output_tokens"],
            "total_tokens": totals["total_tokens"],
            "total_cost_cny": round(totals["total_cost"], 6),
            "latency_ms": totals["latency_ms"],
            "p2_2_pass": f"{p2_2_pass}/{totals['records']}",
            "p2_3_pass": f"{p2_3_pass}/{totals['records']}",
            "timing_records_pass": f"{timing_pass}/{timing_total}",
            "failed_check_ids": failed_check_counts,
            "diagnostic_codes": diagnostic_counts,
        },
    }
    args.output.write_text(json.dumps(report, ensure_ascii=False, indent=2) + "\n")
    print(json.dumps(report["summary"], ensure_ascii=False, indent=2))
    for record_id, entry in records.items():
        print(f"{record_id} status={entry.get('status')} attempts={entry.get('attempts')} "
              f"first_round={entry.get('first_round_structural')} "
              f"tokens={entry.get('total_tokens')} cost={entry.get('cost_cny')} "
              f"p2_2={entry.get('p2_2', {}).get('pass')} p2_3={entry.get('p2_3', {}).get('pass')} "
              f"timing={entry.get('timing', {}).get('pass')} sem={entry.get('semantic_pass')}")


if __name__ == "__main__":
    main()
