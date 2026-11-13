#!/usr/bin/env python3
"""Counterfactual recompilation: rc10 saved responses under rc10 vs rc11.

No model call. For every saved packet in a batch directory it extracts the
frozen snapshot bytes and the exact first saved response, then runs the local
`deal-v2-compile` binary twice. It reports:

  * whether the rc10 recompile still equals the saved rc10 card byte-for-byte;
  * the rc11 card's intent observations, statement lengths and diagnostics;
  * a structural diff count between the two cards.

This is evidence for compiler-level changes only. A prompt-only fix (for example
id 17's non-business analysis_scope) cannot appear here because the old response
has no such field; that needs a new model call and is reported as such.
"""
import argparse
import base64
import hashlib
import json
import subprocess
import tempfile
from pathlib import Path


def sha256(path):
    return hashlib.sha256(Path(path).read_bytes()).hexdigest()


def run(compiler, snapshot, intermediate, rules):
    proc = subprocess.run(
        [str(compiler), "--snapshot", str(snapshot), "--intermediate", str(intermediate), "--rules", rules],
        capture_output=True, text=True,
    )
    if proc.returncode != 0:
        return {"ok": False, "stderr": proc.stderr.strip(), "exit_code": proc.returncode}
    return {"ok": True, "result": json.loads(proc.stdout)}


def first_response(packet):
    for event in packet.get("events") or []:
        if event.get("Type") == "response" and event.get("RawContent"):
            return event["RawContent"].encode("utf-8")
    return None


def saved_card(packet):
    raw = (packet.get("compilation") or {}).get("Card")
    return json.loads(base64.b64decode(raw).decode("utf-8")) if raw else None


def card_metrics(card):
    intent = []
    statements = []
    for obs in card.get("recording_observations") or []:
        kind = obs.get("kind")
        if kind in ("intent", "staff_claim"):
            intent.append({"kind": kind, "summary": obs.get("summary"), "target": obs.get("target"),
                           "refs": obs.get("evidence_refs")})
        if kind in ("need", "concern", "trust"):
            statements.append({"kind": kind, "runes": len(obs.get("summary") or "")})
    return {
        "rules_version": card.get("rules_version"),
        "analysis_scope": card.get("analysis_scope"),
        "intents": intent,
        "statement_lengths": statements,
        "diagnostic_codes": sorted({d.get("code") for d in card.get("diagnostics") or []}),
    }


def diff_count(a, b, path="", depth=0):
    """Structural leaf differences, capped detail for the report."""
    if type(a) is not type(b):
        return 1
    if isinstance(a, dict):
        total = 0
        for key in set(a) | set(b):
            if key not in a or key not in b:
                total += 1
            else:
                total += diff_count(a[key], b[key], path + "/" + str(key), depth + 1)
        return total
    if isinstance(a, list):
        return 0 if a == b else 1
    return 0 if a == b else 1


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--batch-dir", type=Path, required=True)
    parser.add_argument("--compiler", type=Path, required=True)
    parser.add_argument("--rules-a", default="deal-compiler-v2.1.0-rc10")
    parser.add_argument("--rules-b", default="deal-compiler-v2.1.0-rc11")
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--materialize", type=Path, default=None,
                        help="optional dir for derived packets carrying the rules-b card")
    args = parser.parse_args()
    if args.materialize is not None:
        args.materialize.mkdir(parents=True, exist_ok=True)

    records = []
    for packet_path in sorted(args.batch_dir.glob("private-analysis-*.json"),
                              key=lambda p: int(p.stem.rsplit("-", 1)[1])):
        packet = json.loads(packet_path.read_text())
        analysis_id = packet.get("analysis_id")
        manifest = json.loads(packet["manifest_raw"])
        intermediate = first_response(packet)
        saved = saved_card(packet)
        with tempfile.TemporaryDirectory(prefix=f"rc11-cf-{analysis_id}-") as tmp:
            snapshot_path = Path(tmp) / "snapshot.json"
            intermediate_path = Path(tmp) / "intermediate.json"
            snapshot_path.write_bytes(base64.b64decode(manifest["snapshot_bytes"]))
            intermediate_path.write_bytes(intermediate)
            a = run(args.compiler, snapshot_path, intermediate_path, args.rules_a)
            b = run(args.compiler, snapshot_path, intermediate_path, args.rules_b)
        a_card = a.get("result", {}).get("card") if a.get("ok") else None
        b_card = b.get("result", {}).get("card") if b.get("ok") else None
        a_matches_saved = a_card == saved if a_card is not None else False
        if a_card is not None:
            a_card = dict(a_card, diagnostics=a["result"].get("diagnostics") or [])
        if b_card is not None:
            b_card = dict(b_card, diagnostics=b["result"].get("diagnostics") or [])
        if args.materialize is not None and b.get("ok"):
            derived = dict(packet)
            derived["compilation"] = dict(packet.get("compilation") or {})
            derived["compilation"]["Card"] = base64.b64encode(
                json.dumps(b["result"]["card"], ensure_ascii=False).encode("utf-8")).decode("ascii")
            derived["compilation"]["Diagnostics"] = base64.b64encode(
                json.dumps(b["result"].get("diagnostics") or [], ensure_ascii=False).encode("utf-8")).decode("ascii")
            derived["compilation"]["CompilerVersion"] = args.rules_b
            (args.materialize / f"private-analysis-{analysis_id}.json").write_text(
                json.dumps(derived, ensure_ascii=False))
        records.append({
            "analysis_id": analysis_id,
            "rules_a_ok": a.get("ok"),
            "rules_a_matches_saved": a_matches_saved,
            "rules_b_ok": b.get("ok"),
            "rules_b_error": b.get("stderr") if not b.get("ok") else "",
            "structural_diff_a_vs_b": diff_count(a_card, b_card) if a_card is not None and b_card is not None else None,
            "metrics_a": card_metrics(a_card) if a_card else None,
            "metrics_b": card_metrics(b_card) if b_card else None,
            "snapshot_sha256": hashlib.sha256(base64.b64decode(manifest["snapshot_bytes"])).hexdigest(),
        })
    report = {
        "script": "check-rc11-counterfactual",
        "script_sha256": sha256(__file__),
        "compiler_sha256": sha256(args.compiler),
        "rules_a": args.rules_a,
        "rules_b": args.rules_b,
        "model_calls": 0,
        "records": records,
        "summary": {
            "records": len(records),
            "rules_a_matches_saved": sum(1 for r in records if r["rules_a_matches_saved"]),
            "rules_b_ok": sum(1 for r in records if r["rules_b_ok"]),
        },
    }
    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_text(json.dumps(report, ensure_ascii=False, indent=2) + "\n")
    if args.materialize is not None:
        (args.materialize / "summary.json").write_text(json.dumps({
            "batch": {"batch": "rc11-counterfactual-from-batch05", "bundle_id": "counterfactual",
                      "model": {"model_code": args.rules_b}},
        }, ensure_ascii=False, indent=2) + "\n")
    print(json.dumps(report["summary"], ensure_ascii=False))
    for record in records:
        print(f"analysis_id={record['analysis_id']} rc10_saved={record['rules_a_matches_saved']} "
              f"rc11_ok={record['rules_b_ok']} diff={record['structural_diff_a_vs_b']}")


if __name__ == "__main__":
    main()
