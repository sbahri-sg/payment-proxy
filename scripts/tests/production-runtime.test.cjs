const assert = require("node:assert/strict");
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const { randomUUID } = require("node:crypto");
const { spawnSync } = require("node:child_process");
const { before, after, test } = require("node:test");

// Opt-in: use prebuilt local images only. Never deploy the real Compose stack,
// mount its state, publish ports, join its networks, or contact payment providers.
const enabled = process.env.PAYMENT_PROXY_RUN_RUNTIME_TESTS === "1";
const sourceRoot = path.resolve(__dirname, "../..");
let stateDir, env, config;

function command(program, args, overrides = {}) {
  return spawnSync(program, args, {
    cwd: sourceRoot, env: { ...env, ...overrides }, encoding: "utf8", timeout: 15000,
  });
}

function success(result) {
  assert.equal(result.status, 0, result.stderr || result.error?.message || result.stdout);
  return result.stdout.trim();
}

function docker(args, overrides) {
  return success(command("docker", args, overrides));
}

before(() => {
  if (!enabled) return;
  env = { ...process.env };
  for (const key of Object.keys(env)) {
    if (/^(APP_ENV|IMAGE_TAG|CADDY_IMAGE|PAYMENT_PROXY_|POSTGRES_|DATABASE_|CREDENTIAL_|SERVICE_API_KEY|ADMIN_API_KEY|CONNECTOR_|MIDTRANS_|DUITKU_|DOKU_|IPAYMU_|DASHBOARD_|EMISELL_)/.test(key)) delete env[key];
  }
  docker(["info", "--format", "{{.ServerVersion}}"]);
  stateDir = fs.mkdtempSync(path.join(os.tmpdir(), "payment-proxy-runtime-test-"));
  env.PAYMENT_PROXY_DEPLOY_STATE_DIR = stateDir;
  success(command("sh", ["scripts/deploy-production.sh", "init", "payments.example.com"]));
  config = JSON.parse(docker(["compose", "--env-file", path.join(stateDir, "production.env"),
    "-f", "docker-compose.production.yml", "config", "--format", "json"]));
});

after(() => {
  if (stateDir) fs.rmSync(stateDir, { recursive: true, force: true });
});

function start(t, name, extraArgs = []) {
  const service = config.services[name];
  docker(["image", "inspect", service.image, "--format", "{{.Id}}"]);
  assert.ok(!service.ports?.length, `${name} must not publish ports`);
  const args = ["create", "--pull=never", "--name", `payment-runtime-test-${randomUUID()}`,
    "--label", "emisell.payment-proxy.runtime-test=true", "--network", "none"];
  if (service.init) args.push("--init");
  if (service.read_only) args.push("--read-only");
  for (const cap of service.cap_drop ?? []) args.push("--cap-drop", cap);
  for (const cap of service.cap_add ?? []) args.push("--cap-add", cap);
  for (const option of service.security_opt ?? []) args.push("--security-opt", option);
  for (const tmpfs of service.tmpfs ?? []) args.push("--tmpfs", tmpfs);
  args.push("--pids-limit", String(service.pids_limit), "--memory", String(service.mem_limit),
    "--cpus", String(service.cpus));
  const overrides = {};
  for (const [key, value] of Object.entries(service.environment ?? {})) {
    args.push("-e", key);
    overrides[key] = String(value);
  }
  args.push(...extraArgs, service.image);
  const id = docker(args, overrides);
  assert.match(id, /^[a-f0-9]{64}$/);
  // The id belongs to this newly created, isolated test container only.
  t.after(() => docker(["rm", "-f", id]));
  docker(["start", id]);
  const inspected = JSON.parse(docker(["inspect", id]))[0];
  assert.equal(inspected.HostConfig.NetworkMode, "none");
  assert.equal(Object.keys(inspected.HostConfig.PortBindings ?? {}).length, 0);
  return { id, service };
}

const pause = ms => new Promise(resolve => setTimeout(resolve, ms));

async function ready(id, service) {
  assert.equal(service.healthcheck.test[0], "CMD");
  const probe = service.healthcheck.test.slice(1);
  const deadline = Date.now() + 10000;
  let lastError;
  while (Date.now() < deadline) {
    const result = command("docker", ["exec", id, ...probe]);
    if (result.status === 0) return probe;
    lastError = result.stderr;
    const state = JSON.parse(docker(["inspect", "--format", "{{json .State}}", id]));
    assert.equal(state.Running, true, state.Error || docker(["logs", "--tail", "20", id]));
    await pause(100);
  }
  assert.fail(`Isolated container health check timed out: ${lastError}`);
}

function pids(id) {
  // Include cgroup v1 and v2 hosts; this read also counts its own transient task.
  const value = docker(["exec", id, "sh", "-c",
    "if [ -r /sys/fs/cgroup/pids.current ]; then cat /sys/fs/cgroup/pids.current; else cat /sys/fs/cgroup/pids/pids.current; fi"]);
  assert.match(value, /^\d+$/);
  return Number(value);
}

test("Caddy starts with the real production capability restrictions and HTTP-only listener", { skip: !enabled }, async t => {
  const { id, service } = start(t, "gateway", ["--tmpfs", "/data", "--tmpfs", "/config",
    "--mount", `type=bind,source=${path.join(sourceRoot, "deploy/Caddyfile")},target=/etc/caddy/Caddyfile,readonly`]);
  await ready(id, service);
  const adapted = JSON.parse(docker(["exec", id, "wget", "-q", "-O", "-", "http://127.0.0.1:2019/config/"]));
  const servers = Object.values(adapted.apps.http.servers);
  assert.deepEqual(servers.flatMap(server => server.listen), [":8080"]);
  assert.ok(servers.every(server => server.automatic_https.disable));
  assert.deepEqual(service.cap_drop, ["ALL"]);
  assert.deepEqual(service.cap_add, ["NET_BIND_SERVICE"]);
});

for (const name of ["connector-runner", "midtrans-provider-app", "duitku-provider-app", "doku-provider-app", "ipaymu-provider-app"]) {
  test(`${name}: 100 TLS health checks do not exhaust the production PID budget`, { skip: !enabled }, async t => {
    const { id, service } = start(t, name);
    assert.equal(service.init, true);
    assert.equal(service.pids_limit, 256);
    const probe = await ready(id, service);
    assert.ok(probe.some(arg => String(arg).startsWith("https://127.0.0.1:")));
    // Warm up TLS/runtime workers before sampling; no provider request is made.
    for (let i = 0; i < 10; i++) docker(["exec", id, ...probe]);
    await pause(100);
    const baseline = pids(id);
    for (let i = 0; i < 100; i++) docker(["exec", id, ...probe]);
    await pause(100);
    const current = pids(id);
    // Allow normal runtime thread variation, but detect one leaked PID per probe.
    assert.ok(current <= baseline + 8, `${name} leaked health-check child tasks: ${baseline} -> ${current}`);
    assert.ok(current < service.pids_limit / 2, `${name} has insufficient PID headroom`);
    t.diagnostic(`100 TLS probes succeeded; PID tasks ${baseline} -> ${current}, limit ${service.pids_limit}`);
  });
}
