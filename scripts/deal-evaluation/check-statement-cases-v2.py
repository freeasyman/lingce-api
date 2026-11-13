#!/usr/bin/env python3
"""Statement-case assertions v2 — versioned addition; the v1 script is kept unchanged.

版本/依据
--------
- 脚本：check-statement-cases-v2，版本 2026-11-01（v2 首版）。
- 依据：`lingce-test-environment/case2没时间漏报根因定位.md` 第 4、7.2、8.1 节。
  根因是验收投影缺口：v1 只统计 concerns/trust/need，从不读取 condition 通道，
  因此把“已保留在 condition 的客户现实限制”判成 concerns=False。
- 业务口径（用户已拍板方案 A）：不修改 prompt/编译器/bundle，不重新调用模型，
  只把 case2 的断言从“单槽位 concerns 必须为真”改为“信息等价”的并集谓词，
  并保留旧 EXPECTED/旧报告不变。

与 v1 的差异（v1 = scripts/deal-evaluation/check-statement-cases.py，未改动）
--------------------------------------------------------------------------
1. case2 不再要求 `concerns=True`，改为以下任意一条成立即为“保留了没时间”，
   且三条都必须把证据绑定到 `speaker=客户 / subject_binding=target_customer` 的来源：
     a) card.decision / scheme_decisions 的 condition_kind=practical_constraint，
        且 condition_due_quote 含“现在没时间”；
     b) 任一方案 concern 的卡片分类属于约束类（时间与流程/其他），其证据原文含
        “现在没时间”——v1 期望的单槽位写法；注意 v2.1 卡片 schema 不保留
        模型的 statement_type（customer_constraint），只能用卡片分类 + 完整原句近似，
        报告里会注明该限制；
     c) recording_observations 中存在 kind=condition、subject_binding=target_customer，
        且 summary 或其证据原文含“现在没时间”。
2. 新增 case2 完整原句校验：上述命中通道必须携带与冻结输入客户原句逐字相等的引文，
   且该原句同时含“不害怕”和“没时间”。v1 的“不害怕”检查只遍历 all_concerns，
   case2 的 concerns 为空时该检查空转；v2 对命中通道强制校验，不再空转。
3. 新增 case2 恐惧误报的否定语境判断：原文本身就是“我不害怕手术”，不能因为出现
   “害怕”二字就算误报；v2 只把“非否定语境”的恐惧词（恐惧/害怕/惧怕/畏惧/怕手术）
   判为误报。覆盖 concerns、trust、need、condition、condition_due_quote 与录音观察
   summary。v1 的字符串包含检查会被“我不害怕”直接满足，既不空转也不可靠。
4. 其余 5 条（1/3/4/5/6）的期望与检查逻辑逐条沿用 v1（LEGACY_EXPECTED 仅作对照
   复制，v1 文件未改），并保留 v1 的“展示语句必须等于完整冻结原句”“录音观察不得
   夹带被禁止字段”“不得虚构完成事件/销售动作”等检查。
5. 报告增加：脚本版本与自身 sha256、旧脚本 sha256、被断言文件清单与 sha256、
   阶段与批次、每条结果、case2 三条并集谓词的逐项判定依据、完整原句与恐惧否定语境结果。

本脚本只读取已保存的 private-analysis-*.json，不调用模型、不编译、不写共享库。
"""
from __future__ import annotations

import argparse
import base64
import hashlib
import json
import re
from datetime import datetime
from pathlib import Path

SCRIPT_ID = "check-statement-cases-v2"
SCRIPT_VERSION = "2026-11-01"
REPORT_SCHEMA = "deal-statement-cases-report-v2"
BATCH = "phase4-v210-synthetic-20261101-03"

LEGACY_SCRIPT = Path(
    "/Users/yiliiang/Documents/lingce-api/scripts/deal-evaluation/check-statement-cases.py"
)
BASIS_DOC = Path(
    "/Users/yiliiang/Documents/deepseek-harness/default-workspace/"
    "lingce-test-environment/case2没时间漏报根因定位.md"
)

# v1 的 EXPECTED 原样复制，仅用于报告对照；v1 文件本身没有修改。
LEGACY_EXPECTED = {
    1: dict(concerns=False, trust=False, need=False),
    2: dict(concerns=True, trust=False, need=False),
    3: dict(concerns=False, trust=False),
    4: dict(concerns=True, trust=False),
    5: dict(need=True, trust=True),
    6: dict(trust=False),
}

