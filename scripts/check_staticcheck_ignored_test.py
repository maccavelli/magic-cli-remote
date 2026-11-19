#!/usr/bin/env python3
"""Tests for scripts/check_staticcheck_ignored.py (0131-PLAN, the bound script's own tests).

Run from the repository root:  make test-scripts
                          or:  python3 -m unittest discover -s scripts -p '*_test.py'

The convention (0131-PLAN, P3 close-out): a Python script `scripts/<name>.py` has its tests in `scripts/<name>_test.py`, a
`unittest` file (standard library only, no network), and `make test-scripts` runs every such file. The names follow the
`scripts/<name>_test.sh` convention of the shell scripts, with the underscore a Python module name needs.

Nothing here needs a staticcheck binary or the real tree: the Go files are fixtures in a temporary directory, and the
`git ls-files` call and the staticcheck run are passed in or patched. Written for Python 3.9, the oldest `python3` a
developer's macOS ships; no later feature is used.
"""

import contextlib
import io
import os
import sys
import tempfile
import unittest
from types import SimpleNamespace
from unittest import mock

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import check_staticcheck_ignored as script  # noqa: E402

IGN = "//lint:ignore SA1019 0207 amendment H: the unstable schema"
NOL = "//nolint:staticcheck // as the lint:ignore above"
FILE_IGN = "//lint:file-ignore SA1019 0207 amendment H: the unstable schema"
FILE_NOL = "//nolint:staticcheck // as the lint:file-ignore below"
SDK_NOTICE = "this declaration comes from the unstable ACP schema and may change"


def go_text(*body):
    return "\n".join(body) + "\n"


class FixtureCase(unittest.TestCase):
    def setUp(self):
        tmp = tempfile.TemporaryDirectory()
        self.addCleanup(tmp.cleanup)
        self.dir = tmp.name

    def go(self, text, name="a.go"):
        path = os.path.join(self.dir, name)
        with open(path, "w", encoding="utf-8") as fh:
            fh.write(text)
        return path


class DirectiveRule(FixtureCase):
    """Rule 1: every //lint:ignore and //lint:file-ignore names exactly SA1019 and cites 0207 amendment H."""

    def test_directive_naming_sa1019_with_the_reason_passes(self):
        path = self.go(go_text("package p", IGN, "var x int"))
        self.assertEqual(script.bad_directives([path]), [])

    def test_ignore_of_a_code_other_than_sa1019_is_flagged(self):
        path = self.go(go_text("package p", "//lint:ignore SA4006 0207 amendment H", "var x int"))
        bad = script.bad_directives([path])
        self.assertEqual(len(bad), 1)
        self.assertTrue(bad[0].startswith(path + ":2:"), bad)

    def test_file_ignore_of_a_code_other_than_sa1019_is_flagged(self):
        path = self.go(go_text("//lint:file-ignore ST1000 0207 amendment H", "package p"))
        self.assertEqual(len(script.bad_directives([path])), 1)

    def test_ignore_naming_sa1019_among_other_codes_is_flagged(self):
        path = self.go(go_text("package p", "//lint:ignore SA1019,SA4006 0207 amendment H", "var x int"))
        self.assertEqual(len(script.bad_directives([path])), 1)

    def test_ignore_with_no_reason_is_flagged(self):
        path = self.go(go_text("package p", "//lint:ignore SA1019", "var x int"))
        self.assertEqual(len(script.bad_directives([path])), 1)

    def test_ignore_with_a_reason_that_does_not_cite_the_record_is_flagged(self):
        path = self.go(go_text("package p", "//lint:ignore SA1019 because it is old", "var x int"))
        self.assertEqual(len(script.bad_directives([path])), 1)

    def test_unreadable_file_is_skipped(self):
        self.assertEqual(script.bad_directives([os.path.join(self.dir, "missing.go")]), [])

    def test_default_file_list_is_the_tracked_go_files(self):
        path = self.go(go_text("package p", "//lint:ignore SA4006 0207 amendment H", "var x int"))
        with mock.patch.object(script, "tracked_go_files", return_value=[path]):
            self.assertEqual(len(script.bad_directives()), 1)


