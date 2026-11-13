#!/usr/bin/env python3
"""Read-only rc12 checks for a real small batch (no model call, no DB write).

rc11's display checker only bounds *per-source* excerpts, so evaluation id 19
published a 257-rune concern made of two concatenated customer quotes. rc12 caps
the whole statement and drops a republished excerpt. It also adds the structured
local_observations channel for timing / willingness / examination fragments on
partially covered or multi-subject recordings (ids 34/40/41).

This checker is additive. It never modifies the frozen v3 oracle, the rc11
display checker or any older expectation file. It reads the expectations file
written *before* the model call, which carries the per-record `rc12_checks`.
"""
import argparse
import base64
import hashlib
import json
import re
from pathlib import Path

STATEMENT_KINDS = ("need", "concern", "trust")
LOCAL_KINDS = ("timing", "willingness", "exam", "symptom", "question", "staff_statement", "other")
MAX_STATEMENT_RUNES = 240
MAX_DISPLAY_RUNES = MAX_STATEMENT_RUNES + 2  # two display ellipses


def sha256(path):
    return hashlib.sha256(Path(path).read_bytes()).hexdigest()


def normalize(text):
    return re.sub(r"\s+", "", text or "")


def strip_ellipsis(text):
    text = text or ""
    if text.startswith("…"):
        text = text[1:]
    if text.endswith("…"):
        text = text[:-1]
    return text


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
    raise ValueError("frozen input not found")


def decode_card(packet):
    raw = (packet.get("compilation") or {}).get("Card")
    return json.loads(base64.b64decode(raw).decode("utf-8")) if raw else None


def observations(card):
    return card.get("recording_observations") or []


