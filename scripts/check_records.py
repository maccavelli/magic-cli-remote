#!/usr/bin/env python3
"""Record and docs-link tooling for the fixed docs tree (PLAN 0175 / 0180).

Modes:
  --next         print the next unused NNNN across the whole repository
  --check        validate numbered records: filename shape, number pairing,
                 placement, and relative markdown links
  --check-all    --check plus link checks for unnumbered docs
                 (docs/guides/**, docs/README.md, docs/architecture.md,
                 root README.md, and apps/mobile/docs/ README, architecture,
                 and guides/** when those paths exist)
  --write-index  regenerate the records ToC in docs/README.md between the
                 generated markers; never hand-edit between them

Errors print to stdout, warnings to stderr; exit status is 1 iff errors were
found. Warnings: a number claimed by more than one MADR, a PLAN without a
same-number MADR, a GATES without a same-number PLAN, and records sitting
outside their kind directory.
"""

from __future__ import annotations

import argparse
import os
import re
import sys
from pathlib import Path

REPO = Path(__file__).resolve().parent.parent
RECORD_RE = re.compile(r"^(\d{4})-(MADR|PLAN|REPORT|GATES)-(.+)\.md$")
LINK_RE = re.compile(r"\]\(([^)]+)\)")
SCHEME_RE = re.compile(r"^[a-zA-Z][a-zA-Z0-9+.\-]*:")
FENCE_RE = re.compile(r"^\s*(```|~~~)")
KIND_ORDER = {"MADR": 0, "PLAN": 1, "REPORT": 2, "GATES": 3}
KIND_SUFFIX = {"MADR": "docs/decisions", "PLAN": "docs/decisions",
               "REPORT": "docs/reports", "GATES": "docs/reports"}
SKIP_PARTS = {".git", "node_modules", "vendor", "dist", ".pub-cache",
              ".pub", ".symlinks", "build", ".dart_tool", ".gradle",
              ".idea", ".vscode"}
TOC_BEGIN = "<!-- check_records.py ToC begin (generated; do not hand-edit between the markers) -->"
TOC_END = "<!-- check_records.py ToC end -->"
INDEX_PATH = REPO / "docs" / "README.md"


def errors(msg: str) -> None:
    print(msg)


def warn(msg: str) -> None:
    print(msg, file=sys.stderr)


def markdown_files() -> list[Path]:
    out = []
    for p in sorted(REPO.rglob("*.md")):
        rel_parts = p.relative_to(REPO).parts
        if any(part in SKIP_PARTS for part in rel_parts[:-1]):
            continue
        out.append(p)
    return out


class Record:
    def __init__(self, path: Path) -> None:
        m = RECORD_RE.match(path.name)
        if not m:
            raise ValueError(path.name)
        self.path = path
        self.rel = path.relative_to(REPO).as_posix()
        self.num, self.kind, self.slug = m.group(1), m.group(2), m.group(3)

    def title(self) -> str:
        try:
            for line in self.path.read_text(encoding="utf-8").splitlines():
                stripped = line.strip()
                if stripped.startswith("# "):
                    return stripped[2:].strip()
        except OSError:
            pass
        return self.rel


def find_records() -> list[Record]:
    records = []
    for p in markdown_files():
        if RECORD_RE.match(p.name):
            records.append(Record(p))
    return records


def next_number() -> int:
    used = {int(r.num) for r in find_records()}
    n = 1
    while n in used:
        n += 1
    return n


def check_structure(records: list[Record]) -> None:
    by_num: dict[str, list[Record]] = {}
    for r in records:
        by_num.setdefault(r.num, []).append(r)
    for num in sorted(by_num):
        items = by_num[num]
        madrs = [r for r in items if r.kind == "MADR"]
        plans = [r for r in items if r.kind == "PLAN"]
        gates = [r for r in items if r.kind == "GATES"]
        if len(madrs) > 1:
            warn(f"number {num} is claimed by {len(madrs)} MADRs: "
                 + ", ".join(r.rel for r in madrs)
                 + " (a number is never reused)")
        if plans and not madrs:
            warn(f"number {num} has a PLAN but no MADR: "
                 + ", ".join(r.rel for r in plans))
        if gates and not plans:
            warn(f"number {num} has a GATES but no PLAN: "
                 + ", ".join(r.rel for r in gates))
    for r in records:
        expected = KIND_SUFFIX[r.kind]
        parent = r.path.parent.relative_to(REPO).as_posix()
        if parent != expected and not parent.endswith("/" + expected):
            warn(f"record outside its directory: {r.rel} "
                 f"(expected a path ending in {expected}/)")


