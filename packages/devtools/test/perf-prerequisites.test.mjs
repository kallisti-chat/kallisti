import assert from "node:assert/strict";
import { execFileSync, spawnSync } from "node:child_process";
import { access, mkdir, mkdtemp, rm, symlink, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { test as nodeTest } from "node:test";
import { fileURLToPath } from "node:url";

const test = process.platform === "win32" ? nodeTest.skip : nodeTest;
// Check another POSIX shell locally, including dash as used by Ubuntu CI.
const shell = process.env.PERF_TEST_SHELL || "/bin/sh";

const harness = fileURLToPath(new URL("../../../tests/perf/run.sh", import.meta.url));
const stopped = "stub go: stopped before build\n";
const fixture = async (t) => {
  const dir = await mkdtemp(join(tmpdir(), "kallisti-perf-"));
  t.after(() => rm(dir, { recursive: true, force: true }));
  const bin = join(dir, "bin");
  const web = join(dir, "web");
  const work = join(dir, "work");
  await mkdir(bin);
  await mkdir(web);
  await writeFile(join(web, "index.html"), "fixture");
  for (const name of ["dirname", "mkdir", "rm"]) {
    const executable = execFileSync(shell, ["-c", 'command -v "$1"', "sh", name], { encoding: "utf8" }).trim();
    await symlink(executable, join(bin, name));
  }
  const tool = async (path, mode = 0o755) => {
    // A prerequisite check must not execute the tool.
    await writeFile(path, "#!/bin/sh\necho 'unexpected tool execution' >&2\nexit 99\n", { mode });
    return path;
  };
  await writeFile(join(bin, "go"), `#!/bin/sh\nprintf '${stopped}'\nexit 73\n`, { mode: 0o755 });
  return {
    bin, dir, tool, work,
    run: (env = {}) => spawnSync(shell, [harness], {
      env: { PATH: bin, WEB_DIR: web, WORK: work, CHROME: join(dir, "no-browser"), ...env },
      encoding: "utf8",
      timeout: 5000,
    }),
  };
};

const assertBuildReached = (result) => {
  assert.ifError(result.error);
  assert.equal(result.status, 73, result.stderr);
  assert.equal(result.stdout, stopped);
  assert.equal(result.stderr, "");
};

for (const overrides of [undefined, "", "off-path", "spaced paths"]) {
  test(`perf prerequisites accept ${overrides === "" ? "empty overrides with PATH" : overrides ?? "default PATH"} tools`, async (t) => {
    const f = await fixture(t);
    const env = {};
    for (const name of ["ffmpeg", "sqlite3"]) {
      const path = await f.tool(join(overrides ? f.dir : f.bin, `${name}${overrides === "spaced paths" ? " tool" : ""}`));
      if (overrides !== undefined) env[name.toUpperCase()] = overrides ? path : "";
    }
    assertBuildReached(f.run(env));
  });
}

for (const name of ["ffmpeg", "sqlite3"]) {
  test(`perf prerequisites accept a ${name} override with the other tool on PATH`, async (t) => {
    const f = await fixture(t);
    await f.tool(join(f.bin, name === "ffmpeg" ? "sqlite3" : "ffmpeg"));
    const path = await f.tool(join(f.dir, `${name} tool`));
    assertBuildReached(f.run({ [name.toUpperCase()]: path }));
  });

  for (const invalid of ["missing", "not executable", "directory"]) {
    test(`perf prerequisites reject a ${invalid} ${name} override before build`, async (t) => {
      const f = await fixture(t);
      // Valid defaults must not hide an invalid explicit override.
      await f.tool(join(f.bin, "ffmpeg"));
      await f.tool(join(f.bin, "sqlite3"));
      const path = join(f.dir, `${name} ${invalid}`);
      if (invalid === "not executable") await f.tool(path, 0o644);
      if (invalid === "directory") await mkdir(path);
      const result = f.run({ [name.toUpperCase()]: path });
      assert.ifError(result.error);
      assert.equal(result.status, 1, result.stderr);
      assert.equal(result.stdout, "");
      assert.ok(result.stderr.includes(name), result.stderr);
      assert.ok(result.stderr.includes(path), result.stderr);
      assert.match(result.stderr, /SEED=0/);
      await assert.rejects(access(f.work), { code: "ENOENT" });
    });
  }

  test(`perf prerequisites reject missing default ${name} before build`, async (t) => {
    const f = await fixture(t);
    await f.tool(join(f.bin, name === "ffmpeg" ? "sqlite3" : "ffmpeg"));
    const result = f.run();
    assert.ifError(result.error);
    assert.equal(result.status, 1, result.stderr);
    assert.equal(result.stdout, "");
    assert.ok(result.stderr.includes(name), result.stderr);
    await assert.rejects(access(f.work), { code: "ENOENT" });
  });
}

for (const overrides of [false, true]) {
  test(`perf SEED=0 skips tool checks with ${overrides ? "invalid overrides" : "no PATH tools"}`, async (t) => {
    const f = await fixture(t);
    assertBuildReached(f.run({
      SEED: "0",
      ...(overrides ? { FFMPEG: join(f.dir, "missing ffmpeg"), SQLITE3: join(f.dir, "missing sqlite3") } : {}),
    }));
  });
}
