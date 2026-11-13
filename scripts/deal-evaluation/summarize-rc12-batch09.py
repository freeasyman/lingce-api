#!/usr/bin/env python3
"""Aggregate a rc12 real small batch: attempts, tokens, cost, replay, status.

Reads only the private packets written by cmd/deal-v2-shadow. It never calls a
model and never writes to the database. The report is aggregate-only so it can be
stored outside the private directory.
"""
import argparse
import base64
import hashlib
import json
from pathlib import Path


def sha256(path):
    return hashlib.sha256(Path(path).read_bytes()).hexdigest()


def decode_card(packet):
    raw = (packet.get("compilation") or {}).get("Card")
    return json.loads(base64.b64decode(raw).decode("utf-8")) if raw else None


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--batch-dir", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    args = parser.parse_args()
    summary = json.loads((args.batch_dir / "summary.json").read_text())
    results = {int(r["analysis_id"]): r for r in summary.get("results") or []}

    records = []
    total_tokens = total_cost = 0
    for packet_path in sorted(args.batch_dir.glob("private-analysis-*.json"),
                              key=lambda p: int(p.stem.rsplit("-", 1)[1])):
        packet = json.loads(packet_path.read_text())
        analysis_id = int(packet["analysis_id"])
        card = decode_card(packet)
        result = results.get(analysis_id, {})
        requests = 0
        input_tokens = output_tokens = 0
        cost = 0.0
        for event in packet.get("events") or []:
            if event.get("Type") != "response" or not event.get("Payload"):
                continue
            requests += 1
            payload = json.loads(base64.b64decode(event["Payload"]).decode("utf-8"))
            usage = payload.get("usage") or {}
            input_tokens += int(usage.get("input_tokens") or 0)
            output_tokens += int(usage.get("output_tokens") or 0)
            cost += float((payload.get("cost") or {}).get("total_cost") or 0)
        tokens = input_tokens + output_tokens
        total_tokens += tokens
        total_cost += cost
        observations = (card or {}).get("recording_observations") or []
        statements = [o for o in observations if o.get("kind") in ("need", "concern", "trust")]
        records.append({
            "analysis_id": analysis_id,
            "status": result.get("status"),
            "card_status": (card or {}).get("status"),
            "attempts": result.get("attempts"),
            "requests": requests,
            "replay_equal": result.get("replay_equal"),
            "total_tokens": tokens,
            "input_tokens": input_tokens,
            "output_tokens": output_tokens,
            "total_cost_cny": round(cost, 6),
            "latency_ms": result.get("latency_ms"),
            "rules_version": (card or {}).get("rules_version"),
            "observation_kinds": [o.get("kind") for o in observations],
            "local_observations": len([o for o in observations
                                       if o.get("kind") in ("timing", "willingness", "exam", "symptom",
                                                            "question", "staff_statement", "other")]),
            "max_statement_runes": max((len(o.get("summary") or "") for o in statements), default=0),
            "statement_texts_unique": len({o.get("summary") for o in statements}) == len(statements),
            "diagnostic_codes": sorted({d.get("code") for d in
                                        json.loads(base64.b64decode(
                                            (packet.get("compilation") or {}).get("Diagnostics") or "W10="))
                                        if isinstance(d, dict)}),
            "target_binding": (card or {}).get("target_binding", {}).get("state"),
            "primary_action": None if (card or {}).get("primary_action") is None
                              else (card or {}).get("primary_action", {}).get("type"),
        })

    report = {
        "script": "summarize-rc12-batch09",
        "script_sha256": sha256(__file__),
        "batch": (summary.get("batch") or {}).get("batch"),
        "bundle_id": (summary.get("batch") or {}).get("bundle_id"),
        "legacy_unchanged": summary.get("legacy_unchanged"),
        "records": records,
        "totals": {
            "records": len(records),
            "requests": sum(r["requests"] for r in records),
            "total_tokens": total_tokens,
            "total_cost_cny": round(total_cost, 6),
            "needs_review": sum(1 for r in records if r["status"] == "needs_review"),
            "replay_equal": sum(1 for r in records if r["replay_equal"]),
            "first_attempt_only": sum(1 for r in records if r["attempts"] == 1),
        },
    }
    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_text(json.dumps(report, ensure_ascii=False, indent=2) + "\n")
    print(json.dumps(report["totals"], ensure_ascii=False))
    for record in records:
        print(f"analysis_id={record['analysis_id']} status={record['status']} attempts={record['attempts']} "
              f"tokens={record['total_tokens']} cost={record['total_cost_cny']} "
              f"max_stmt={record['max_statement_runes']} unique={record['statement_texts_unique']} "
              f"local={record['local_observations']}")


if __name__ == "__main__":
    main()