def strip_code_spans(line: str) -> str:
    """Blank out `inline code` spans so example links inside them, like
    `[text](url)` or `SendRequest[T any](c *Connection, …)`, are not
    mistaken for live markdown links."""
    parts = line.split("`")
    return "".join(p for i, p in enumerate(parts) if i % 2 == 0)


def check_links(path: Path) -> int:
    rel = path.relative_to(REPO).as_posix()
    found = 0
    in_fence = False
    try:
        lines = path.read_text(encoding="utf-8").splitlines()
    except OSError as exc:
        errors(f"unreadable file: {rel}: {exc}")
        return 1
    for i, line in enumerate(lines, 1):
        if FENCE_RE.match(line):
            in_fence = not in_fence
            continue
        if in_fence:
            continue
        for m in LINK_RE.finditer(strip_code_spans(line)):
            target = m.group(1).strip()
            if not target or target.startswith("#"):
                continue
            if SCHEME_RE.match(target):
                continue
            if any(c.isspace() for c in target):
                continue
            file_part = target.split("#", 1)[0]
            if not file_part:
                continue
            resolved = Path(os.path.normpath(path.parent / file_part))
            if not resolved.exists():
                errors(f"broken relative link: {rel}:{i}: {target}")
                found += 1
    return found


def unnumbered_docs() -> list[Path]:
    out = []
    for p in markdown_files():
        rel = p.relative_to(REPO).as_posix()
        if rel == "README.md" or rel in ("docs/README.md", "docs/architecture.md"):
            out.append(p)
        elif rel.startswith("docs/guides/"):
            out.append(p)
        elif rel in ("apps/mobile/docs/README.md",
                     "apps/mobile/docs/architecture.md"):
            out.append(p)
        elif rel.startswith("apps/mobile/docs/guides/"):
            out.append(p)
    return out


def write_index(records: list[Record]) -> None:
    rows = []
    for r in sorted(records, key=lambda r: (int(r.num), KIND_ORDER[r.kind], r.rel)):
        link = f"decisions/{r.path.name}" if r.path.parent == REPO / "docs" / "decisions" else f"../{r.rel}"
        title = r.title().replace("|", "\\|")
        rows.append(f"| {r.num} | {r.kind} | {title} | [{r.path.name}]({link}) |")
    table = ["| Number | Kind | Title | Record |",
             "| :--- | :--- | :--- | :--- |"] + rows
    block = TOC_BEGIN + "\n" + "\n".join(table) + "\n" + TOC_END
    if INDEX_PATH.exists():
        text = INDEX_PATH.read_text(encoding="utf-8")
        if TOC_BEGIN in text and TOC_END in text:
            before = text.split(TOC_BEGIN)[0]
            after = text.split(TOC_END, 1)[1]
            text = before + block + after
        else:
            text = text.rstrip("\n") + "\n\n" + block + "\n"
    else:
        text = ("# Docs index\n\n" + block
                + "\n\n## I want to…\n\n| I want to… | Start here |\n| :--- | :--- |\n")
    INDEX_PATH.write_text(text, encoding="utf-8")


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    ap.add_argument("--next", action="store_true", help="print the next unused record number")
    ap.add_argument("--check", action="store_true", help="validate numbered records")
    ap.add_argument("--check-all", action="store_true", help="--check plus unnumbered docs")
    ap.add_argument("--write-index", action="store_true", help="regenerate the records ToC in docs/README.md")
    args = ap.parse_args()
    if not (args.next or args.check or args.check_all or args.write_index):
        ap.print_help()
        return 2
    records = find_records()
    if args.next:
        print(f"{next_number():04d}")
    if args.write_index:
        write_index(records)
    if args.check or args.check_all:
        check_structure(records)
        broken = sum(check_links(r.path) for r in records)
        if args.check_all:
            for p in unnumbered_docs():
                broken += check_links(p)
        if broken:
            print(f"{broken} broken relative link(s)")
            return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
