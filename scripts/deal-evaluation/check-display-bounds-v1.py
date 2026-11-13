#!/usr/bin/env python3
"""Read-only display checks for rc10/rc11 real batches.

The frozen semantic oracle (v2/v3) asks whether expected facts survived. This
checker asks three separate display questions that the rc10 12-sample run
exposed:

  * statement_summaries_bounded: need/concern/trust summaries are <= 240 runes
    (rc11 compiler cap; id 18 was 3,963 runes of mixed-role transcript);
  * statement_summary_verbatim: every published excerpt is a verbatim slice of
    a cited source after removing the display ellipsis; no model rewrite;
  * intent_has_customer_speaker: a positive kind=intent observation cites at
    least one customer-spoken turn (id 26 was an employee conclusion).

It never calls a model. It does not modify any expectation or old checker.
"""
import argparse
import base64
import hashlib
import json
import re
from pathlib import Path

MAX_STATEMENT_RUNES = 240
STATEMENT_KINDS = ("need", "concern", "trust")


def sha256(path):
    return hashlib.sha256(Path(path).read_bytes()).hexdigest()


def load_input(packet):
    for event in packet.get("events") or []:
        if event.get("Type") != "request" or not event.get("Payload"):
            continue
        request = json.loads(base64.b64decode(event["Payload"]).decode("utf-8"))
        for message in request.get("messages") or []:
            content = message.get("content") or ""
            marker = "请分析以下冻结输入："
            if marker in content:
                remainder = content.split(marker, 1)[1].lstrip("\n")
                value, _ = json.JSONDecoder().raw_decode(remainder)
                return value
    raise ValueError("frozen input not found in request events")


def decode_card(packet):
    raw = (packet.get("compilation") or {}).get("Card")
    if not raw:
        return None
    return json.loads(base64.b64decode(raw).decode("utf-8"))


def normalize(text):
    return re.sub(r"\s+", "", text or "")


def strip_display_ellipsis(text):
    text = text or ""
    if text.startswith("…"):
        text = text[1:]
    if text.endswith("…"):
        text = text[:-1]
    return text


def check_record(packet):
    card = decode_card(packet)
    frozen = load_input(packet)
    sources = {s["id"]: s for s in (frozen.get("input") or {}).get("transcript_sources") or []}
    evidence = {e["id"]: e for e in card.get("evidence_refs") or []}
    checks = []

    def add(check_id, ok, detail):
        checks.append({"id": check_id, "ok": bool(ok), "detail": detail})

    bounded_detail = []
    verbatim_detail = []
    for index, obs in enumerate(card.get("recording_observations") or []):
        kind = obs.get("kind")
        if kind not in STATEMENT_KINDS:
            continue
        summary = obs.get("summary") or ""
        length = len(summary)
        if length > MAX_STATEMENT_RUNES + 2:  # two display ellipses
            bounded_detail.append(f"obs[{index}] kind={kind} runes={length}")
        refs = [evidence[r]["source_ref"] for r in obs.get("evidence_refs") or [] if r in evidence]
        source_texts = [normalize(sources[r]["text"]) for r in refs if r in sources]
        for part in (summary.split("\n") if summary else []):
            part = normalize(strip_display_ellipsis(part))
            if not part:
                continue
            if not any(part in text for text in source_texts):
                verbatim_detail.append(f"obs[{index}] kind={kind} excerpt not in cited source")
    add("statement_summaries_bounded", not bounded_detail, "; ".join(bounded_detail) or "all <= 240 runes")
    add("statement_summary_verbatim", not verbatim_detail, "; ".join(verbatim_detail) or "all excerpts verbatim")

    intent_detail = []
    for index, obs in enumerate(card.get("recording_observations") or []):
        if obs.get("kind") != "intent" or obs.get("summary") in (None, "无法判断"):
            continue
        refs = [evidence[r]["source_ref"] for r in obs.get("evidence_refs") or [] if r in evidence]
        customer = any(
            sources[r].get("speaker") == "客户" and sources[r].get("role_basis") != "unknown"
            for r in refs if r in sources
        )
        if not customer:
            intent_detail.append(f"obs[{index}] target={obs.get('target')!r} refs={refs}")
    add("intent_has_customer_speaker", not intent_detail,
        "; ".join(intent_detail) or "positive intents are customer-spoken")

    scope = card.get("analysis_scope") or {}
    if scope.get("kind") == "non_business":
        conclusion = (card.get("conclusion") or {}).get("current_state") or ""
        obs_kinds = [o.get("kind") for o in card.get("recording_observations") or []]
        add("non_business_label_visible", "非业务" in conclusion and "non_business" in obs_kinds,
            f"conclusion={conclusion!r} kinds={obs_kinds}")
    else:
        add("non_business_label_visible", True, f"scope={scope.get('kind')}")
    return checks


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--batch-dir", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    args = parser.parse_args()

    records = []
    failed = 0
    for packet_path in sorted(args.batch_dir.glob("private-analysis-*.json"),
                              key=lambda p: int(p.stem.rsplit("-", 1)[1])):
        packet = json.loads(packet_path.read_text())
        analysis_id = packet.get("analysis_id")
        checks = check_record(packet)
        record_failed = [c for c in checks if not c["ok"]]
        if record_failed:
            failed += 1
        records.append({
            "analysis_id": analysis_id,
            "pass": not record_failed,
            "failed_checks": record_failed,
            "checks": checks,
        })
    report = {
        "script": "check-display-bounds-v1",
        "script_sha256": sha256(__file__),
        "max_statement_runes": MAX_STATEMENT_RUNES,
        "records": records,
        "summary": {"records": len(records), "pass": len(records) - failed, "fail": failed},
    }
    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_text(json.dumps(report, ensure_ascii=False, indent=2) + "\n")
    print(json.dumps(report["summary"], ensure_ascii=False))
    for record in records:
        if record["failed_checks"]:
            print(f"analysis_id={record['analysis_id']} FAIL "
                  + "; ".join(f"{c['id']}: {c['detail']}" for c in record["failed_checks"]))


if __name__ == "__main__":
    main()