# v2 仍强制检查的字段。case2 的 concerns 被并集谓词替代，trust/need 仍强制为 False。
HARD_EXPECTED = {
    1: dict(concerns=False, trust=False, need=False),
    3: dict(concerns=False, trust=False),
    4: dict(concerns=True, trust=False),
    5: dict(need=True, trust=True),
    6: dict(trust=False),
}
CASE2_HARD = dict(trust=False, need=False)

# v2.1 卡片 schema 只保留 9 个展示分类，模型的 customer_constraint 不落卡；
# 可承载“客户现实限制”的分类按“时间与流程/其他”近似，报告会记录该限制。
CUSTOMER_CONSTRAINT_CARD_TYPES = ("时间与流程", "其他")

FEAR_TOKENS = ("恐惧", "害怕", "惧怕", "畏惧", "怕手术")
NEGATION_RE = re.compile(r"(?:不|没(?:有)?|并不|绝不|从不|从未|不用|不必|无需|别)[^，。！？；、,.;!?]{0,3}$")
CLAUSE_SPLIT = "，。！？；、,.;!?"
DISPLAY_KINDS = ("concern", "trust", "need", "condition")


def sha256_bytes(raw: bytes) -> str:
    return "sha256:" + hashlib.sha256(raw).hexdigest()


def load_sources(manifest_raw: str):
    manifest = json.loads(manifest_raw)
    snapshot = json.loads(base64.b64decode(manifest["snapshot_bytes"]))
    return snapshot


def customer_bound(source) -> bool:
    return source.get("speaker") == "客户" and source.get("subject_binding") == "target_customer"


def resolve_refs(evidence, refs):
    """Return (resolved evidence rows, unresolved ref ids)."""
    rows, missing = [], []
    for ref in refs or []:
        ev = evidence.get(ref)
        if ev is None:
            missing.append(ref)
        else:
            rows.append(ev)
    return rows, missing


def positive_fear_claims(text: str):
    """Return excerpts where a fear word is used without a preceding negation.

    “我不害怕手术” is negated and must NOT be reported. Clause punctuation resets
    the negation window so “不，我害怕” is still a positive claim.
    """
    hits = []
    pattern = "|".join(FEAR_TOKENS)
    for match in re.finditer(pattern, text):
        start = match.start()
        prefix = text[:start]
        cut = max((prefix.rfind(ch) for ch in CLAUSE_SPLIT), default=-1)
        prefix = prefix[cut + 1:]
        if NEGATION_RE.search(prefix):
            continue
        hits.append(text[max(0, start - 6):match.end() + 2])
    return hits


def displayed_texts(card):
    """All card-authored display texts that could misreport a customer feeling."""
    texts = []
    decisions = [("decision", card["decision"])] + [
        (f"scheme_decisions[{i}]", d) for i, d in enumerate(card["scheme_decisions"])
    ]
    for path, d in decisions:
        for j, c in enumerate(d.get("concerns") or []):
            texts.append((f"{path}.concerns[{j}].summary", c["summary"]))
        for j, t in enumerate(d.get("trust") or []):
            texts.append((f"{path}.trust[{j}].summary", t["summary"]))
        if d.get("need_summary"):
            texts.append((f"{path}.need_summary", d["need_summary"]))
        if d.get("condition"):
            texts.append((f"{path}.condition", d["condition"]))
        if d.get("condition_due_quote"):
            texts.append((f"{path}.condition_due_quote", d["condition_due_quote"]))
    for i, obs in enumerate(card.get("recording_observations") or []):
        if obs.get("kind") in DISPLAY_KINDS:
            texts.append((f"recording_observations[{i}]({obs['kind']})", obs.get("summary") or ""))
    seen, unique = set(), []
    for path, text in texts:
        if (path, text) not in seen:
            seen.add((path, text))
            unique.append((path, text))
    return unique


