// The TypeScript helper, held to conformance/scenarios.json as the Go and Python ones are, and to
// the same unit tests, name for name, as brick/brick_test.go.

import assert from "node:assert/strict";
import { existsSync, mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import { afterEach, describe, it } from "node:test";
import * as brick from "../src/index.ts";

const scenarios = JSON.parse(readFileSync(new URL("../../conformance/scenarios.json", import.meta.url), "utf8"));

let root = "";
const saved = { ...process.env };

// task sets up a root of its own and the environment the runner sets, as a container would.
function task(runId = "r", step = "s", attempt = 1, ports = "out"): void {
  root = mkdtempSync(join(tmpdir(), "brick-"));
  brick.setRoot(root);
  brick.setStdin(() => "");
  Object.assign(brick.limits, brick.DEFAULT_LIMITS);
  Object.assign(process.env, { AGK_RUN_ID: runId, AGK_STEP: step, AGK_ATTEMPT: String(attempt), AGK_OUT_PORTS: ports });
}

afterEach(() => {
  if (root) rmSync(root, { recursive: true, force: true });
  root = "";
  brick.setRoot("");
  for (const k of Object.keys(process.env)) if (!(k in saved)) delete process.env[k];
  Object.assign(process.env, saved);
});

function write(path: string, content: string): void {
  const full = join(root, path);
  mkdirSync(dirname(full), { recursive: true });
  writeFileSync(full, content);
}

function published(port: string): any {
  return JSON.parse(readFileSync(join(root, "agk/out/ports", `${port}.json`), "utf8"));
}

describe("conformance", () => {
  it("the defaults are the documented ones", () => {
    const l = scenarios.limits;
    assert.deepEqual({ ...brick.DEFAULT_LIMITS }, { inlineMax: l.inline_max_bytes, envelopeMax: l.envelope_max_bytes, maxItems: l.max_items });
  });

  it("exit codes stay in the brick's bands", () => {
    for (const [code, want] of scenarios.exit) assert.equal(brick.exitCode(code), want, String(code));
  });

  it("segments are filed as the engine files them", () => {
    for (const [s, want] of scenarios.segment) assert.equal(brick.segment(s), want, s);
  });

  it("values are encoded as the engine encodes them", () => {
    for (const [value, want] of scenarios.encode) assert.equal(brick.encode(value), want);
  });

  for (const c of scenarios.spill) {
    it(`heavy values are spilled: ${c.name}`, () => {
      task(c.task.run_id, c.task.step, c.task.attempt, c.port);
      brick.limits.inlineMax = c.limits.inline_max_bytes;
      brick.emit(c.port, structuredClone(c.items));
      const got = published(c.port);
      assert.deepEqual(got.items, c.want);
      assert.deepEqual(got.meta, { run_id: c.task.run_id, step: c.task.step, port: c.port, attempt: c.task.attempt, count: 1, produced_at: "1970-01-01T00:00:00Z" });
    });
  }

  for (const c of scenarios.refuse) {
    it(`an envelope the runner would refuse is refused first: ${c.name}`, () => {
      task();
      Object.assign(brick.limits, { inlineMax: c.limits.inline_max_bytes ?? 0, envelopeMax: c.limits.envelope_max_bytes ?? 0, maxItems: c.limits.max_items ?? 0 });
      const items = Array.from({ length: c.items }, (_, i) => ({ id: "x".repeat(i + 1), data: { v: "0123456789" } }));
      assert.throws(() => brick.emit("out", items), (err: Error) => err instanceof brick.TooLarge && err.message.includes(c.says));
      assert.equal(existsSync(join(root, "agk/out/ports/out.json")), false);
    });
  }
});

describe("contract", () => {
  it("an envelope is read from its port", () => {
    task();
    write("agk/in/orders/envelope.json", '{"meta":{"run_id":"r","step":"up","port":"ok","attempt":1,"count":1,"produced_at":"2026-01-31T09:00:00Z"},"items":[{"id":"a","data":{"cents":1234},"files":[]}]}');
    assert.equal(brick.items("orders")[0]!.data.cents, 1234);
  });

  it("a port nobody fed is an empty batch", () => {
    task();
    for (const port of ["in", "reference"]) assert.deepEqual(brick.items(port), []);
  });

  it("the port in is read from standard input when no file is mounted", () => {
    task();
    brick.setStdin(() => '{"meta":{},"items":[{"id":"a","data":{},"files":[]}]}');
    assert.deepEqual(brick.items("in").map((i) => i.id), ["a"]);
  });

  it("emit writes the metadata the container was given", () => {
    task("01M2AAZ9G62NQXFAFCXKRPJEH5", "charge", 3, "ok,rejected");
    brick.emit("ok", [{ id: "a" }]);
    assert.deepEqual(published("ok"), {
      meta: { run_id: "01M2AAZ9G62NQXFAFCXKRPJEH5", step: "charge", port: "ok", attempt: 3, count: 1, produced_at: "1970-01-01T00:00:00Z" },
      items: [{ id: "a", data: {}, files: [] }],
    });
  });

  it("emit refuses what the runner would refuse", () => {
    task("r", "s", 1, "ok");
    assert.throws(() => brick.emit("other", []), /not one this step declares/);
    assert.throws(() => brick.emit("../ok", []));
    delete process.env.AGK_RUN_ID;
    assert.throws(() => brick.emit("ok", []), /AGK_RUN_ID/);
  });

  it("two ports never spill to one name", () => {
    task("r", "s", 1, "ok,invalid");
    brick.limits.inlineMax = 4;
    for (const port of ["ok", "invalid"]) brick.emit(port, [{ id: "a", data: { note: `${port}-heavy` } }]);
    assert.equal(published("ok").items[0].files[0].name, "a-note.json");
    assert.equal(published("invalid").items[0].files[0].name, "a-note-2.json");
  });

  it("attach carries its own digest", () => {
    task("r", "render");
    assert.deepEqual(brick.attach("out", "invoice.pdf", "application/pdf", "%PDF"), {
      name: "invoice.pdf",
      uri: "agk://run/r/render/out/invoice.pdf",
      media_type: "application/pdf",
      size: 4,
      sha256: "315d429b7714cedb6ad04ac31240145257692630457f3c88253c5beceac76027",
    });
    assert.throws(() => brick.attach("out", "../x", "text/plain", ""));
  });

  it("a parameter absent is not a parameter zero", () => {
    task();
    write("agk/params.json", '{"retries":0}');
    assert.equal(brick.param("retries"), 0);
    assert.equal(brick.param("timeout"), undefined);
  });

  it("the run context joins run.json and the environment", () => {
    task("01JMZ8V1P9C4XQ7K2N4D6F8H0A", "charge", 2, "ok,rejected");
    write("agk/run.json", '{"id":"01JMZ8V1P9C4XQ7K2N4D6F8H0A","workflow":"monthly-invoicing","namespace":"finance","commit":"a3f9c1e","trigger_kind":"schedule","triggered_by":"finance/agentiik","state":"running","started_at":"2026-10-01T04:03:13Z"}');
    Object.assign(process.env, { AGK_WORKFLOW: "monthly-invoicing@a3f9c1e", AGK_SHARD: "3/8", AGK_DEADLINE: "2026-10-01T04:13:13Z", AGK_REPO: "/agk/repo" });
    const r = brick.context();
    assert.deepEqual([r.workflow, r.version, r.trigger_kind, r.attempt, r.shard, r.out_ports, r.repo], ["monthly-invoicing", "a3f9c1e", "schedule", 2, { index: 3, count: 8 }, ["ok", "rejected"], "/agk/repo"]);
    assert.equal(r.deadline?.toISOString(), "2026-10-01T04:13:13.000Z");
  });

  it("a secret is read from its file and a missing one is said so", () => {
    task();
    write("agk/secrets/bearer", "s3cret");
    process.env.BEARER = "from the environment";
    assert.equal(brick.secret("bearer")?.toString(), "s3cret");
    assert.equal(brick.secret("smtp"), undefined);
    assert.throws(() => brick.secret("../params.json"));
  });

  it("exit leaves by the bands and says why", (t) => {
    const left: number[] = [];
    let log = "";
    t.mock.method(process, "exit", ((code: number) => {
      left.push(code);
      throw new Error("exited");
    }) as never);
    t.mock.method(process.stderr, "write", ((s: string) => {
      log += s;
      return true;
    }) as never);
    for (const leave of [() => brick.fail("a"), () => brick.retry("b"), () => brick.refuse("c"), () => brick.exit(125, "d")]) {
      assert.throws(leave, /exited/);
    }
    t.mock.restoreAll();
    assert.deepEqual(left, [1, 100, 120, 1]);
    assert.ok(log.startsWith("a\nb\nc\nd\nexit code 125 is not a brick's to leave with"), log);
  });
});
