#!/usr/bin/env python3
"""为 cmd/his-price-import 生成拼音表（name<TAB>pinyin<TAB>initials），与既有 charge_pinyin.tsv 约定一致：
全拼小写无音调、保留括号与拉丁字母，首字母串取每个音节首字母。

用法：
  python3 scripts/his_price_pinyin.py tmp/price-import-report.csv > tmp/price_pinyin.tsv
"""
import csv
import sys

from pypinyin import lazy_pinyin


def to_pinyin(name: str):
    parts = [p.lower() for p in lazy_pinyin(name, errors="default")]
    pinyin = "".join(parts)
    initials = "".join(p[0] for p in parts if p)
    return pinyin, initials


def main() -> int:
    src, out = sys.argv[1], sys.stdout
    seen = set()
    with open(src, encoding="utf-8") as f:
        for row in csv.DictReader(f):
            name = (row.get("name") or "").strip()
            if not name or name in seen:
                continue
            seen.add(name)
            py, ini = to_pinyin(name)
            out.write(f"{name}\t{py}\t{ini}\n")
    return 0


if __name__ == "__main__":
    sys.exit(main())
