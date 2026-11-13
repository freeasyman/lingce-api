#!/usr/bin/env python3
"""Run the frozen v2 small-batch checks against an rc10 batch packet.

Why this wrapper exists: rc10 keeps rc9's *user prompt template* and appends two
short semantic reminders after the frozen input JSON, so the v2 loader (which
assumes the input JSON runs to the end of the user message) raises `Extra data`.
This wrapper imports the frozen v2 module unchanged and replaces only that
loader with a `raw_decode`-based one, which parses the leading JSON object and
ignores the trailing rc10 reminders. Every check function, expectation file and
pass/fail rule is the untouched v2 implementation; the v2 script's sha256 is
recorded here as evidence that it was not edited.

Usage mirrors check-real-smallbatch-v2.py.
"""
import argparse
import hashlib
import importlib.util
import json
import sys
from pathlib import Path

V2_PATH = Path(__file__).resolve().parent / "check-real-smallbatch-v2.py"


def load_v2():
    spec = importlib.util.spec_from_file_location("frozen_v2_check", V2_PATH)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def load_input_rc10(packet):
    """Recover the exact frozen input object from an rc10 user message."""
    for event in packet.get("events") or []:
        if event.get("Type") != "request" or not event.get("Payload"):
            continue
        import base64

        request = json.loads(base64.b64decode(event["Payload"]).decode("utf-8"))
        for message in request.get("messages") or []:
            content = message.get("content") or ""
            marker = "请分析以下冻结输入："
            if marker in content:
                remainder = content.split(marker, 1)[1].lstrip("\n")
                value, _ = json.JSONDecoder().raw_decode(remainder)
                return value
    raise ValueError("frozen input not found in request events")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--batch-dir", type=Path, required=True)
    parser.add_argument("--expectations", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--expected-bundle", default="")
    args = parser.parse_args()

    v2 = load_v2()
    v2.load_input = load_input_rc10
    sys.argv = [
        str(V2_PATH),
        "--batch-dir", str(args.batch_dir),
        "--expectations", str(args.expectations),
        "--output", str(args.output),
    ]
    if args.expected_bundle:
        sys.argv += ["--expected-bundle", args.expected_bundle]
    v2.main()

    report = json.loads(args.output.read_text())
    report["script"] = "check-real-smallbatch-rc10 (wrapper)"
    report["wrapper_sha256"] = hashlib.sha256(Path(__file__).read_bytes()).hexdigest()
    report["frozen_v2_sha256"] = hashlib.sha256(V2_PATH.read_bytes()).hexdigest()
    report["wrapper_reason"] = "rc10 appends wording-strength and symptom-type reminders after the frozen input JSON; only the v2 input loader is replaced, no rule/expectation change"
    args.output.write_text(json.dumps(report, ensure_ascii=False, indent=2) + "\n")


if __name__ == "__main__":
    main()
