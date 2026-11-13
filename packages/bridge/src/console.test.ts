import { describe, expect, test } from "bun:test";
import { type ConsoleMessage, encode, hookConsole } from "./console";
import { callSite, utf8Length } from "./console-format";

type Listener = (e: unknown) => void;

/** A page's console and window, hooked, and what the hook posted. */
function setup(max = 4096) {
  const calls: [string, unknown, unknown[]][] = [];
  const console: Record<string, unknown> = {};
  for (const name of ["log", "info", "warn", "error", "debug", "trace", "assert", "table"]) {
    console[name] = function (this: unknown, ...args: unknown[]) {
      calls.push([name, this, args]);
      return name;
    };
  }
  const listeners: Record<string, Listener[]> = {};
  const scope = {
    console,
    addEventListener: (type: string, fn: Listener) => (listeners[type] ??= []).push(fn),
  };
  const sent: ConsoleMessage[] = [];
  let failing = false;
  hookConsole(
    scope,
    (m) => {
      if (failing) throw new Error("torn down");
      sent.push(JSON.parse(m));
    },
    max,
  );
  const fire = (type: string, e: unknown) => listeners[type]?.forEach((fn) => fn(e));
  return { c: console as Record<string, (...args: unknown[]) => unknown>, calls, sent, fire, fail: () => (failing = true) };
}

describe("console calls", () => {
  test("tell Go their level and text, and still reach the console", () => {
    const { c, calls, sent } = setup();
    expect(c.log!("a %s", "b", { x: 1 })).toBe("log");
    c.info!("i");
    c.warn!("w");
    c.error!("e");
    c.debug!("d");
    expect(sent.map((m) => [m.t, m.l, m.v])).toEqual([
      ["console", "log", "a b {x: 1}"],
      ["console", "info", "i"],
      ["console", "warn", "w"],
      ["console", "error", "e"],
      ["console", "debug", "d"],
    ]);
    expect(calls.map(([name, , args]) => [name, args])).toEqual([
      ["log", ["a %s", "b", { x: 1 }]],
      ["info", ["i"]],
      ["warn", ["w"]],
      ["error", ["e"]],
      ["debug", ["d"]],
    ]);
    expect(calls[0]![1]).toBe(c); // called on the console
    expect(sent[0]!.s).toBeUndefined(); // no stack but a trace's
  });

  test("keep the methods' names and the caller's this", () => {
    const { c, calls } = setup();
    for (const name of ["log", "info", "warn", "error", "debug", "trace", "assert"]) expect(c[name]!.name).toBe(name);
    const other = {};
    c.log!.call(other, "x");
    const { log } = c;
    log!("unbound");
    expect(calls.map(([, self]) => self)).toEqual([other, undefined]);
  });

  test("tell where they were made", () => {
    const { c, sent } = setup();
    const here = callSite(new Error().stack, 0)!; c.warn!("here"); // prettier-ignore
    expect(sent[0]).toMatchObject({ u: here.url, n: here.line });
    expect(sent[0]!.c).toBeGreaterThan(here.column);
    // A block, not an expression: JavaScriptCore drops the frame of a
    // function whose last act is the call (a tail call in strict code).
    [1].forEach(() => {
      c.log!("in a callback");
    });
    expect(sent[1]!.u).toBe(here.url);
  });

  test("a trace tells its stack, a failed assertion is an error", () => {
    const { c, calls, sent } = setup();
    c.trace!();
    c.trace!("where %d", 1);
    c.assert!(true, "fine");
    c.assert!(false, "broken: %s", "math");
    c.assert!(0);
    expect(sent.map((m) => [m.l, m.v])).toEqual([
      ["log", "console.trace"],
      ["log", "where 1"],
      ["error", "Assertion failed: broken: math"],
      ["error", "Assertion failed"],
    ]);
    expect(sent[0]!.s).toContain("console.test.ts");
    expect(calls.filter(([name]) => name === "assert").length).toBe(3);
  });

  test("leave other methods alone", () => {
    const { c, calls, sent } = setup();
    c.table!([1]);
    expect(sent).toEqual([]);
    expect(calls.length).toBe(1);
  });

  test("made while one is formatted reach the console only", () => {
    const { c, calls, sent } = setup();
    const sneaky = {
      toString() {
        c.log!("inner");
        return "7";
      },
    };
    c.log!("%d", sneaky);
    expect(sent.map((m) => m.v)).toEqual(["7"]);
    expect(calls.map(([, , args]) => args[0])).toEqual(["inner", "%d"]);
  });

  test("keep working when Go can't be told", () => {
    const { c, calls, fail } = setup();
    fail();
    expect(() => c.error!("x")).not.toThrow();
    expect(calls.length).toBe(1);
  });

  test("tell an error with its stack", () => {
    const { c, sent } = setup();
    const e = new Error("boom");
    e.stack = "Error: boom\n    at f (https://app.test/a.js:1:2)";
    c.error!("failed:", e);
    expect(sent[0]!.v).toBe("failed: Error: boom\nat f (https://app.test/a.js:1:2)");
  });
});

