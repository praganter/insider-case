#!/usr/bin/env python3
"""Redact local info from Claude session exports before they are committed.

  session.py sanitize RAW OUT   write a redacted copy of RAW to OUT, print what changed
  session.py check FILE...      exit 1 if a file still holds local info (pre-commit hook)

Username and git email are read at runtime so nothing personal is hardcoded here.
"""
import getpass
import re
import subprocess
import sys
from pathlib import Path

# Addresses that are public and expected to stay in the export.
KEEP_EMAILS = {"noreply@anthropic.com", "git@github.com"}


def git_config(key):
    return subprocess.run(["git", "config", key], capture_output=True, text=True).stdout.strip()


def redact(text):
    counts = {}

    def sub(label, pattern, repl, flags=0):
        nonlocal text
        changed = 0

        def swap(m):
            nonlocal changed
            out = repl(m) if callable(repl) else m.expand(repl)
            changed += out != m.group(0)  # kept matches (allowed emails) are not changes
            return out
        text = re.sub(pattern, swap, text, flags=flags)
        if changed:
            counts[label] = counts.get(label, 0) + changed

    sub("local path",
        r"[A-Za-z]:[\\/]+Users[\\/]+[^\\/\s\"'`]+|/(?:[a-z]/)?Users/[^/\s\"'`]+|/home/[^/\s\"'`]+", "~")
    sub("api key", r"sk-ant-[\w-]+|sk-[A-Za-z0-9]{20,}", "<api-key>")
    sub("api key", r"(ANTHROPIC_API_KEY=)[\w-]{8,}", r"\1<api-key>")

    def email(m):
        return m.group(0) if m.group(0).lower() in KEEP_EMAILS else "<email>"
    sub("email", r"[\w.+-]+@[\w-]+(?:\.[\w-]+)+", email)

    for value in {getpass.getuser(), git_config("user.email").split("@")[0]}:
        if len(value) >= 3:
            sub("username", rf"\b{re.escape(value)}\b", "<user>", re.IGNORECASE)
    return text, counts


def main(argv):
    if len(argv) == 4 and argv[1] == "sanitize":
        raw, out = Path(argv[2]), Path(argv[3])
        text, counts = redact(raw.read_text(encoding="utf-8", errors="replace"))
        out.parent.mkdir(parents=True, exist_ok=True)
        out.write_text(text, encoding="utf-8")
        print(f"wrote {out}")
        for label, n in sorted(counts.items()):
            print(f"  {label}: {n} replaced")
        return 0
    if len(argv) >= 3 and argv[1] == "check":
        bad = False
        for f in argv[2:]:
            text = Path(f).read_text(encoding="utf-8", errors="replace")
            _, counts = redact(text)
            if counts:
                bad = True
                print(f"{f}: local info found {counts}; run .claude/skills/ship-session/scripts/session.py sanitize first", file=sys.stderr)
        return 1 if bad else 0
    print(__doc__, file=sys.stderr)
    return 2


if __name__ == "__main__":
    sys.exit(main(sys.argv))
