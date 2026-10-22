#!/usr/bin/env python3
"""Turn P3.0's live raw-output samples into phone renderer fixtures (PLAN 0208 P3).

Input: the directory a `TestLiveRawOutputSamples` run wrote, one
<provider>/samples.json per provider. Output, per provider, under
apps/mobile/test/fixtures/raw_output/<provider>/:

  NN-<kind>.txt   one sample's text as Magic received it, redacted
  expected.json   {file: expected kind}, created empty for a new file; the
                  author fills each kind in once, by reading the file

Redaction replaces the account name, the home directory and the host name with
placeholders. Like wirecap's, it is not a full identifier scrub: every file is
read before it is staged.

Usage:
  python3 scripts/extract-raw-output-fixtures.py --in <dir> [--out <dir>]
"""
import argparse
import getpass
import json
import os
import pathlib
import re
import socket
import sys

REPO = pathlib.Path(__file__).resolve().parent.parent
DEFAULT_OUT = REPO / "apps/mobile/test/fixtures/raw_output"


def redactions() -> list[tuple[re.Pattern, str]]:
    """Patterns for this machine's identifiers, longest first."""
    subs = []
    home = os.path.expanduser("~")
    if home and home != "/":
        subs.append((re.compile(re.escape(home)), "/home/user"))
    host = socket.gethostname()
    for h in sorted({host, host.split(".")[0]}, key=len, reverse=True):
        if h:
            subs.append((re.compile(r"\b" + re.escape(h) + r"\b"), "host"))
    user = getpass.getuser()
    if user:
        subs.append((re.compile(r"\b" + re.escape(user) + r"\b"), "user"))
    return subs


def redact(text: str, subs: list[tuple[re.Pattern, str]]) -> str:
    for pattern, placeholder in subs:
        text = pattern.sub(placeholder, text)
    return text


def sample_text(sample: dict) -> str:
    """A sample's text, then each diff as old and new text under its path."""
    parts = [sample.get("text", "")]
    for d in sample.get("diffs") or []:
        parts.append(f"{d.get('path', '')}\n--- old\n{d.get('old_text', '')}\n+++ new\n{d.get('new_text', '')}")
    return "\n\n".join(p for p in parts if p.strip())


def extract(provider_dir: pathlib.Path, out_dir: pathlib.Path, subs) -> list[str]:
    samples = json.loads((provider_dir / "samples.json").read_text())
    out_dir.mkdir(parents=True, exist_ok=True)
    written = []
    for i, sample in enumerate(samples, start=1):
        name = f"{i:02d}-{sample.get('kind', 'sample')}.txt"
        (out_dir / name).write_text(redact(sample_text(sample), subs) + "\n")
        written.append(name)
    expected_path = out_dir / "expected.json"
    expected = json.loads(expected_path.read_text()) if expected_path.exists() else {}
    for name in written:
        expected.setdefault(name, "")
    expected_path.write_text(json.dumps(expected, indent=2, sort_keys=True) + "\n")
    return written


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--in", dest="src", required=True, type=pathlib.Path)
    ap.add_argument("--out", dest="dst", default=DEFAULT_OUT, type=pathlib.Path)
    args = ap.parse_args()
    subs = redactions()
    found = sorted(p.parent for p in args.src.glob("*/samples.json"))
    if not found:
        print(f"no <provider>/samples.json under {args.src}", file=sys.stderr)
        return 1
    for provider_dir in found:
        written = extract(provider_dir, args.dst / provider_dir.name, subs)
        print(f"{provider_dir.name}: {len(written)} fixtures")
    return 0


if __name__ == "__main__":
    sys.exit(main())
