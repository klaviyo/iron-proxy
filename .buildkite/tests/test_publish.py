"""Exercise publication guards and digest handoff without cloud credentials."""
import os
from pathlib import Path
import subprocess
import tempfile
import unittest

SCRIPT = Path(__file__).resolve().parents[1] / "publish.sh"
SHA = "a" * 40
DIGEST = "sha256:" + "b" * 64


class PublishTests(unittest.TestCase):
    def run_publish(self, branch="klaviyo-main", pr="false", sha=SHA, fail="", digest=DIGEST):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            for name, body in {
                "kbc": 'echo "kbc $*" >> calls\n[ "$FAIL_KBC" != yes ]',
                "aws": 'echo "aws $*" >> calls\nprintf "%s\\n" "$TEST_DIGEST"',
            }.items():
                binary = root / name
                binary.write_text("#!/bin/sh\n" + body + "\n")
                binary.chmod(0o755)
            environment = dict(os.environ, PATH=str(root) + os.pathsep + os.environ["PATH"],
                               BUILDKITE_BRANCH=branch, BUILDKITE_PULL_REQUEST=pr,
                               BUILDKITE_COMMIT=sha, FAIL_KBC=fail, TEST_DIGEST=digest)
            result = subprocess.run(["bash", str(SCRIPT)], cwd=root, env=environment,
                                    capture_output=True, text=True)
            calls = (root / "calls").read_text() if (root / "calls").exists() else ""
            refs = root / "iron-proxy-image-refs.txt"
            return result, calls, refs.read_text() if refs.exists() else ""

    def test_untrusted_builds_never_invoke_publisher(self):
        for branch, pr, sha in [("feature", "false", SHA), ("klaviyo-main", "42", SHA),
                                ("klaviyo-main", "", SHA), ("klaviyo-main", "false", "HEAD")]:
            with self.subTest(branch=branch, pr=pr, sha=sha):
                result, calls, _ = self.run_publish(branch, pr, sha)
                self.assertNotEqual(result.returncode, 0)
                self.assertEqual(calls, "")

    def test_success_publishes_commit_to_both_accounts_and_records_digests(self):
        result, calls, refs = self.run_publish()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn(f"-t {SHA} -a eng -a dev", calls)
        self.assertEqual(refs.splitlines(), [
            f"{registry}.dkr.ecr.us-east-1.amazonaws.com/klaviyo-iron-proxy@{DIGEST}"
            for registry in ("340433579684", "439508887365")])
        self.assertEqual(calls.count("aws ecr describe-images"), 2)

    def test_publish_failure_stops_before_recording_images(self):
        result, calls, refs = self.run_publish(fail="yes")
        self.assertNotEqual(result.returncode, 0)
        self.assertNotIn("aws ", calls)
        self.assertEqual(refs, "")

    def test_invalid_digest_fails(self):
        result, _, refs = self.run_publish(digest="None")
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(refs, "")