describe("uncaught errors", () => {
  test("come as errors, with their place and stack", () => {
    const { sent, fire } = setup();
    const e = new TypeError("boom");
    e.stack = "TypeError: boom\n    at f (https://app.test/a.js:3:7)\n    at https://app.test/a.js:9:1";
    fire("error", { message: "Uncaught TypeError: boom", filename: "https://app.test/a.js", lineno: 3, colno: 7, error: e });
    expect(sent).toEqual([
      {
        t: "console",
        l: "error",
        v: "Uncaught TypeError: boom",
        u: "https://app.test/a.js",
        n: 3,
        c: 7,
        s: "at f (https://app.test/a.js:3:7)\nat https://app.test/a.js:9:1",
      },
    ]);
  });

  test("of a value or another origin's script", () => {
    const { sent, fire } = setup();
    fire("error", { message: "Uncaught str", filename: "https://app.test/", lineno: 2, colno: 3, error: "str" });
    fire("error", { message: "Script error.", filename: "", lineno: 0, colno: 0, error: null });
    fire("error", { type: "error" }); // an Event, not an ErrorEvent
    expect(sent).toEqual([
      { t: "console", l: "error", v: "Uncaught str", u: "https://app.test/", n: 2, c: 3 },
      { t: "console", l: "error", v: "Uncaught Script error." },
    ]);
  });

  test("unhandled rejections come as errors", () => {
    const { sent, fire } = setup();
    const e = new RangeError("too far");
    e.stack = "load@https://app.test/main.js:4:11\nglobal code@https://app.test/main.js:8:1";
    fire("unhandledrejection", { reason: e });
    fire("unhandledrejection", { reason: "nope" });
    fire("unhandledrejection", { reason: { code: 7 } });
    expect(sent).toEqual([
      {
        t: "console",
        l: "error",
        v: "Uncaught (in promise) RangeError: too far",
        u: "https://app.test/main.js",
        n: 4,
        c: 11,
        s: "load@https://app.test/main.js:4:11\nglobal code@https://app.test/main.js:8:1",
      },
      { t: "console", l: "error", v: "Uncaught (in promise) nope" },
      { t: "console", l: "error", v: "Uncaught (in promise) {code: 7}" },
    ]);
  });
});

describe("encode", () => {
  test("fits a message in max bytes of UTF-8, cutting its longest parts", () => {
    const m: ConsoleMessage = { t: "console", l: "error", v: "é".repeat(5000), u: "https://app.test/", n: 1, c: 1, s: "at f\n".repeat(400) };
    const s = encode({ ...m }, 1000)!;
    expect(utf8Length(s)).toBeLessThanOrEqual(1000);
    const got = JSON.parse(s) as ConsoleMessage;
    expect(got.v).toEndWith("é…");
    expect(got.u).toBe("https://app.test/");
    expect(encode({ ...m, v: "short", s: "" }, 1000)).toBe(JSON.stringify({ ...m, v: "short", s: "" }));
    // Quotes and control characters take more room in JSON.
    const q = encode({ t: "console", l: "log", v: '"\u0001'.repeat(1000) }, 200)!;
    expect(utf8Length(q)).toBeLessThanOrEqual(200);
    expect(encode({ t: "console", l: "log", v: "x" }, 10)).toBeNull(); // no room for any text
  });

  test("hooked calls are cut to max", () => {
    const { c, sent } = setup(300);
    c.log!("ü".repeat(10000));
    expect(utf8Length(JSON.stringify(sent[0]))).toBeLessThanOrEqual(300);
    expect(sent[0]!.v).toEndWith("…");
  });
});