def legacy_checks(card, case, sources, evidence):
    """Checks copied from v1 (kept byte-for-byte in behavior) for all cases."""
    errors = []
    decisions = card["scheme_decisions"]
    all_concerns = [r for d in decisions for r in d["concerns"]]
    all_trust = [r for d in decisions for r in d["trust"]]
    all_needs = [d for d in decisions if d["need_summary"] is not None]
    values = dict(
        concerns=bool(all_concerns), trust=bool(all_trust), need=bool(all_needs)
    )
    rows = [(r["summary"], r["evidence_refs"]) for r in all_concerns + all_trust]
    rows += [(d["need_summary"], d["need_evidence_refs"]) for d in all_needs]
    for r in card["recording_observations"]:
        if r["kind"] in ("need", "concern", "trust"):
            rows.append((r["summary"], r["evidence_refs"]))
            field = {"concern": "concerns", "trust": "trust", "need": "need"}[r["kind"]]
            if LEGACY_EXPECTED[case].get(field) is False:
                errors.append(f"forbidden {field} escaped via recording observations")
    for text, refs in rows:
        evs, missing = resolve_refs(evidence, refs)
        if missing:
            errors.append(f"unknown evidence refs {missing}")
            continue
        exact = "\n".join(sources[ev["source_ref"]]["text"] for ev in evs)
        if text != exact:
            errors.append("displayed statement differs from complete frozen utterances")
    if case == 4:
        if not any("我害怕手术" in r["summary"] for r in all_concerns):
            errors.append("explicit fear was dropped")
    if card["business_facts"] or card["event_reports"]:
        errors.append("invented completed/reported event")
    if card["primary_action"] and card["primary_action"]["type"] == "sales_follow_up":
        errors.append("sales action without contact permission")
    return values, errors


