#!/usr/bin/env python3
"""Self-test for the v3 negation-aware forbid matcher (no batch, no model)."""
import importlib.util
from pathlib import Path

PATH = Path(__file__).resolve().parent / "check-real-smallbatch-v3.py"
spec = importlib.util.spec_from_file_location("check_v3", PATH)
v3 = importlib.util.module_from_spec(spec)
spec.loader.exec_module(v3)

CASES = [
    # (text, pattern, expected_hit)
    ("非客户已预约。", "已预约", False),
    ("不是已预约。", "已预约", False),
    ("并未确认已预约。", "已预约", False),
    ("不等于已预约。", "已预约", False),
    ("时间安排为录音级观察，不等于已预约。", "已预约", False),
    ("客户已预约。", "已预约", True),
    ("非客户已预约，但已排期。", "已排期", True),
    ("非客户已预约，但已排期。", "已预约", False),
    ("未来已预约（假设）。", "已预约", True),
    ("已预约，不是已复诊。", "已预约", True),
    ("已预约，不是已复诊。", "已复诊", False),
]

def hit(text, pattern):
    return v3.negated_at(text, text.index(pattern)) is False

failures = []
for text, pattern, expected in CASES:
    actual = hit(text, pattern)
    if actual != expected:
        failures.append({"text": text, "pattern": pattern, "expected": expected, "actual": actual})
print({"cases": len(CASES), "pass": len(CASES) - len(failures), "fail": len(failures), "failures": failures})
raise SystemExit(1 if failures else 0)