class TrackedFiles(unittest.TestCase):
    def test_tracked_go_files_asks_git_for_go_files(self):
        done = SimpleNamespace(stdout="a.go\nb/c.go\n")
        with mock.patch.object(script.subprocess, "run", return_value=done) as run:
            self.assertEqual(script.tracked_go_files(), ["a.go", "b/c.go"])
        self.assertEqual(run.call_args[0][0], ["git", "ls-files", "*.go"])


class PairingRules(FixtureCase):
    """Rule 3: each //lint:ignore and //lint:file-ignore is paired with its //nolint:staticcheck, and no other is allowed."""

    def pairs(self, text):
        return script.bad_pairs([self.go(text)])

    # //lint:ignore -> the next line carries //nolint:staticcheck // as the lint:ignore above

    def test_ignore_followed_by_its_nolint_passes(self):
        bad, count = self.pairs(go_text("package p", "func f() {", "\t" + IGN, "\tuse() " + NOL, "}"))
        self.assertEqual((bad, count), ([], 1))

    def test_ignore_whose_next_line_has_no_nolint_is_flagged(self):
        bad, _ = self.pairs(go_text("package p", "func f() {", "\t" + IGN, "\tuse()", "}"))
        self.assertEqual(len(bad), 1)
        self.assertIn("the line under the //lint:ignore at line 3", bad[0])

    def test_ignore_on_the_last_line_is_flagged(self):
        bad, _ = self.pairs(go_text("package p", IGN).rstrip("\n"))
        self.assertEqual(len(bad), 1)

    def test_ignore_whose_nolint_names_another_linter_is_flagged(self):
        bad, _ = self.pairs(go_text("package p", IGN, "use() //nolint:gosec // as the lint:ignore above"))
        self.assertEqual(len(bad), 1)

    def test_ignore_whose_nolint_has_no_pairing_comment_is_flagged(self):
        bad, _ = self.pairs(go_text("package p", IGN, "use() //nolint:staticcheck // old API"))
        self.assertEqual(len(bad), 1)
        self.assertIn("the line under the //lint:ignore", bad[0])

    def test_ignore_whose_nolint_has_no_comment_at_all_is_flagged(self):
        bad, _ = self.pairs(go_text("package p", IGN, "use() //nolint:staticcheck"))
        self.assertEqual(len(bad), 1)

    # //lint:file-ignore -> //nolint:staticcheck // as the lint:file-ignore below, directly above package

    def test_file_ignore_with_the_file_level_nolint_above_package_passes(self):
        bad, count = self.pairs(go_text(FILE_NOL, "package p", "", FILE_IGN, "var x int"))
        self.assertEqual((bad, count), ([], 1))

    def test_file_ignore_with_no_file_level_nolint_is_flagged(self):
        bad, _ = self.pairs(go_text("package p", "", FILE_IGN, "var x int"))
        self.assertEqual(len(bad), 1)
        self.assertIn("//lint:file-ignore has no", bad[0])

    def test_file_ignore_with_the_nolint_not_directly_above_package_is_flagged(self):
        bad, _ = self.pairs(go_text(FILE_NOL, "", "package p", "", FILE_IGN, "var x int"))
        self.assertTrue(any("//lint:file-ignore has no" in b for b in bad), bad)

    def test_file_ignore_whose_nolint_has_the_wrong_pairing_comment_is_flagged(self):
        bad, _ = self.pairs(go_text("//nolint:staticcheck // as the lint:ignore above", "package p", FILE_IGN))
        self.assertTrue(any("//lint:file-ignore has no" in b for b in bad), bad)

    def test_file_ignore_whose_nolint_names_another_linter_is_flagged(self):
        bad, _ = self.pairs(go_text("//nolint:gosec // as the lint:file-ignore below", "package p", FILE_IGN))
        self.assertTrue(any("//lint:file-ignore has no" in b for b in bad), bad)

    def test_file_ignore_in_a_file_with_no_package_clause_is_flagged(self):
        bad, _ = self.pairs(go_text(FILE_IGN, "var x int"))
        self.assertEqual(len(bad), 1)

    # //nolint:staticcheck -> must be one of those two forms

    def test_nolint_with_no_lint_ignore_above_is_flagged(self):
        bad, count = self.pairs(go_text("package p", "func f() {", "\tuse() " + NOL, "}"))
        self.assertEqual(count, 1)
        self.assertEqual(len(bad), 1)
        self.assertIn("with no //lint:ignore on the line above", bad[0])

    def test_nolint_with_a_blank_line_between_it_and_the_ignore_is_flagged(self):
        bad, _ = self.pairs(go_text("package p", IGN, "", "use() " + NOL))
        self.assertTrue(any("with no //lint:ignore on the line above" in b for b in bad), bad)

    def test_file_level_nolint_with_no_file_ignore_in_the_file_is_flagged(self):
        bad, count = self.pairs(go_text(FILE_NOL, "package p", "var x int"))
        self.assertEqual(count, 1)
        self.assertEqual(len(bad), 1)
        self.assertIn("with no //lint:file-ignore in the file", bad[0])

    def test_file_level_nolint_with_a_file_ignore_in_the_file_passes(self):
        bad, count = self.pairs(go_text(FILE_NOL, "package p", FILE_IGN))
        self.assertEqual((bad, count), ([], 1))

    # //nolint naming staticcheck among several linters

    def test_nolint_naming_staticcheck_among_several_linters_is_a_staticcheck_nolint(self):
        text = go_text("package p", IGN, "use() //nolint:gosec,staticcheck // as the lint:ignore above")
        bad, count = self.pairs(text)
        self.assertEqual((bad, count), ([], 1))

    def test_unpaired_nolint_naming_staticcheck_among_several_linters_is_flagged(self):
        bad, count = self.pairs(go_text("package p", "use() //nolint:gosec,staticcheck // old API"))
        self.assertEqual(count, 1)
        self.assertEqual(len(bad), 1)

    def test_nolint_that_does_not_name_staticcheck_is_not_counted_or_flagged(self):
        bad, count = self.pairs(go_text("package p", "use() //nolint:gosec // reviewed", "use() //nolint:staticcheckx // not it"))
        self.assertEqual((bad, count), ([], 0))

    # the count the bound is held against

    def test_count_is_every_nolint_staticcheck_in_every_file(self):
        a = self.go(go_text("package p", IGN, "use() " + NOL, IGN, "use() " + NOL), "a.go")
        b = self.go(go_text(FILE_NOL, "package p", FILE_IGN), "b.go")
        bad, count = script.bad_pairs([a, b])
        self.assertEqual((bad, count), ([], 3))

    def test_unreadable_file_is_skipped(self):
        self.assertEqual(script.bad_pairs([os.path.join(self.dir, "missing.go")]), ([], 0))

    def test_default_file_list_is_the_tracked_go_files(self):
        path = self.go(go_text("package p", "use() " + NOL))
        with mock.patch.object(script, "tracked_go_files", return_value=[path]):
            bad, count = script.bad_pairs()
        self.assertEqual((len(bad), count), (1, 1))


