"""The Python helper, held to conformance/scenarios.json as the Go and TypeScript ones are, and to
the same unit tests, name for name, as brick/brick_test.go."""

from __future__ import annotations

import datetime
import io
import json
import os
import pathlib
import sys
import tempfile
import unittest
from unittest import mock

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parents[1] / "src"))

import agentiik_brick as brick  # noqa: E402

SCENARIOS = json.loads((pathlib.Path(__file__).resolve().parents[2] / "conformance" / "scenarios.json").read_text(encoding="utf-8"))


class Task(unittest.TestCase):
    """Each test runs as a container would: a root of its own and the environment the runner
    sets."""

    def task(self, run_id="r", step="s", attempt=1, ports="out"):
        tmp = tempfile.TemporaryDirectory()
        self.addCleanup(tmp.cleanup)
        self.root = tmp.name
        env = mock.patch.dict(os.environ, {"AGK_RUN_ID": run_id, "AGK_STEP": step, "AGK_ATTEMPT": str(attempt), "AGK_OUT_PORTS": ports})
        env.start()
        self.addCleanup(env.stop)
        for name, value in (("root", tmp.name), ("limits", brick.SizeLimits()), ("stdin", io.StringIO(""))):
            patch = mock.patch.object(brick, name, value)
            patch.start()
            self.addCleanup(patch.stop)

    def write(self, path, content):
        full = pathlib.Path(self.root, path)
        full.parent.mkdir(parents=True, exist_ok=True)
        full.write_text(content, encoding="utf-8")

    def published(self, port):
        return json.loads(pathlib.Path(self.root, "agk/out/ports", port + ".json").read_text(encoding="utf-8"))


class Conformance(Task):
    def test_the_defaults_are_the_documented_ones(self):
        l = SCENARIOS["limits"]
        self.assertEqual(brick.DEFAULT_LIMITS, brick.SizeLimits(l["inline_max_bytes"], l["envelope_max_bytes"], l["max_items"]))

    def test_exit_codes_stay_in_the_bricks_bands(self):
        for code, want in SCENARIOS["exit"]:
            self.assertEqual(brick.exit_code(code), want, code)

    def test_segments_are_filed_as_the_engine_files_them(self):
        for s, want in SCENARIOS["segment"]:
            self.assertEqual(brick.segment(s), want, s)

    def test_values_are_encoded_as_the_engine_encodes_them(self):
        for value, want in SCENARIOS["encode"]:
            self.assertEqual(brick._encode(value).decode("utf-8"), want)

    def test_heavy_values_are_spilled(self):
        for c in SCENARIOS["spill"]:
            with self.subTest(c["name"]):
                t = c["task"]
                self.task(t["run_id"], t["step"], t["attempt"], c["port"])
                brick.limits.inline_max = c["limits"]["inline_max_bytes"]
                brick.emit(c["port"], json.loads(json.dumps(c["items"])))
                got = self.published(c["port"])
                self.assertEqual(got["items"], c["want"])
                self.assertEqual(got["meta"], {"run_id": t["run_id"], "step": t["step"], "port": c["port"], "attempt": t["attempt"], "count": 1, "produced_at": "1970-01-01T00:00:00Z"})

    def test_an_envelope_the_runner_would_refuse_is_refused_first(self):
        for c in SCENARIOS["refuse"]:
            with self.subTest(c["name"]):
                self.task()
                l = c["limits"]
                brick.limits = brick.SizeLimits(l.get("inline_max_bytes", 0), l.get("envelope_max_bytes", 0), l.get("max_items", 0))
                items = [{"id": "x" * (i + 1), "data": {"v": "0123456789"}} for i in range(c["items"])]
                with self.assertRaisesRegex(brick.TooLarge, c["says"]):
                    brick.emit("out", items)
                self.assertFalse(pathlib.Path(self.root, "agk/out/ports/out.json").exists())