def case2_predicate(card, sources, evidence):
    """Union predicate for case2 + complete-sentence and negation-context checks."""
    errors = []
    detail = {
        "version_basis": "case2没时间漏报根因定位.md §4/§7.2/§8.1；用户拍板方案 A",
        "clauses": {
            "a_condition_practical_constraint": {"matched": False, "objects": [], "rejected": []},
            "b_concern_customer_constraint": {"matched": False, "objects": [], "rejected": []},
            "c_recording_observation_condition": {"matched": False, "objects": [], "rejected": []},
        },
        "complete_sentence": {},
        "fear_negation": {},
    }

    # The frozen customer utterance that carries the real constraint. We do not
    # hardcode the text; it is re-read from the packet's frozen snapshot.
    candidates = [s for s in sources.values() if "现在没时间" in s["text"]]
    complete = [s for s in candidates if "不害怕" in s["text"] and "没时间" in s["text"]]
    complete_customer = [s for s in complete if customer_bound(s)]
    detail["complete_sentence"]["frozen_candidate_source_ids"] = [s["id"] for s in candidates]
    detail["complete_sentence"]["complete_source_ids"] = [s["id"] for s in complete]
    detail["complete_sentence"]["customer_complete_source_ids"] = [
        s["id"] for s in complete_customer
    ]
    if not complete_customer:
        errors.append(
            "case2 complete original sentence check: frozen input has no customer source "
            "containing both 不害怕 and 没时间"
        )
        detail["complete_sentence"]["exact"] = False
        expected_texts = set()
    else:
        expected_texts = {s["text"] for s in complete_customer}
        detail["complete_sentence"]["text"] = sorted(expected_texts)
        detail["complete_sentence"]["has_bu_haipa"] = all(
            "不害怕" in t for t in expected_texts
        )
        detail["complete_sentence"]["has_meishijian"] = all(
            "没时间" in t for t in expected_texts
        )

    matched_quotes = []
    decisions = [("decision", card["decision"])] + [
        (f"scheme_decisions[{i}]", d) for i, d in enumerate(card["scheme_decisions"])
    ]

    # (a) compiled condition channel.
    for path, d in decisions:
        kind = d.get("condition_kind")
        due = d.get("condition_due_quote") or ""
        refs = d.get("condition_evidence_refs") or []
        evs, missing = resolve_refs(evidence, refs)
        bound = bool(evs) and all(customer_bound(sources[ev["source_ref"]]) for ev in evs)
        ok = kind == "practical_constraint" and "现在没时间" in due and bound and not missing
        record = {
            "path": path,
            "condition_kind": kind,
            "condition_due_quote": due,
            "condition_evidence_refs": refs,
            "refs_resolve": not missing,
            "refs_customer_bound": bound,
        }
        if ok:
            detail["clauses"]["a_condition_practical_constraint"]["matched"] = True
            detail["clauses"]["a_condition_practical_constraint"]["objects"].append(record)
            matched_quotes.append((f"{path}.condition_due_quote", due))
        else:
            record["failed_on"] = [
                name
                for name, value in (
                    ("condition_kind is practical_constraint", kind == "practical_constraint"),
                    ("condition_due_quote contains 现在没时间", "现在没时间" in due),
                    ("evidence refs resolve", not missing),
                    ("evidence bound to target_customer", bound),
                )
                if not value
            ]
            detail["clauses"]["a_condition_practical_constraint"]["rejected"].append(record)

    # (b) concern channel (v1's single-slot expectation).
    for path, d in decisions:
        for j, c in enumerate(d.get("concerns") or []):
            refs = c.get("evidence_refs") or []
            evs, missing = resolve_refs(evidence, refs)
            bound = bool(evs) and all(customer_bound(sources[ev["source_ref"]]) for ev in evs)
            quote_hit = any("现在没时间" in ev.get("quote", "") for ev in evs)
            type_ok = c.get("type") in CUSTOMER_CONSTRAINT_CARD_TYPES
            ok = (
                type_ok
                and "没时间" in c.get("summary", "")
                and quote_hit
                and bound
                and not missing
            )
            record = {
                "path": f"{path}.concerns[{j}]",
                "type": c.get("type"),
                "summary": c.get("summary"),
                "evidence_refs": refs,
                "refs_customer_bound": bound,
                "evidence_quote_contains_现在没时间": quote_hit,
                "note": "card schema drops statement_type; category-level approximation",
            }
            if ok:
                detail["clauses"]["b_concern_customer_constraint"]["matched"] = True
                detail["clauses"]["b_concern_customer_constraint"]["objects"].append(record)
                matched_quotes.append((f"{path}.concerns[{j}].summary", c.get("summary", "")))
                matched_quotes.extend(
                    (f"{path}.concerns[{j}].evidence[{k}]", ev.get("quote", ""))
                    for k, ev in enumerate(evs)
                )
            else:
                record["failed_on"] = [
                    name
                    for name, value in (
                        ("type in constraint categories", type_ok),
                        ("summary contains 没时间", "没时间" in c.get("summary", "")),
                        ("evidence quote contains 现在没时间", quote_hit),
                        ("evidence bound to target_customer", bound),
                        ("evidence refs resolve", not missing),
                    )
                    if not value
                ]
                detail["clauses"]["b_concern_customer_constraint"]["rejected"].append(record)

    # (c) recording-level condition observation channel.
    for i, obs in enumerate(card.get("recording_observations") or []):
        if obs.get("kind") != "condition":
            continue
        refs = obs.get("evidence_refs") or []
        evs, missing = resolve_refs(evidence, refs)
        bound = bool(evs) and all(customer_bound(sources[ev["source_ref"]]) for ev in evs)
        hit = "现在没时间" in (obs.get("summary") or "") or any(
            "现在没时间" in ev.get("quote", "") for ev in evs
        )
        ok = (
            obs.get("subject_binding") == "target_customer"
            and hit
            and bound
            and not missing
        )
        record = {
            "path": f"recording_observations[{i}]",
            "kind": obs.get("kind"),
            "subject_binding": obs.get("subject_binding"),
            "summary": obs.get("summary"),
            "evidence_refs": refs,
            "refs_customer_bound": bound,
            "contains_现在没时间": hit,
        }
        if ok:
            detail["clauses"]["c_recording_observation_condition"]["matched"] = True
            detail["clauses"]["c_recording_observation_condition"]["objects"].append(record)
            matched_quotes.extend(
                (f"recording_observations[{i}].evidence[{k}]", ev.get("quote", ""))
                for k, ev in enumerate(evs)
            )
        else:
            record["failed_on"] = [
                name
                for name, value in (
                    ("subject_binding is target_customer", obs.get("subject_binding") == "target_customer"),
                    ("summary or evidence contains 现在没时间", hit),
                    ("evidence bound to target_customer", bound),
                    ("evidence refs resolve", not missing),
                )
                if not value
            ]
            detail["clauses"]["c_recording_observation_condition"]["rejected"].append(record)

    matched = [
        name
        for name, value in detail["clauses"].items()
        if value["matched"]
    ]
    detail["matched_clauses"] = matched
    if not matched:
        errors.append(
            "case2 real_constraint_preserved: no union clause matched "
            "(a condition practical_constraint / b concern customer_constraint / "
            "c recording_observation condition); see case2_detail.clauses"
        )

    # Complete original sentence must be carried by the matched channel.
    if matched and expected_texts:
        exact = [(path, quote) for path, quote in matched_quotes if quote in expected_texts]
        detail["complete_sentence"]["matched_exact_quotes"] = [
            {"path": path, "quote": quote} for path, quote in exact
        ]
        detail["complete_sentence"]["exact"] = bool(exact)
        if not exact:
            errors.append(
                "case2 complete original sentence check: matched channel does not carry "
                f"a quote equal to the customer utterance {sorted(expected_texts)}"
            )

    # Negation-aware fear check. The frozen sentence itself is negated and must not
    # be treated as a fear report; only non-negated fear words are failures.
    fear_hits = []
    for path, text in displayed_texts(card):
        for excerpt in positive_fear_claims(text):
            fear_hits.append({"path": path, "text": text, "excerpt": excerpt})
    detail["fear_negation"] = {
        "policy": "fear words (恐惧/害怕/惧怕/畏惧/怕手术) fail only without a preceding negation",
        "checked_texts": len(displayed_texts(card)),
        "positive_fear_claims": fear_hits,
        "negated_fear_quotes": [
            {"path": path, "text": text}
            for path, text in displayed_texts(card)
            if any(token in text for token in FEAR_TOKENS)
            and not positive_fear_claims(text)
        ],
    }
    if fear_hits:
        errors.append(
            "case2 fear misreport: positive (non-negated) fear claim in "
            + ", ".join(sorted({h["path"] for h in fear_hits}))
        )

    return errors, detail


