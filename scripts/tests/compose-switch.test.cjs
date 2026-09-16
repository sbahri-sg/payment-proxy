const assert = require("node:assert/strict");
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const { spawnSync } = require("node:child_process");
const { X509Certificate, createPrivateKey, createPublicKey } = require("node:crypto");
const { test } = require("node:test");

const sourceRoot = path.resolve(__dirname, "../..");

function fixture(t, dotenv) {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), "payment-proxy-compose-test-"));
  t.after(() => fs.rmSync(root, { recursive: true, force: true }));
  fs.mkdirSync(path.join(root, "scripts"));
  for (const file of ["scripts/compose.sh", "scripts/deploy-production.sh", "docker-compose.yml", "docker-compose.production.yml"]) {
    fs.copyFileSync(path.join(sourceRoot, file), path.join(root, file));
  }
  if (dotenv !== undefined) fs.writeFileSync(path.join(root, ".env"), dotenv);
  const bin = path.join(root, "bin");
  fs.mkdirSync(bin);
  fs.writeFileSync(path.join(bin, "docker"), '#!/bin/sh\nprintf "%s\\n" "$*" >> "$DOCKER_TEST_LOG"\nexit "${DOCKER_TEST_EXIT_CODE:-0}"\n', { mode: 0o700 });
  const env = cleanEnvironment();
  env.PATH = bin + path.delimiter + process.env.PATH;
  env.PAYMENT_PROXY_DEPLOY_STATE_DIR = path.join(root, ".deploy");
  env.DOCKER_TEST_LOG = path.join(root, "docker.log");
  const run = (args, overrides = {}) => spawnSync("sh", [path.join(root, "scripts/compose.sh"), ...args], {
    cwd: os.tmpdir(), env: { ...env, ...overrides }, encoding: "utf8", timeout: 30000,
  });
  const calls = () => fs.existsSync(env.DOCKER_TEST_LOG) ? fs.readFileSync(env.DOCKER_TEST_LOG, "utf8").trim().split("\n") : [];
  return { root, env, run, calls };
}

function cleanEnvironment() {
  const env = { ...process.env };
  // Never inherit the developer's live settings. Unit tests use a Docker stub;
  // the integration check below only renders Compose, never contacts its daemon.
  for (const key of Object.keys(env)) {
    if (/^(APP_ENV|PAYMENT_PROXY_|POSTGRES_|DATABASE_|CREDENTIAL_|SERVICE_API_KEY|ADMIN_API_KEY|CONNECTOR_|MIDTRANS_|DUITKU_|DOKU_|IPAYMU_|DASHBOARD_|EMISELL_)/.test(key)) delete env[key];
  }
  return env;
}

function requireSuccess(result) {
  assert.equal(result.status, 0, result.stderr || result.error?.message || result.stdout);
}