class Contract(Task):
    def test_an_envelope_is_read_from_its_port_and_numbers_keep_their_digits(self):
        self.task()
        self.write("agk/in/orders/envelope.json", '{"meta":{"run_id":"r","step":"up","port":"ok","attempt":1,"count":1,"produced_at":"2026-01-31T09:00:00Z"},"items":[{"id":"a","data":{"cents":12345678901234567890},"files":[]}]}')
        self.assertEqual(brick.items("orders")[0]["data"]["cents"], 12345678901234567890)

    def test_a_port_nobody_fed_is_an_empty_batch(self):
        self.task()
        for port in ("in", "reference"):
            self.assertEqual(brick.items(port), [])

    def test_the_port_in_is_read_from_standard_input_when_no_file_is_mounted(self):
        self.task()
        brick.stdin = io.StringIO('{"meta":{},"items":[{"id":"a","data":{},"files":[]}]}')
        self.assertEqual([i["id"] for i in brick.items("in")], ["a"])

    def test_emit_writes_the_metadata_the_container_was_given(self):
        self.task("01M2AAZ9G62NQXFAFCXKRPJEH5", "charge", 3, "ok,rejected")
        brick.emit("ok", [{"id": "a"}])
        self.assertEqual(self.published("ok"), {"meta": {"run_id": "01M2AAZ9G62NQXFAFCXKRPJEH5", "step": "charge", "port": "ok", "attempt": 3, "count": 1, "produced_at": "1970-01-01T00:00:00Z"}, "items": [{"id": "a", "data": {}, "files": []}]})

    def test_emit_refuses_what_the_runner_would_refuse(self):
        self.task(ports="ok")
        with self.assertRaisesRegex(ValueError, "not one this step declares"):
            brick.emit("other", [])
        with self.assertRaises(ValueError):
            brick.emit("../ok", [])
        del os.environ["AGK_RUN_ID"]
        with self.assertRaisesRegex(ValueError, "AGK_RUN_ID"):
            brick.emit("ok", [])

    def test_two_ports_never_spill_to_one_name(self):
        self.task(ports="ok,invalid")
        brick.limits.inline_max = 4
        for port in ("ok", "invalid"):
            brick.emit(port, [{"id": "a", "data": {"note": port + "-heavy"}}])
        self.assertEqual(self.published("ok")["items"][0]["files"][0]["name"], "a-note.json")
        self.assertEqual(self.published("invalid")["items"][0]["files"][0]["name"], "a-note-2.json")

    def test_attach_carries_its_own_digest(self):
        self.task(step="render")
        f = brick.attach("out", "invoice.pdf", "application/pdf", b"%PDF")
        self.assertEqual(f, {"name": "invoice.pdf", "uri": "agk://run/r/render/out/invoice.pdf", "media_type": "application/pdf", "size": 4, "sha256": "315d429b7714cedb6ad04ac31240145257692630457f3c88253c5beceac76027"})
        with self.assertRaises(ValueError):
            brick.attach("out", "../x", "text/plain", b"")

    def test_a_parameter_absent_is_not_a_parameter_zero(self):
        self.task()
        self.write("agk/params.json", '{"retries":0}')
        self.assertEqual(brick.param("retries", 3), 0)
        self.assertEqual(brick.param("timeout", "30s"), "30s")
        with self.assertRaises(KeyError):
            brick.param("timeout")

    def test_the_run_context_joins_run_json_and_the_environment(self):
        self.task("01JMZ8V1P9C4XQ7K2N4D6F8H0A", "charge", 2, "ok,rejected")
        self.write("agk/run.json", '{"id":"01JMZ8V1P9C4XQ7K2N4D6F8H0A","workflow":"monthly-invoicing","namespace":"finance","commit":"a3f9c1e","trigger_kind":"schedule","triggered_by":"finance/agentiik","state":"running","started_at":"2026-10-01T04:03:13Z"}')
        with mock.patch.dict(os.environ, {"AGK_WORKFLOW": "monthly-invoicing@a3f9c1e", "AGK_SHARD": "3/8", "AGK_DEADLINE": "2026-10-01T04:13:13Z", "AGK_REPO": "/agk/repo"}):
            r = brick.context()
        self.assertEqual((r.workflow, r.version, r.trigger_kind, r.attempt, r.shard, r.out_ports, r.repo), ("monthly-invoicing", "a3f9c1e", "schedule", 2, brick.Shard(3, 8), ["ok", "rejected"], "/agk/repo"))
        self.assertEqual(r.deadline, datetime.datetime(2026, 10, 1, 4, 13, 13, tzinfo=datetime.timezone.utc))

    def test_a_secret_is_read_from_its_file_and_a_missing_one_is_said_so(self):
        self.task()
        self.write("agk/secrets/bearer", "s3cret")
        with mock.patch.dict(os.environ, {"BEARER": "from the environment"}):
            self.assertEqual(brick.secret("bearer"), b"s3cret")
            self.assertIsNone(brick.secret("smtp"))
        with self.assertRaises(ValueError):
            brick.secret("../params.json")

    def test_exit_leaves_by_the_bands_and_says_why(self):
        left = []
        log = io.StringIO()
        with mock.patch.object(sys, "stderr", log):
            for leave in (lambda: brick.fail("a"), lambda: brick.retry("b"), lambda: brick.refuse("c"), lambda: brick.exit(125, "d")):
                with self.assertRaises(SystemExit) as e:
                    leave()
                left.append(e.exception.code)
        self.assertEqual(left, [1, 100, 120, 1])
        self.assertTrue(log.getvalue().startswith("a\nb\nc\nd\nexit code 125 is not a brick's to leave with"))


if __name__ == "__main__":
    unittest.main()