def check_packet(packet, case, path):
    compilation = packet.get("compilation")
    if not compilation:
        return dict(case=case, packet=str(path), passed=False, errors=["no completed compilation"]), None

    card = json.loads(base64.b64decode(compilation["Card"]))
    snapshot = load_sources(packet["manifest_raw"])
    sources = {r["id"]: r for r in snapshot["input"]["transcript_sources"]}
    evidence = {r["id"]: r for r in card["evidence_refs"]}
    errors = []

    values, legacy_errors = legacy_checks(card, case, sources, evidence)
    errors.extend(legacy_errors)
    for key, wanted in HARD_EXPECTED.get(case, {}).items():
        if values[key] != wanted:
            errors.append(f"{key}: expected {wanted}, got {values[key]}")
    if case == 2:
        for key, wanted in CASE2_HARD.items():
            if values[key] != wanted:
                errors.append(f"{key}: expected {wanted}, got {values[key]}")

    detail = None
    if case == 2:
        extra_errors, detail = case2_predicate(card, sources, evidence)
        errors.extend(extra_errors)

    result = dict(
        case=case,
        packet=str(path),
        passed=not errors,
        errors=errors,
        observed=dict(
            concerns=values["concerns"],
            trust=values["trust"],
            need=values["need"],
            condition_present=bool(card["decision"].get("condition")),
            condition_kind=card["decision"].get("condition_kind"),
            condition_due_quote=card["decision"].get("condition_due_quote"),
            concern_count=sum(len(d.get("concerns") or []) for d in card["scheme_decisions"]),
            recording_condition_count=sum(
                1 for o in card.get("recording_observations") or [] if o.get("kind") == "condition"
            ),
        ),
        legacy_expected=LEGACY_EXPECTED[case],
        hard_expected=HARD_EXPECTED.get(case, {}),
    )
    if case == 2:
        result["case2_detail"] = detail
        if result["passed"]:
            matched = detail["matched_clauses"]
            result["why_pass"] = (
                "口径修订（非模型修复）："
                + " + ".join(
                    {
                        "a_condition_practical_constraint": "a) condition.kind=practical_constraint 且 condition_due_quote 含“现在没时间”，证据指向客户原句",
                        "b_concern_customer_constraint": "b) concern 属约束类且证据原文含“现在没时间”",
                        "c_recording_observation_condition": "c) recording_observations 有 kind=condition 且含“现在没时间”",
                    }[name]
                    for name in matched
                )
                + "；完整原句同时含“不害怕”“没时间”；无肯定语境恐惧误报"
            )
    return result, card


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("packets", type=Path, help="frozen private-analysis-*.json directory")
    parser.add_argument("--output", required=True, type=Path)
    parser.add_argument("--old-script", type=Path, default=LEGACY_SCRIPT)
    args = parser.parse_args()

    results = []
    packet_rows = []
    summary = None
    summary_path = args.packets / "summary.json"
    if summary_path.exists():
        summary = json.loads(summary_path.read_text())
    summary_cards = {}
    if summary:
        for row in summary.get("results", []):
            summary_cards[row.get("analysis_id")] = row

    for case in LEGACY_EXPECTED:
        path = args.packets / f"private-analysis-{case}.json"
        if not path.exists():
            results.append(
                dict(case=case, packet=str(path), passed=False, errors=["missing packet"])
            )
            continue
        raw = path.read_bytes()
        packet = json.loads(raw)
        result, card = check_packet(packet, case, path)
        results.append(result)
        integrity = {}
        if card is not None:
            recorded = summary_cards.get(case, {})
            recorded_card_hash = recorded.get("card_hash")
            own_card_hash = packet["compilation"].get("CardHash")
            integrity = {
                "analysis_id": packet.get("analysis_id"),
                "analysis_id_matches_case": packet.get("analysis_id") == case,
                "rules_version": card.get("rules_version"),
                "compiler_version": packet["compilation"].get("CompilerVersion"),
                "contract_hash": packet["compilation"].get("ContractHash"),
                "card_hash": own_card_hash,
                "batch_summary_card_hash": recorded_card_hash,
                "batch_summary_bundle_id": recorded.get("bundle_id"),
                "batch_summary_replay_equal": recorded.get("replay_equal"),
                "card_hash_matches_batch_summary": (
                    None
                    if recorded_card_hash is None
                    else recorded_card_hash == own_card_hash
                ),
            }
            if integrity["card_hash_matches_batch_summary"] is False:
                result["errors"].append(
                    "packet card hash does not match the batch summary (saved data integrity)"
                )
                result["passed"] = False
        packet_rows.append(
            dict(
                case=case,
                path=str(path),
                sha256=sha256_bytes(raw),
                bytes=len(raw),
                **integrity,
            )
        )

    report = dict(
        schema=REPORT_SCHEMA,
        status="PASS" if all(r["passed"] for r in results) else "FAIL",
        generated_at=datetime.now().astimezone().isoformat(timespec="seconds"),
        script=dict(
            id=SCRIPT_ID,
            version=SCRIPT_VERSION,
            path=str(Path(__file__).resolve()),
            sha256=sha256_bytes(Path(__file__).read_bytes()),
            supersedes="check-statement-cases.py (kept unchanged)",
            legacy_script_sha256=sha256_bytes(args.old_script.read_bytes())
            if args.old_script.exists()
            else None,
        ),
        basis=dict(
            document=str(BASIS_DOC),
            sha256=sha256_bytes(BASIS_DOC.read_bytes()) if BASIS_DOC.exists() else None,
            decision="用户拍板方案 A：只修断言，不改模型/prompt/编译器，不重采",
        ),
        batch=BATCH,
        packets_dir=str(args.packets),
        packets=packet_rows,
        scope="Six frozen synthetic dialogues compiled by deal-compiler-v2.1.0-rc7; "
        "no model calls, no recompilation, no database writes.",
        criteria=dict(
            legacy_expected=LEGACY_EXPECTED,
            hard_expected={str(k): v for k, v in HARD_EXPECTED.items()},
            case2_hard={str(k): v for k, v in CASE2_HARD.items()},
            case2_union=dict(
                a="card.decision/scheme_decisions condition_kind=practical_constraint, "
                "condition_due_quote contains 现在没时间, evidence bound to target_customer",
                b="concern type in 时间与流程/其他, summary contains 没时间, evidence quote "
                "contains 现在没时间, evidence bound to target_customer (card drops statement_type)",
                c="recording_observations kind=condition, subject_binding=target_customer, "
                "summary or evidence contains 现在没时间",
                complete_sentence="matched channel carries a quote equal to the frozen customer "
                "utterance that contains both 不害怕 and 没时间",
                fear="no positive (non-negated) fear claim in displayed statements",
            ),
            legacy_checks_kept=[
                "displayed concerns/trust/need must equal complete frozen utterances",
                "recording observations of kind need/concern/trust follow the same expectations",
                "no forbidden field may escape via recording observations",
                "no invented completed/reported business facts",
                "no sales follow-up action without contact permission",
                "case4 must keep explicit fear",
            ],
        ),
        results=results,
    )
    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_text(json.dumps(report, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")

    compact = {
        "status": report["status"],
        "script_version": SCRIPT_VERSION,
        "generated_at": report["generated_at"],
        "output": str(args.output),
        "cases": [
            {"case": r["case"], "passed": r["passed"], "errors": r["errors"]} for r in results
        ],
        "case2": {
            "matched_clauses": results[1].get("case2_detail", {}).get("matched_clauses"),
            "complete_sentence_exact": results[1]
            .get("case2_detail", {})
            .get("complete_sentence", {})
            .get("exact"),
            "positive_fear_claims": results[1]
            .get("case2_detail", {})
            .get("fear_negation", {})
            .get("positive_fear_claims"),
            "why_pass": results[1].get("why_pass"),
        },
    }
    print(json.dumps(compact, ensure_ascii=False, indent=2))
    raise SystemExit(0 if report["status"] == "PASS" else 1)


if __name__ == "__main__":
    main()