def check_record(record_id, card, frozen, expectation, expected_bundle, summary):
    checks = []

    def add(check_id, ok, detail):
        checks.append({"id": check_id, "ok": bool(ok), "detail": detail})

    # rc12.1 is the rc12 repair release for the same defect classes; both carry the
    # rc12 behavior gates, so the family prefix is what this checker requires.
    add("rules_version_rc12", str(card.get("rules_version", "")).startswith("deal-compiler-v2.1.0-rc12"),
        str(card.get("rules_version")))
    if expected_bundle:
        add("bundle_id_expected", (summary.get("batch") or {}).get("bundle_id") == expected_bundle,
            f"{(summary.get('batch') or {}).get('bundle_id')} vs {expected_bundle}")

    statements = [o for o in observations(card) if o.get("kind") in STATEMENT_KINDS]
    over = [f"{o.get('kind')}={len(o.get('summary') or '')}" for o in statements
            if len(o.get("summary") or "") > MAX_DISPLAY_RUNES]
    add("statement_bounded", not over, "; ".join(over) or f"{len(statements)} statements <= {MAX_STATEMENT_RUNES} runes")

    seen = {}
    duplicates = []
    for index, obs in enumerate(statements):
        key = obs.get("summary") or ""
        if key in seen:
            duplicates.append(f"obs[{seen[key]}] == obs[{index}]")
        else:
            seen[key] = index
    # Informational only: a statement excerpt can legitimately contain another
    # short clause from the same utterance (id 24's concern contains its need).
    # id 19's rc11 defect was exactly this shape on a 257-rune concatenation, but
    # turning containment into a hard failure would flag a card the frozen oracle
    # accepts, so it is reported and not scored here.
    overlaps = []
    normalized = [(index, normalize(o.get("summary"))) for index, o in enumerate(statements)]
    for i, a in normalized:
        for j, b in normalized:
            if i == j or len(a) < 12 or len(b) < 12:
                continue
            if a in b and len(b) > len(a):
                overlaps.append(f"obs[{j}] contains obs[{i}]")
    add("statement_unique", not duplicates,
        "; ".join(duplicates) or f"{len(seen)} distinct statements; overlaps={sorted(set(overlaps))}")

    evidence = {e["id"]: e for e in card.get("evidence_refs") or []}
    sources = {s["id"]: s for s in ((frozen.get("input") or {}).get("transcript_sources") or [])}
    not_verbatim = []
    for index, obs in enumerate(statements):
        refs = [evidence[r]["source_ref"] for r in obs.get("evidence_refs") or [] if r in evidence]
        texts = [normalize(sources[r]["text"]) for r in refs if r in sources]
        for part in (obs.get("summary") or "").split("\n"):
            part = normalize(strip_ellipsis(part))
            if part and not any(part in text for text in texts):
                not_verbatim.append(f"obs[{index}] {obs.get('kind')}")
    add("statement_verbatim", not not_verbatim, "; ".join(not_verbatim) or "all excerpts verbatim")

    for expected in expectation.get("rc12_checks") or []:
        if "min_local_observations" in expected:
            local = [o for o in observations(card) if o.get("kind") in LOCAL_KINDS]
            add("rc12:" + expected["id"], len(local) >= expected["min_local_observations"],
                f"local_observations={len(local)} kinds={[o.get('kind') for o in local]}")
            continue
        if "max_statement_runes" in expected:
            limit = expected["max_statement_runes"]
            add("rc12:" + expected["id"],
                MAX_STATEMENT_RUNES <= limit and not over and not duplicates,
                f"limit={limit} max={max((len(o.get('summary') or '') for o in statements), default=0)} "
                f"duplicates={duplicates}")
            continue
        if "kinds" in expected:
            hits = []
            for obs in observations(card):
                if obs.get("kind") not in expected["kinds"]:
                    continue
                text = obs.get("summary") or ""
                hits.extend(word for word in expected.get("any_of") or [] if word in text)
            add("rc12:" + expected["id"], bool(hits),
                f"kinds={expected['kinds']} hits={sorted(set(hits))} desc={expected.get('desc')}")
            continue
        add("rc12:" + expected.get("id"), False, "unknown rc12 check shape")
    return checks, overlaps


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

    records = []
    failed = 0
    for record_id in expectations.get("records") or {}:
        packet_path = args.batch_dir / f"private-analysis-{record_id}.json"
        if not packet_path.exists():
            records.append({"analysis_id": int(record_id), "pass": False,
                            "failed_checks": [{"id": "packet_present", "detail": "missing packet"}]})
            failed += 1
            continue
        packet = json.loads(packet_path.read_text())
        card = decode_card(packet)
        if card is None:
            records.append({"analysis_id": int(record_id), "pass": False,
                            "failed_checks": [{"id": "card_present", "detail": "no card"}]})
            failed += 1
            continue
        frozen = load_input(packet)
        checks, overlaps = check_record(record_id, card, frozen, expectations["records"][record_id],
                                        args.expected_bundle, summary)
        bad = [c for c in checks if not c["ok"]]
        if bad:
            failed += 1
        records.append({"analysis_id": int(record_id), "pass": not bad, "checks": checks,
                        "statement_overlaps": overlaps, "failed_checks": bad})

    report = {
        "script": "check-rc12-batch09",
        "script_sha256": sha256(__file__),
        "expectations_sha256": sha256(args.expectations),
        "batch": (summary.get("batch") or {}).get("batch"),
        "bundle_id": (summary.get("batch") or {}).get("bundle_id"),
        "expected_bundle": args.expected_bundle,
        "max_statement_runes": MAX_STATEMENT_RUNES,
        "records": records,
        "summary": {"records": len(records), "pass": len(records) - failed, "fail": failed},
    }
    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_text(json.dumps(report, ensure_ascii=False, indent=2) + "\n")
    print(json.dumps(report["summary"], ensure_ascii=False))
    for record in records:
        print(f"analysis_id={record['analysis_id']} pass={record['pass']}")
        for failure in record.get("failed_checks") or []:
            print(f"  FAIL {failure['id']}: {failure['detail']}")


if __name__ == "__main__":
    main()