def record(code="SA1019", msg=None, severity="ignored", file="x.go", line=7):
    if msg is None:
        msg = "github.com/maccavelli/acp-go-sdk2.Thing is deprecated: " + SDK_NOTICE
    return '{"code": "%s", "severity": "%s", "location": {"file": "%s", "line": %d}, "message": "%s"}' % (
        code, severity, file, line, msg)


def stub_run(stdout="", stderr="", returncode=1, calls=None):
    def run(argv, **kwargs):
        if calls is not None:
            calls.append(argv)
        return SimpleNamespace(stdout=stdout, stderr=stderr, returncode=returncode)
    return run


class IgnoredFindingsRule(unittest.TestCase):
    """Rule 2: staticcheck reports no ignored finding but SA1019 on an acp-go-sdk2 unstable-schema declaration."""

    def test_staticcheck_is_run_with_show_ignored_json_over_the_module(self):
        calls = []
        script.bad_ignored("stub-bin", run=stub_run(calls=calls))
        self.assertEqual(calls, [["stub-bin", "-show-ignored", "-f", "json", "./..."]])

    def test_sdk_declaration_with_the_unstable_notice_is_allowed_and_counted(self):
        out = record() + "\n" + record(line=8) + "\n"
        self.assertEqual(script.bad_ignored("s", run=stub_run(out)), ([], 2))

    def test_sdk_method_form_of_the_subject_is_allowed(self):
        msg = "(github.com/maccavelli/acp-go-sdk2.Type).Field is deprecated: " + SDK_NOTICE
        self.assertEqual(script.bad_ignored("s", run=stub_run(record(msg=msg))), ([], 1))

    def test_ignored_finding_of_another_code_is_flagged(self):
        bad, allowed = script.bad_ignored("s", run=stub_run(record(code="SA4006")))
        self.assertEqual(allowed, 0)
        self.assertEqual(len(bad), 1)
        self.assertTrue(bad[0].startswith("x.go:7: ignored SA4006:"), bad)

    def test_ignored_sa1019_outside_the_sdk_is_flagged(self):
        msg = "example.com/other.Old is deprecated: " + SDK_NOTICE
        bad, allowed = script.bad_ignored("s", run=stub_run(record(msg=msg)))
        self.assertEqual((len(bad), allowed), (1, 0))

    def test_ignored_sa1019_on_the_sdk_with_another_deprecation_is_flagged(self):
        msg = "github.com/maccavelli/acp-go-sdk2.Thing is deprecated: use Other instead"
        bad, allowed = script.bad_ignored("s", run=stub_run(record(msg=msg)))
        self.assertEqual((len(bad), allowed), (1, 0))

    def test_finding_that_is_not_ignored_is_not_this_scripts_business(self):
        out = record(code="SA4006", severity="error")
        self.assertEqual(script.bad_ignored("s", run=stub_run(out)), ([], 0))

    def test_unreadable_output_line_is_flagged(self):
        bad, _ = script.bad_ignored("s", run=stub_run("not json at all\n"))
        self.assertEqual(len(bad), 1)
        self.assertIn("unreadable staticcheck output", bad[0])

    def test_staticcheck_failing_to_run_is_flagged(self):
        bad, _ = script.bad_ignored("s", run=stub_run("", "boom", returncode=2))
        self.assertEqual(len(bad), 1)
        self.assertIn("exited 2: boom", bad[0])

    def test_staticcheck_exiting_one_with_findings_is_not_a_failure(self):
        self.assertEqual(script.bad_ignored("s", run=stub_run(record(), returncode=1)), ([], 1))

    def test_clean_run_with_no_output_is_empty(self):
        self.assertEqual(script.bad_ignored("s", run=stub_run("", "", returncode=0)), ([], 0))