test("production Caddy uses internal HTTP and keeps the canonical HTTPS origin", () => {
  const caddyfile = fs.readFileSync(path.join(sourceRoot, "deploy/Caddyfile"), "utf8");
  assert.match(caddyfile, /^\s*auto_https off\s*$/m);
  assert.match(caddyfile, /^http:\/\/\{\$PAYMENT_PROXY_DOMAIN\}:8080\s*\{/m);
  assert.doesNotMatch(caddyfile, /^\s*(tls\b|issuer\b|https:\/\/|:80\b|:443\b)/m);
  for (const upstream of ["api:8080", "dashboard:3000"]) {
    const block = caddyfile.match(new RegExp(`reverse_proxy ${upstream} \\{([\\s\\S]*?)\\n\\s*\\}`));
    assert.ok(block, `${upstream} reverse proxy block missing`);
    assert.match(block[1], /header_up Host \{\$PAYMENT_PROXY_DOMAIN\}/);
    assert.match(block[1], /header_up X-Forwarded-Host \{\$PAYMENT_PROXY_DOMAIN\}/);
    assert.match(block[1], /header_up X-Forwarded-Proto https/);
  }
});

test("development reads quoted dotenv metadata and selects only local Compose", t => {
  const f = fixture(t, ' export APP_ENV = "development" # local\r\n');
  requireSuccess(f.run(["up", "-d", "--build", "--wait"]));
  assert.deepEqual(f.calls(), [`compose --env-file ${f.root}/.env -f ${f.root}/docker-compose.yml up -d --build --wait`]);
  assert.equal(fs.existsSync(path.join(f.root, ".deploy")), false);
});

test("production bootstraps TLS/secrets once, excludes local secrets, and preserves them on rerun", t => {
  const f = fixture(t, [
    "APP_ENV='production' # first install",
    "PAYMENT_PROXY_DOMAIN=payments.example.com",
    "SERVICE_API_KEY=local-development-secret",
    "ADMIN_API_KEY=local-development-admin",
    "CREDENTIAL_ENCRYPTION_KEY=invalid-development-key",
    "EMISELL_BACKEND_WEBHOOK_URL=http://emisell-receiver:19090/webhooks/v1/payment-proxy",
  ].join("\n"));
  requireSuccess(f.run(["up", "-d", "--build", "--wait"]));
  const statePath = path.join(f.root, ".deploy/production.env");
  const state = fs.readFileSync(statePath, "utf8");
  assert.match(state, /^APP_ENV=production$/m);
  assert.match(state, /^PAYMENT_PROXY_DOMAIN=payments.example.com$/m);
  assert.match(state, /^SERVICE_API_KEY=[a-f0-9]{64}$/m);
  assert.match(state, /^ADMIN_API_KEY=[a-f0-9]{64}$/m);
  assert.match(state, /^EMISELL_BACKEND_WEBHOOK_URL=$/m);
  assert.doesNotMatch(state, /local-development|invalid-development|emisell-receiver/);
  for (const provider of ["XENDIT", "MIDTRANS", "DUITKU", "DOKU", "IPAYMU"]) {
    const cert = state.match(new RegExp(`^${provider}_CONNECTOR_TLS_CERT_BASE64=(.+)$`, "m"));
    const key = state.match(new RegExp(`^${provider}_CONNECTOR_TLS_KEY_BASE64=(.+)$`, "m"));
    assert.ok(cert && key, `${provider} TLS missing`);
    assert.match(Buffer.from(cert[1], "base64").toString(), /BEGIN CERTIFICATE/);
    assert.match(Buffer.from(key[1], "base64").toString(), /BEGIN PRIVATE KEY/);
  }
  assert.equal(fs.statSync(statePath).mode & 0o777, 0o600);
  assert.equal(f.calls().length, 2);
  assert.match(f.calls()[0], /docker-compose.production.yml config --quiet$/);
  assert.match(f.calls()[1], /docker-compose.production.yml up -d --build --wait$/);

  // Shell mode override wins, but a later .env edit does not rotate live keys/domain.
  fs.writeFileSync(path.join(f.root, ".env"), "APP_ENV=development\nPAYMENT_PROXY_DOMAIN=other.example.com\n");
  requireSuccess(f.run(["up", "-d"], { APP_ENV: "production" }));
  assert.equal(fs.readFileSync(statePath, "utf8"), state);
  assert.equal(f.calls().length, 3, "rerun unexpectedly initialized or removed a stack");
  assert.match(f.calls()[2], /docker-compose.production.yml up -d$/);
});

test("production can infer hostname from HTTPS public URL and accepts a separate HTTPS callback", t => {
  const f = fixture(t, "APP_ENV=production\nPAYMENT_PROXY_PUBLIC_BASE_URL=https://payments.example.com/\nPAYMENT_PROXY_PRODUCTION_WEBHOOK_URL=https://api.example.com/webhooks/payment-proxy\n");
  requireSuccess(f.run(["build"]));
  const state = fs.readFileSync(path.join(f.root, ".deploy/production.env"), "utf8");
  assert.match(state, /^PAYMENT_PROXY_DOMAIN=payments.example.com$/m);
  assert.match(state, /^EMISELL_BACKEND_WEBHOOK_URL=https:\/\/api.example.com\/webhooks\/payment-proxy$/m);
  assert.match(f.calls().at(-1), /docker-compose.production.yml build$/);
});

test("read-only production commands never initialize a missing deployment", t => {
  const f = fixture(t, "APP_ENV=production\nPAYMENT_PROXY_DOMAIN=payments.example.com\n");
  assert.equal(f.run(["ps", "-a"]).status, 1);
  assert.deepEqual(f.calls(), []);
  assert.equal(fs.existsSync(path.join(f.root, ".deploy")), false);
});

test("invalid modes and dotenv shell substitution fail without execution or Docker calls", t => {
  const f = fixture(t, "APP_ENV=development\n");
  assert.equal(f.run(["up"], { APP_ENV: "staging" }).status, 1);
  const marker = path.join(f.root, "executed");
  fs.writeFileSync(path.join(f.root, ".env"), `APP_ENV=$(touch ${marker})\n`);
  assert.equal(f.run(["up"]).status, 1);
  assert.equal(fs.existsSync(marker), false);
  fs.writeFileSync(path.join(f.root, ".env"), 'APP_ENV="production\n');
  assert.equal(f.run(["up"]).status, 1);
  assert.deepEqual(f.calls(), []);
});

test("production rejects an invalid hostname before generating secrets", t => {
  const f = fixture(t, "APP_ENV=production\nPAYMENT_PROXY_PUBLIC_BASE_URL=https://payments.example.com/api\n");
  assert.equal(f.run(["up"]).status, 1);
  assert.deepEqual(f.calls(), []);
  assert.equal(fs.existsSync(path.join(f.root, ".deploy")), false);
});

test("launcher preserves Docker exit code and rejects topology overrides", t => {
  const f = fixture(t, "APP_ENV=development\n");
  assert.equal(f.run(["build"], { DOCKER_TEST_EXIT_CODE: "42" }).status, 42);
  assert.equal(f.run(["-f", "other.yml", "up"]).status, 1);
  assert.equal(f.calls().length, 1);
});

test("missing development .env fails clearly, while help does not require a configuration", t => {
  const f = fixture(t);
  assert.equal(f.run(["up"]).status, 1);
  requireSuccess(f.run(["--help"]));
  assert.deepEqual(f.calls(), []);
});

test("real Compose validates first production bootstrap, HTTPS health checks, and matching TLS keys", t => {
  const env = cleanEnvironment();
  if (spawnSync("docker", ["compose", "version"], { env, encoding: "utf8" }).status !== 0) {
    t.skip("Docker Compose CLI is unavailable");
    return;
  }
  const stateDir = fs.mkdtempSync(path.join(os.tmpdir(), "payment-proxy-compose-config-test-"));
  t.after(() => fs.rmSync(stateDir, { recursive: true, force: true }));
  Object.assign(env, {
    APP_ENV: "production", PAYMENT_PROXY_DOMAIN: "payments.example.com",
    PAYMENT_PROXY_PRODUCTION_WEBHOOK_URL: "", PAYMENT_PROXY_DEPLOY_STATE_DIR: stateDir,
  });
  const initialized = spawnSync("sh", [path.join(sourceRoot, "scripts/compose.sh"), "config", "--quiet"], {
    env, encoding: "utf8", timeout: 30000,
  });
  requireSuccess(initialized);
  const rendered = spawnSync("docker", ["compose", "--env-file", path.join(stateDir, "production.env"),
    "-f", path.join(sourceRoot, "docker-compose.production.yml"), "config", "--format", "json"], {
    env, encoding: "utf8", timeout: 15000,
  });
  requireSuccess(rendered);
  const services = JSON.parse(rendered.stdout).services;
  assert.deepEqual(services.gateway.expose.map(String), ["8080"]);
  assert.ok(!services.gateway.cap_add?.includes("NET_BIND_SERVICE"), "gateway does not need privileged-port binding");
  for (const [name, service] of Object.entries(services)) {
    assert.ok(!service.ports?.length, `${name} must not publish host ports, including 80 and 443`);
  }
  for (const name of ["connector-runner", "midtrans-provider-app", "duitku-provider-app", "doku-provider-app", "ipaymu-provider-app"]) {
    const service = services[name];
    assert.equal(service.environment.APP_ENV, "production");
    assert.ok(service.healthcheck.test.some(arg => String(arg).startsWith("https://127.0.0.1:")), `${name} health check must use TLS`);
    assert.ok(!service.ports?.length, `${name} must not publish its control port`);
    const cert = new X509Certificate(Buffer.from(service.environment.CONNECTOR_TLS_CERT_BASE64, "base64"));
    const key = createPrivateKey(Buffer.from(service.environment.CONNECTOR_TLS_KEY_BASE64, "base64"));
    assert.equal(cert.checkHost(name), name);
    assert.equal(cert.publicKey.export({ type: "spki", format: "pem" }), createPublicKey(key).export({ type: "spki", format: "pem" }));
  }
  assert.equal(services.api.environment.APP_ENV, "production");
  assert.ok(services.api.environment.CONNECTOR_RUNNER_BASE_URLS.split(",").every(url => url.startsWith("https://")));
  assert.equal(services.api.environment.WEBHOOK_ALLOW_INSECURE_HTTP, "false");
  assert.equal(services.api.environment.WEBHOOK_ALLOW_PRIVATE_NETWORKS, "false");
  assert.equal(Buffer.from(services.api.environment.CREDENTIAL_ENCRYPTION_KEY, "base64").length, 32);
});