class Main(unittest.TestCase):
    """The bounds, the exit status and the lines printed, with the three rules patched to return what a test needs."""

    def run_main(self, nolints=None, allowed=None, directives=(), pairs=(), ignored=(), argv=("check", "stub-bin")):
        nolints = script.BOUND_NOLINT if nolints is None else nolints
        allowed = script.BOUND_IGNORED if allowed is None else allowed
        out = io.StringIO()
        with mock.patch.object(sys, "argv", list(argv)), \
                mock.patch.object(script, "bad_directives", return_value=list(directives)), \
                mock.patch.object(script, "bad_pairs", return_value=(list(pairs), nolints)), \
                mock.patch.object(script, "bad_ignored", return_value=(list(ignored), allowed)) as bi, \
                contextlib.redirect_stdout(out):
            rc = script.main()
        self.bad_ignored_mock = bi
        return rc, out.getvalue()

    # the bound on //nolint:staticcheck directives

    def test_nolint_count_at_the_bound_passes(self):
        rc, out = self.run_main(nolints=script.BOUND_NOLINT)
        self.assertEqual(rc, 0)
        self.assertNotIn("lower it", out)
        self.assertNotIn("bound exceeded", out)

    def test_nolint_count_one_over_the_bound_fails(self):
        rc, out = self.run_main(nolints=script.BOUND_NOLINT + 1)
        self.assertEqual(rc, 1)
        self.assertIn("suppression bound exceeded", out)
        self.assertIn("over the recorded bound of %d" % script.BOUND_NOLINT, out)

    def test_nolint_count_below_the_bound_passes_and_says_to_lower_it(self):
        rc, out = self.run_main(nolints=script.BOUND_NOLINT - 1)
        self.assertEqual(rc, 0)
        self.assertIn("(below the recorded bound: lower it)", out)

    # the bound on ignored findings

    def test_ignored_count_at_the_bound_passes(self):
        rc, out = self.run_main(allowed=script.BOUND_IGNORED)
        self.assertEqual(rc, 0)
        self.assertNotIn("lower it", out)
        self.assertNotIn("bound exceeded", out)

    def test_ignored_count_one_over_the_bound_fails(self):
        rc, out = self.run_main(allowed=script.BOUND_IGNORED + 1)
        self.assertEqual(rc, 1)
        self.assertIn("suppression bound exceeded", out)
        self.assertIn("over the recorded bound of %d" % script.BOUND_IGNORED, out)

    def test_ignored_count_below_the_bound_passes_and_says_to_lower_it(self):
        rc, out = self.run_main(allowed=script.BOUND_IGNORED - 1)
        self.assertEqual(rc, 0)
        self.assertIn("(below the recorded bound: lower it)", out)

    # the status and the lines

    def test_clean_run_prints_the_summary_line_and_exits_zero(self):
        rc, out = self.run_main()
        self.assertEqual(rc, 0)
        self.assertEqual(out, (
            "check_staticcheck_ignored: %d ignored finding(s), each an acp-go-sdk2 unstable-schema name; "
            "%d //nolint:staticcheck directive(s), each paired\n" % (script.BOUND_IGNORED, script.BOUND_NOLINT)))

    def test_a_bad_directive_fails_and_is_printed(self):
        rc, out = self.run_main(directives=["a.go:3: //lint:ignore SA4006"])
        self.assertEqual(rc, 1)
        self.assertIn("lint directive outside MADR 0207 amendment H (D45): a.go:3:", out)

    def test_an_unpaired_suppression_fails_and_is_printed(self):
        rc, out = self.run_main(pairs=["a.go:4: unpaired"])
        self.assertEqual(rc, 1)
        self.assertIn("unpaired suppression (0131-PLAN, Q5 A): a.go:4: unpaired", out)

    def test_an_ignored_finding_outside_the_exception_fails_and_is_printed(self):
        rc, out = self.run_main(ignored=["x.go:7: ignored SA4006: m"])
        self.assertEqual(rc, 1)
        self.assertIn("ignored finding outside MADR 0207 amendment H (D45): x.go:7:", out)

    def test_no_summary_line_is_printed_on_failure(self):
        _, out = self.run_main(pairs=["a.go:4: unpaired"])
        self.assertNotIn("check_staticcheck_ignored:", out)

    def test_the_staticcheck_binary_is_the_argument(self):
        self.run_main(argv=("check", "./bin/tools/sc"))
        self.assertEqual(self.bad_ignored_mock.call_args[0][0], "./bin/tools/sc")

    def test_without_the_binary_argument_it_prints_usage_and_exits_two(self):
        err = io.StringIO()
        with mock.patch.object(sys, "argv", ["check"]), contextlib.redirect_stderr(err):
            rc = script.main()
        self.assertEqual(rc, 2)
        self.assertIn("Usage: check_staticcheck_ignored.py", err.getvalue())


if __name__ == "__main__":
    unittest.main()
