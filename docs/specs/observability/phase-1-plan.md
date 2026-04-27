# Observability Phase 1 — OBS VM Stand-up — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Provision a separate $4/mo DigitalOcean droplet running VictoriaMetrics + Grafana + Caddy + node-exporter, deployable from GitHub Actions, presenting a TLS-terminated Grafana dashboard at `https://<OBS_DOMAIN>/` showing the OBS VM's own host metrics.

**Architecture:** A new `obs/` directory at repo root holds an independent Docker Compose stack and its config. A new GitHub Actions workflow (`.github/workflows/deploy-obs.yml`) deploys it to a separate DO droplet over SSH, mirroring the pattern of the existing `deploy.yml`. Caddy fronts Grafana with Let's Encrypt TLS and HTTP basic-auth. VictoriaMetrics is the metrics store; Grafana datasources and dashboards are file-provisioned. node-exporter on the OBS VM provides host metrics so we have something to display end-to-end before any game-droplet exporters land.

**Tech Stack:** Docker Compose v2, VictoriaMetrics single-node (`victoriametrics/victoria-metrics`), Grafana OSS, Caddy v2 (Let's Encrypt + basic-auth), prom/node-exporter, GitHub Actions + `appleboy/ssh-action`, DigitalOcean Droplets.

**Reference:** [`design.md`](./design.md) — the spec this plan implements. Phase 1 deliverables are listed there; Phases 2 (game-droplet exporters) and 3 (Go app instrumentation) get their own plans after Phase 1 ships.

---

## Pre-flight checklist (do before starting)

- [ ] You are on a feature branch (e.g. `leyew-dev`) — not `main`.
- [ ] You have read [`design.md`](./design.md) end-to-end. Understanding the network topology, the basic-auth posture, and the 512 MB sizing rationale will let you make sensible judgment calls in this plan.
- [ ] You have access to the repo's GitHub Actions Secrets/Variables settings — you'll add new ones in Task 11.
- [ ] You have an account at DigitalOcean and can provision droplets there. The runbook step (Task 12) walks through this; if not, escalate to a maintainer.

## File structure (what gets created)

```
obs/
├── .env.example                # Documents the env vars OBS deploy expects (vs. the root .env.example which is for the game stack)
├── docker-compose.yml          # 4 services: victoria-metrics, grafana, caddy, node-exporter
├── Caddyfile                   # TLS + basic-auth gate, reverse-proxy to Grafana
├── setup-swap.sh               # Idempotent 1 GB swapfile creation, run once on the droplet
├── victoria-metrics/
│   └── scrape.yml              # Prometheus-format scrape config: VM self-scrape + local node-exporter
└── grafana/
    └── provisioning/
        ├── datasources/
        │   └── prometheus.yml  # Grafana "Prometheus" datasource pointing at http://victoria-metrics:8428
        └── dashboards/
            ├── dashboards.yml  # Tells Grafana to load *.json files from this dir
            └── node-exporter.json  # Standard node-exporter dashboard (Grafana ID 1860)

.github/workflows/
└── deploy-obs.yml              # New workflow, mirrors deploy.yml shape but targets the OBS droplet

docs/specs/foundations/
└── 09-ci-cd-deploy.md          # Existing file; add a new "OBS observability droplet" section
```

Each file has one clear responsibility. The `obs/` tree is self-contained — nothing in it touches the game droplet's compose file or env. That isolation is what makes it independently deployable.

---

## Task 1: Create `obs/.env.example`

Document the env vars the OBS stack expects. The OBS droplet has its own `.env` file; we never share the game droplet's `.env`.

**Files:**
- Create: `obs/.env.example`

- [ ] **Step 1: Create the directory and file**

```bash
mkdir -p obs
```

- [ ] **Step 2: Write the env-example file**

Write to `obs/.env.example`:

```text
# OBS observability droplet env. Copy to obs/.env and fill in.
# docker-compose -f obs/docker-compose.yml auto-loads obs/.env from this directory.
#
# obs/.env is gitignored (.env is matched anywhere by .gitignore).
# The deploy workflow writes this file onto the OBS droplet from repo
# Variables + Secrets — same pattern as the game-droplet deploy.

# ── Grafana access (Caddy basic-auth gate) ────────────────────────────────
# Username is hardcoded to `nusgreyhats` in obs/Caddyfile — it's not a
# secret and doesn't vary per deployment. Only the password (bcrypt-
# hashed below) needs to be configured.
#
# bcrypt hash from `caddy hash-password`. Single shared mod credential
# (see design.md decisions log). Rotate by re-running hash-password and
# re-deploying.
# Generate with: docker run --rm caddy:2-alpine caddy hash-password --plaintext "<your-password>"
#
# IMPORTANT: bcrypt hashes contain `$` characters. docker-compose's env_file
# parser applies parameter expansion to UNQUOTED values, which mangles the
# hash. Wrap the value in single quotes so docker-compose passes it through
# literally:
#     OBS_BASIC_AUTH_HASH='$2a$14$...'
OBS_BASIC_AUTH_HASH=

# ── Public domain (Caddy TLS via Let's Encrypt) ───────────────────────────
# DNS A-record must already point to the OBS droplet's public IP.
# Caddy obtains and renews the cert automatically on first start.
OBS_DOMAIN=

# ── (No LE email registration) ────────────────────────────────────────────
# Caddy's `email` directive requires a non-empty value, which makes
# OBS_LE_EMAIL awkward to support optionally. Caddy auto-renews well
# before expiry so the LE warning emails are not load-bearing — we drop
# the directive entirely and don't register an email with Let's Encrypt.
```

- [ ] **Step 3: Verify .env will be gitignored**

```bash
git check-ignore -v obs/.env || echo "WARN: obs/.env is NOT gitignored"
```

Expected: `.gitignore:23:.env	obs/.env` (or similar path showing the rule that matches). The `.env` rule from the existing `.gitignore` matches at any depth, so `obs/.env` is covered.

- [ ] **Step 4: Commit**

```bash
git add obs/.env.example
git commit -m "obs: env-example for observability droplet"
```

---

## Task 2: Swap setup script

A 1 GB swapfile gives the 512 MB droplet headroom for unexpected memory spikes (per design.md "512 MB mitigations"). The script is idempotent so re-running on a droplet that already has swap is a no-op.

**Files:**
- Create: `obs/setup-swap.sh`

- [ ] **Step 1: Write the script**

Write to `obs/setup-swap.sh`:

```bash
#!/usr/bin/env bash
# Create a 1 GB swapfile on /swapfile if one isn't already active.
# Idempotent: safe to re-run.
#
# Run once during droplet provisioning, before `docker compose up`.
# See docs/specs/observability/design.md "512 MB mitigations".

set -euo pipefail

SWAPFILE=/swapfile
SIZE_MB=1024

if swapon --show=NAME --noheadings | grep -qx "${SWAPFILE}"; then
    echo "swap: ${SWAPFILE} already active — nothing to do"
    exit 0
fi

if [ -f "${SWAPFILE}" ]; then
    echo "swap: ${SWAPFILE} exists but isn't active; activating"
else
    echo "swap: creating ${SWAPFILE} (${SIZE_MB} MB)"
    fallocate -l "${SIZE_MB}M" "${SWAPFILE}" || \
        dd if=/dev/zero of="${SWAPFILE}" bs=1M count="${SIZE_MB}"
    chmod 600 "${SWAPFILE}"
    mkswap "${SWAPFILE}"
fi

swapon "${SWAPFILE}"

# Persist across reboots.
if ! grep -qE "^${SWAPFILE}\\s+none\\s+swap" /etc/fstab; then
    echo "${SWAPFILE} none swap sw 0 0" >> /etc/fstab
fi

# Conservative swappiness: only swap under real memory pressure.
sysctl -w vm.swappiness=10 >/dev/null
if ! grep -q '^vm.swappiness' /etc/sysctl.d/99-swappiness.conf 2>/dev/null; then
    echo "vm.swappiness=10" > /etc/sysctl.d/99-swappiness.conf
fi

echo "swap: done"
swapon --show
```

- [ ] **Step 2: Make it executable**

```bash
chmod +x obs/setup-swap.sh
```

- [ ] **Step 3: Validate shell syntax**

```bash
bash -n obs/setup-swap.sh
```

Expected: no output, exit 0.

- [ ] **Step 4: Commit**

```bash
git add obs/setup-swap.sh
git commit -m "obs: idempotent swap setup script for OBS droplet"
```

---

## Task 3: VictoriaMetrics scrape config

VictoriaMetrics single-node uses a Prometheus-format `scrape.yml`. Initial config has just two jobs: VM self-scrape and the local node-exporter on the OBS VM. Phase 2 will add a `game-droplet` job to this same file.

**Files:**
- Create: `obs/victoria-metrics/scrape.yml`

- [ ] **Step 1: Create the directory and file**

```bash
mkdir -p obs/victoria-metrics
```

- [ ] **Step 2: Write the scrape config**

Write to `obs/victoria-metrics/scrape.yml`:

```yaml
# VictoriaMetrics scrape config (Prometheus-format).
# Single-node VM reads this via -promscrape.config flag (see obs/docker-compose.yml).
#
# Phase 1: just the OBS VM itself (VM self-scrape + local node-exporter).
# Phase 2 will add a 'game-droplet' job pointing at the existing client
# Caddy's /_metrics/* paths.

global:
  scrape_interval: 15s
  scrape_timeout: 10s

scrape_configs:
  # VictoriaMetrics's own internal metrics. Useful for monitoring the
  # monitoring stack (memory pressure, scrape failures, ingestion rate).
  - job_name: victoria-metrics
    static_configs:
      - targets: ["victoria-metrics:8428"]

  # Local node-exporter on the OBS VM. Reaches it over the Docker network
  # by service name; node-exporter binds to :9100 inside the network.
  - job_name: node-exporter-obs
    static_configs:
      - targets: ["node-exporter:9100"]
        labels:
          host: obs
```

- [ ] **Step 3: Validate YAML syntax**

```bash
python3 -c "import yaml; yaml.safe_load(open('obs/victoria-metrics/scrape.yml'))" && echo "YAML OK"
```

Expected: `YAML OK`.

- [ ] **Step 4: Commit**

```bash
git add obs/victoria-metrics/scrape.yml
git commit -m "obs: VictoriaMetrics scrape config (VM self + local node-exporter)"
```

---

## Task 4: Grafana datasource provisioning

Tell Grafana about the VictoriaMetrics datasource at boot, so we never click anything in the UI. VictoriaMetrics serves the Prometheus query API at `http://victoria-metrics:8428` — Grafana speaks to it via the standard `prometheus` datasource type.

**Files:**
- Create: `obs/grafana/provisioning/datasources/prometheus.yml`

- [ ] **Step 1: Create the directory tree**

```bash
mkdir -p obs/grafana/provisioning/datasources
mkdir -p obs/grafana/provisioning/dashboards
```

- [ ] **Step 2: Write the datasource provisioning file**

Write to `obs/grafana/provisioning/datasources/prometheus.yml`:

```yaml
# Grafana provisioned datasource pointing at VictoriaMetrics.
# Loaded automatically at Grafana boot — no UI clicks needed.
#
# isDefault: true means dashboards that reference an unknown datasource UID
# (e.g. community dashboards downloaded from grafana.com) fall back to this
# one. Saves us from rewriting the UID inside every dashboard JSON.

apiVersion: 1

datasources:
  - name: Prometheus
    type: prometheus
    uid: prometheus
    access: proxy
    url: http://victoria-metrics:8428
    isDefault: true
    editable: false
    jsonData:
      timeInterval: "15s"
      httpMethod: POST
      prometheusType: Prometheus
      prometheusVersion: "2.50.0"
```

- [ ] **Step 3: Validate YAML**

```bash
python3 -c "import yaml; yaml.safe_load(open('obs/grafana/provisioning/datasources/prometheus.yml'))" && echo "YAML OK"
```

Expected: `YAML OK`.

- [ ] **Step 4: Commit**

```bash
git add obs/grafana/provisioning/datasources/prometheus.yml
git commit -m "obs: Grafana provisioned datasource for VictoriaMetrics"
```

---

## Task 5: Grafana dashboard provisioner config

Tell Grafana to scan a directory for dashboard JSON files and load them all on boot.

**Files:**
- Create: `obs/grafana/provisioning/dashboards/dashboards.yml`

- [ ] **Step 1: Write the dashboards provisioner file**

Write to `obs/grafana/provisioning/dashboards/dashboards.yml`:

```yaml
# Grafana dashboard provisioner. Loads any *.json file in the same
# directory as a dashboard, on Grafana boot and on file change.
#
# Add new dashboards by dropping JSON files alongside this one. They'll
# show up under General/<file>.json in Grafana's dashboard browser.

apiVersion: 1

providers:
  - name: file-provisioned
    orgId: 1
    folder: ""
    type: file
    disableDeletion: true
    updateIntervalSeconds: 30
    allowUiUpdates: true
    options:
      path: /etc/grafana/provisioning/dashboards
      foldersFromFilesStructure: false
```

- [ ] **Step 2: Validate YAML**

```bash
python3 -c "import yaml; yaml.safe_load(open('obs/grafana/provisioning/dashboards/dashboards.yml'))" && echo "YAML OK"
```

Expected: `YAML OK`.

- [ ] **Step 3: Commit**

```bash
git add obs/grafana/provisioning/dashboards/dashboards.yml
git commit -m "obs: Grafana dashboard provisioner config"
```

---

## Task 6: Add node-exporter dashboard JSON

Use the standard "Node Exporter Full" community dashboard (Grafana dashboard ID `1860`). It's the de facto choice for node-exporter visualization.

**Files:**
- Create: `obs/grafana/provisioning/dashboards/node-exporter.json`

- [ ] **Step 1: Download dashboard ID 1860**

```bash
curl -fsSL "https://grafana.com/api/dashboards/1860/revisions/latest/download" \
  -o obs/grafana/provisioning/dashboards/node-exporter.json
```

Expected: a ~1 MB JSON file. If the download returns HTML or an error, the dashboard endpoint may have changed; in that case grab the JSON manually from <https://grafana.com/grafana/dashboards/1860-node-exporter-full/> and save it to the same path.

- [ ] **Step 2: Validate JSON**

```bash
python3 -c "import json; d = json.load(open('obs/grafana/provisioning/dashboards/node-exporter.json')); print('JSON OK; title:', d.get('title'))"
```

Expected output: `JSON OK; title: Node Exporter Full`.

- [ ] **Step 3: Commit**

```bash
git add obs/grafana/provisioning/dashboards/node-exporter.json
git commit -m "obs: Grafana dashboard — Node Exporter Full (ID 1860)"
```

---

## Task 7: Caddyfile

Caddy fronts Grafana with Let's Encrypt TLS and HTTP basic-auth. Single virtual host on `${OBS_DOMAIN}` reverse-proxies everything to `grafana:3000`.

**Files:**
- Create: `obs/Caddyfile`

- [ ] **Step 1: Write the Caddyfile**

Write to `obs/Caddyfile`:

```text
# Caddy v2 config for the OBS observability droplet.
# Reads env vars from the container's environment (set by env_file in
# obs/docker-compose.yml). Caddy substitutes {$VAR} at parse time.
#
# Auth posture (see design.md § "Grafana auth"):
#   - Caddy basic-auth gates every request.
#   - Grafana itself runs in anonymous-Editor mode — mods past the gate
#     can view AND edit dashboards without a second login. The Grafana
#     admin login form is disabled and basic-auth API is off, so the
#     internal admin user is dormant. Provisioning still works (it's
#     internal, doesn't go through HTTP auth).
#
# We don't set a global `email` directive — it requires a non-empty value
# and Caddy auto-renews well before expiry, so LE warning emails aren't
# load-bearing.

{$OBS_DOMAIN} {
    encode gzip

    # Single shared mod credential. Username is hardcoded to `nusgreyhats` —
    # it's just a string mods type at the basic-auth prompt and provides
    # no security beyond the password (the bcrypt-hashed half), so we
    # don't manage it as a secret. The hash is supplied via env.
    basic_auth {
        nusgreyhats {$OBS_BASIC_AUTH_HASH}
    }

    # Everything else → Grafana. Grafana's frontend handles all routing
    # under /, including /api/, /login/, /d/, etc.
    reverse_proxy grafana:3000
}
```

- [ ] **Step 2: Validate Caddyfile syntax**

We can't run `caddy validate` directly without env vars set; substitute placeholders for the validate step:

```bash
docker run --rm \
  -e OBS_DOMAIN=obs.example.com \
  -e OBS_BASIC_AUTH_HASH='$2a$14$placeholderhashplaceholderhashplaceholderhashplaceholder' \
  -v "$(pwd)/obs/Caddyfile:/etc/caddy/Caddyfile:ro" \
  caddy:2-alpine \
  caddy validate --config /etc/caddy/Caddyfile --adapter caddyfile
```

Expected: `Valid configuration` (warnings about auto-https are fine). If you see `parse error`, fix the syntax before continuing.

- [ ] **Step 3: Commit**

```bash
git add obs/Caddyfile
git commit -m "obs: Caddyfile — TLS + basic-auth + reverse-proxy to Grafana"
```

---

## Task 8: docker-compose.yml

The four services tied together. Pinned image versions for reproducibility. Read carefully — the bind-mount and flag list for node-exporter is the gotcha called out in design.md.

**Files:**
- Create: `obs/docker-compose.yml`

- [ ] **Step 1: Write the compose file**

Write to `obs/docker-compose.yml`:

```yaml
# OBS observability stack.
# Run from the obs/ directory: `docker compose up -d --build`
# (compose auto-loads obs/.env from this directory).
#
# Architecture: see ../docs/specs/observability/design.md.
# Phase 1 scope: stand up the platform monitoring itself.
# Game-droplet exporters and app metrics arrive in Phases 2 and 3.

services:
  victoria-metrics:
    image: victoriametrics/victoria-metrics:v1.108.1
    container_name: obs-victoria-metrics
    command:
      - "-storageDataPath=/storage"
      - "-retentionPeriod=7d"
      - "-promscrape.config=/etc/victoria-metrics/scrape.yml"
      - "-search.maxSeries=10000"
      - "-memory.allowedPercent=70"
      - "-httpListenAddr=:8428"
    volumes:
      - vm-data:/storage
      - ./victoria-metrics/scrape.yml:/etc/victoria-metrics/scrape.yml:ro
    restart: unless-stopped
    # No ports exposed to host — only Grafana (via Caddy) talks to it,
    # over the Docker network.

  grafana:
    image: grafana/grafana:11.4.0
    container_name: obs-grafana
    environment:
      # Auth: everyone past the Caddy basic-auth gate is anonymous Editor.
      # No Grafana login flow — the admin user still exists internally
      # but the login form is disabled and basic-auth API is off, so
      # nothing can authenticate as admin. Provisioning still works
      # (it bypasses HTTP auth).
      GF_AUTH_ANONYMOUS_ENABLED: "true"
      GF_AUTH_ANONYMOUS_ORG_ROLE: Editor
      GF_AUTH_DISABLE_LOGIN_FORM: "true"
      GF_AUTH_BASIC_ENABLED: "false"
      GF_USERS_ALLOW_SIGN_UP: "false"
      GF_USERS_ALLOW_ORG_CREATE: "false"
      GF_INSTALL_PLUGINS: ""
      GF_LOG_LEVEL: warn
    volumes:
      - grafana-data:/var/lib/grafana
      - ./grafana/provisioning:/etc/grafana/provisioning:ro
    depends_on:
      - victoria-metrics
    restart: unless-stopped
    # No ports exposed to host — Caddy fronts it.

  caddy:
    image: caddy:2.8-alpine
    container_name: obs-caddy
    env_file:
      - ./.env
    ports:
      - "80:80"
      - "443:443"
    volumes:
      - ./Caddyfile:/etc/caddy/Caddyfile:ro
      - caddy-data:/data
      - caddy-config:/config
    depends_on:
      - grafana
    restart: unless-stopped

  node-exporter:
    image: prom/node-exporter:v1.8.2
    container_name: obs-node-exporter
    pid: host
    command:
      - "--path.procfs=/host/proc"
      - "--path.sysfs=/host/sys"
      - "--path.rootfs=/rootfs"
      - "--collector.filesystem.mount-points-exclude=^/(sys|proc|dev|host|etc)($$|/)"
    volumes:
      - /proc:/host/proc:ro
      - /sys:/host/sys:ro
      - /:/rootfs:ro
    restart: unless-stopped
    # No ports exposed to host — VictoriaMetrics scrapes it over the Docker
    # network at node-exporter:9100.

volumes:
  vm-data:
  grafana-data:
  caddy-data:
  caddy-config:
```

Why these specific choices:
- `pid: host` + the three bind mounts + the three `--path.*` flags: the gotcha from design.md. Without all of these, node-exporter reports the *container's* `/proc` and `/sys` (mostly empty), not the host's.
- The `mount-points-exclude` regex uses `$$` because docker-compose evaluates `${...}` at compose-parse time — escaping `$` as `$$` makes compose pass a literal `$` into the container.
- `container_name:` is set so logs and `docker ps` are readable when SSHing into the droplet to debug.
- No host port mappings on victoria-metrics, grafana, or node-exporter — Caddy is the only public entry point, all internal traffic stays on the Docker bridge network.
- Image versions are pinned. Bump intentionally as part of a separate maintenance task.

- [ ] **Step 2: Validate compose syntax**

```bash
# We need at least placeholder values for the env vars referenced via env_file.
# Create a temporary obs/.env for validation only.
cat > obs/.env <<'EOF'
OBS_DOMAIN=obs.example.com
OBS_BASIC_AUTH_HASH='$2a$14$placeholderhashplaceholderhashplaceholderhashplaceholder'
EOF

docker compose -f obs/docker-compose.yml --env-file obs/.env config >/dev/null && echo "compose OK"
```

Expected: `compose OK`. The bcrypt hash is single-quoted so docker-compose's env_file parser passes the `$` characters through literally instead of treating them as parameter expansion.

- [ ] **Step 3: Remove the placeholder .env**

The placeholder `obs/.env` was only for validation. Delete it before committing — it must NOT be committed (and it's gitignored, but explicit is better):

```bash
rm obs/.env
```

- [ ] **Step 4: Commit**

```bash
git add obs/docker-compose.yml
git commit -m "obs: docker-compose for observability stack (VM, Grafana, Caddy, node-exporter)"
```

---

## Task 9: Local stack smoke test

Bring the stack up locally with test creds. This catches errors that compose-validate misses (image pull failures, port conflicts, Grafana provisioning errors, Caddyfile semantics). The smoke test does *not* exercise TLS — Let's Encrypt only works with a real domain pointing at the host. We hit Caddy on plain HTTP via `:80`, verify basic-auth and the reverse-proxy work, then tear down.

**Files:** none modified — this is a local validation step.

- [ ] **Step 1: Generate a test bcrypt hash**

```bash
TEST_HASH="$(docker run --rm caddy:2-alpine caddy hash-password --plaintext 'localtest')"
echo "Test hash: $TEST_HASH"
```

Expected: a string starting with `$2a$14$...` (~60 chars). Save it for the next step.

- [ ] **Step 2: Write a test obs/.env**

```bash
cat > obs/.env <<EOF
OBS_DOMAIN=:80
OBS_BASIC_AUTH_HASH='${TEST_HASH}'
EOF
```

`OBS_DOMAIN=:80` tells Caddy to bind to all interfaces on port 80 with auto-https disabled — useful for local testing without TLS. The credential is `mod` / `localtest`. The single quotes around `${TEST_HASH}` are required so docker-compose passes the `$`-containing bcrypt hash through literally instead of treating each `$X` as parameter expansion.

- [ ] **Step 3: Bring the stack up**

```bash
docker compose -f obs/docker-compose.yml up -d
```

Wait ~10 s for everything to settle. Then:

```bash
docker compose -f obs/docker-compose.yml ps
```

Expected: all four services (`obs-victoria-metrics`, `obs-grafana`, `obs-caddy`, `obs-node-exporter`) showing `Up`/`healthy` (Grafana may take a beat).

- [ ] **Step 4: Verify Caddy basic-auth gate**

```bash
# Without credentials → 401
curl -sS -o /dev/null -w "%{http_code}\n" http://localhost/
```

Expected: `401`.

```bash
# With wrong credentials → 401
curl -sS -o /dev/null -w "%{http_code}\n" -u mod:wrong http://localhost/
```

Expected: `401`.

```bash
# With right credentials → 302 redirect to /login (Grafana's response)
curl -sS -o /dev/null -w "%{http_code}\n" -u mod:localtest http://localhost/
```

Expected: `302` or `200` (depends on whether Grafana redirects to /login). Anything in the 2xx/3xx range is success — the basic-auth gate passed.

- [ ] **Step 5: Verify Grafana renders the node-exporter dashboard**

Open `http://localhost/` in a browser. Enter `mod` / `localtest` at the basic-auth prompt. You should land in Grafana's UI as an anonymous Editor. Navigate to `Dashboards` → search "Node Exporter Full" → open it. CPU / memory / disk panels should populate within ~30 s (one scrape cycle).

If a panel says "No data": check that the datasource selector at the top of the dashboard says "Prometheus" (our provisioned datasource). If it's pointing at a different name, the `isDefault: true` fallback didn't kick in — verify Task 4's file landed at the right path.

- [ ] **Step 6: Verify VictoriaMetrics is scraping**

```bash
curl -fsS -u mod:localtest 'http://localhost/api/datasources/proxy/uid/prometheus/api/v1/query?query=count(up==1)'
```

Expected: a JSON response containing `"value":[<timestamp>,"<n>"]` where `<n>` is `2` (one for VM, one for node-exporter). If `<n>` is `0`, scrapes are failing — check `docker compose logs victoria-metrics` for `connection refused` errors.

- [ ] **Step 7: Tear down**

```bash
docker compose -f obs/docker-compose.yml down -v
rm obs/.env
```

`-v` removes the named volumes too — we don't want test data persisted.

- [ ] **Step 8: No commit**

This task validates behaviour, doesn't change code. No commit.

---

## Task 10: GitHub Actions deploy workflow

Mirror the existing `.github/workflows/deploy.yml` shape but target the OBS droplet with the OBS-specific env vars. Manual trigger only (`workflow_dispatch`) — same as the existing workflow.

**Files:**
- Create: `.github/workflows/deploy-obs.yml`

- [ ] **Step 1: Write the workflow**

Write to `.github/workflows/deploy-obs.yml`:

```yaml
name: Deploy OBS to DigitalOcean

on:
  workflow_dispatch:

# Serialize OBS deploys so two pushes in a row don't fight for the droplet.
concurrency:
  group: deploy-obs-digitalocean
  cancel-in-progress: false

jobs:
  deploy:
    name: Deploy obs/ docker-compose stack
    runs-on: ubuntu-latest
    timeout-minutes: 20

    steps:
      - name: Deploy over SSH
        uses: appleboy/ssh-action@v1.0.3
        env:
          OBS_DOMAIN: ${{ vars.OBS_DOMAIN }}
          OBS_BASIC_AUTH_HASH: ${{ secrets.OBS_BASIC_AUTH_HASH }}
        with:
          host: ${{ secrets.OBS_HOST }}
          # Reuses DO_USER/DO_SSH_KEY from the existing game-droplet deploy.
          # Both droplets are provisioned with the same admin SSH user and key.
          username: ${{ secrets.DO_USER }}
          key: ${{ secrets.DO_SSH_KEY }}
          script_stop: true
          command_timeout: 15m
          envs: OBS_DOMAIN,OBS_BASIC_AUTH_HASH
          script: |
            set -euo pipefail

            REPO_SLUG="${{ github.repository }}"
            REF="${{ github.ref_name }}"
            DIR="$HOME/defcon-game"

            echo "==> Target: $REPO_SLUG @ $REF"
            echo "==> Host:   $(hostname)  user: $(whoami)"
            echo "==> Path:   $DIR"
            echo "==> Env:    OBS_DOMAIN=${OBS_DOMAIN:-<unset>} basic_auth=$([ -n "${OBS_BASIC_AUTH_HASH:-}" ] && echo set || echo unset)"

            # Preflight: docker + compose v2.
            docker version >/dev/null 2>&1 || {
              echo "docker is not available to $USER" >&2; exit 1;
            }
            docker compose version >/dev/null 2>&1 || {
              echo "docker compose v2 is required" >&2; exit 1;
            }

            # First deploy: clone. Subsequent: fetch + hard reset.
            if [ ! -d "$DIR/.git" ]; then
              echo "==> Cloning fresh checkout"
              git clone --depth 50 "https://github.com/${REPO_SLUG}.git" "$DIR"
            fi

            cd "$DIR"
            git remote set-url origin "https://github.com/${REPO_SLUG}.git"
            git fetch --prune --tags origin "+refs/heads/*:refs/remotes/origin/*"
            RESOLVED_SHA="$(git rev-parse "origin/${REF}" 2>/dev/null || git rev-parse "${REF}")"
            echo "==> Resolved ${REF} → ${RESOLVED_SHA}"
            git reset --hard "${RESOLVED_SHA}"

            # One-shot swap setup. Idempotent — safe to run every deploy.
            echo "==> swap setup"
            sudo bash obs/setup-swap.sh

            # Write obs/.env from forwarded Variables/Secrets.
            # OBS_BASIC_AUTH_HASH is single-quoted because docker-compose's
            # env_file parser applies parameter expansion to unquoted values
            # — bcrypt hashes contain `$` chars that would otherwise be mangled.
            echo "==> Writing obs/.env"
            umask 077
            cat > obs/.env <<EOF
            OBS_DOMAIN=${OBS_DOMAIN:-}
            OBS_BASIC_AUTH_HASH='${OBS_BASIC_AUTH_HASH:-}'
            EOF
            umask 022

            # Build + launch.
            echo "==> docker compose up -d --build"
            docker compose -f obs/docker-compose.yml up -d --build --remove-orphans

            echo "==> docker image prune"
            docker image prune -f >/dev/null

            echo "==> Status"
            docker compose -f obs/docker-compose.yml ps

            # Smoke checks. Caddy on :80 (plain HTTP) should redirect to https
            # once a real domain is bound; for the smoke check we hit it
            # directly and expect either 301/308 (HTTP→HTTPS redirect from
            # Caddy auto-https) or 401 (basic-auth challenge if auto-https
            # is off / domain isn't ready).
            echo "==> Caddy on :80"
            curl -sS -o /dev/null -w "  http://localhost/ → %{http_code}\n" http://localhost/ || true

            # VictoriaMetrics health: check from Grafana's container, which
            # we already know has network connectivity to victoria-metrics:8428
            # (that's the datasource path mods rely on). VM's own image lacks
            # a usable wget for self-checks.
            echo "==> VictoriaMetrics health"
            docker compose -f obs/docker-compose.yml exec -T grafana \
              wget -qO- 'http://victoria-metrics:8428/health' \
              && echo "  VM OK" \
              || { echo "!! VictoriaMetrics health failed" >&2; exit 1; }

            echo "==> Deploy complete: ${REPO_SLUG}@${RESOLVED_SHA}"
```

Differences from the existing `deploy.yml` worth noticing:
- Targets a different host via `OBS_HOST` (game droplet's `DO_HOST` doesn't apply).
- Reuses `DO_USER` and `DO_SSH_KEY` — the same admin credentials authorize both droplets, so we don't manage parallel `OBS_USER` / `OBS_SSH_KEY` secrets.
- Forwards a different set of env vars (no `AUTH_MODE`, `QUEUE_ENABLED`, etc. — those are game-droplet concerns).
- Runs `obs/setup-swap.sh` via `sudo` once per deploy. The script is idempotent so this is safe.
- Health check hits VictoriaMetrics's `/health` endpoint over the internal network — Caddy/Grafana are gated by basic-auth so a vanilla curl can't easily check them; VM is the simplest end-to-end-stack-is-up signal.

- [ ] **Step 2: Validate workflow YAML**

```bash
python3 -c "import yaml; yaml.safe_load(open('.github/workflows/deploy-obs.yml'))" && echo "YAML OK"
```

Expected: `YAML OK`.

- [ ] **Step 3: Commit**

```bash
git add .github/workflows/deploy-obs.yml
git commit -m "ci: deploy-obs workflow (mirrors deploy.yml for OBS droplet)"
```

---

## Task 11: GitHub Secrets and Variables checklist

The deploy workflow won't run successfully until these are configured in the repo settings. This task is operational, not code — but the plan owns it because the workflow expects them to exist.

**Files:** none in the repo. Configures GitHub Actions settings via the web UI.

- [ ] **Step 1: Add Repository Variables**

In `Settings → Secrets and variables → Actions → Variables` add:

- `OBS_DOMAIN` — `observability.nusgreyhats.org` (the DNS name pointed at the droplet).

- [ ] **Step 2: Add Repository Secrets**

In `Settings → Secrets and variables → Actions → Secrets` add:

- `OBS_HOST` — public IPv4 of the OBS droplet (you'll get this when you provision in Task 12).
- `OBS_BASIC_AUTH_HASH` — the bcrypt hash of the mod password. The Caddyfile hardcodes the username to `nusgreyhats`, so this is the only credential-half you manage. Generate with:
  ```bash
  docker run --rm caddy:2-alpine caddy hash-password --plaintext 'YOUR_PASSWORD_HERE'
  ```
  Paste the output (full `$2a$14$...` string) into the secret value.

The OBS deploy reuses the existing `DO_USER` and `DO_SSH_KEY` secrets — both droplets are provisioned with the same admin SSH credentials, so there's no need for parallel `OBS_USER` / `OBS_SSH_KEY` secrets. Confirm `DO_USER` and `DO_SSH_KEY` are already populated (they should be, from the game-droplet deploy).

- [ ] **Step 3: Sanity-check that the variable list matches the workflow**

```bash
grep -E '\${{ (vars|secrets)\.' .github/workflows/deploy-obs.yml | sort -u
```

Cross-reference each `${{ vars.X }}` and `${{ secrets.Y }}` with what you configured. Anything in the workflow that isn't configured will produce empty strings at deploy time — leading to subtle failures (Grafana with empty admin password locks itself; Caddy with empty domain refuses to start).

- [ ] **Step 4: No commit** — settings are configured in GitHub's UI, not in the repo.

---

## Task 12: Runbook section in the deploy doc

Add a new section to the existing CI/CD doc covering OBS-specific operator concerns: provisioning the droplet, the swap step, and the live-resize escape hatch.

**Files:**
- Modify: `docs/specs/foundations/09-ci-cd-deploy.md`

- [ ] **Step 1: Read the existing file**

```bash
wc -l docs/specs/foundations/09-ci-cd-deploy.md
```

It's about 165 lines and already has sections like `Pipeline — game server`, `Pipeline — lobby`, `Source code publishing`, etc. We add a new top-level section after the existing ones.

- [ ] **Step 2: Append the runbook section**

Open `docs/specs/foundations/09-ci-cd-deploy.md` and insert this new section just before the existing `## Implementation status` section (so it's grouped with the other deploy/runbook content):

```markdown
## OBS observability droplet

Separate $4/mo DigitalOcean Basic droplet (512 MB / 10 GB / 1 vCPU) running the observability stack — see [`../observability/design.md`](../observability/design.md) for the full design.

### Provisioning (one-time)

1. **Create the droplet.** DO control panel → Droplets → Create. Distribution: Ubuntu 24.04 LTS. Plan: Basic, $4/mo (1 vCPU / 512 MB / 10 GB SSD). Region: same as the game droplet to minimise scrape latency. Authentication: SSH key — use the **same key already authorized for the game droplet**, since the OBS deploy reuses `DO_SSH_KEY`.
2. **SSH user posture.** The deploy workflow reuses `DO_USER`, so the same admin user must exist on the OBS droplet. `setup-swap.sh` runs via `sudo` during deploy — that means `DO_USER` must have **passwordless sudo** (or BE root, which is the default on a fresh DO droplet). If you log in as a non-root `DO_USER`, run `echo "$USER ALL=(ALL) NOPASSWD: ALL" | sudo tee /etc/sudoers.d/$USER && sudo chmod 440 /etc/sudoers.d/$USER` once during provisioning.
3. **DNS.** Point an A-record for `observability.nusgreyhats.org` at the droplet's public IPv4. Caddy obtains a Let's Encrypt cert on first start; it needs the DNS to resolve before the deploy workflow runs.
4. **DigitalOcean Cloud Firewall** (new firewall, attached to the OBS droplet only):
    - Inbound `22/tcp` allowlisted to the team's IPs.
    - Inbound `80/tcp` and `443/tcp` from world (Caddy + LE).
    - Everything else denied.
5. **Install Docker + compose v2** on the droplet (one-time):
    ```bash
    curl -fsSL https://get.docker.com | sh
    ```
6. **Configure GitHub Secrets/Variables.** New entries needed: `OBS_HOST` (the OBS droplet's public IP), `OBS_BASIC_AUTH_HASH` (bcrypt output of `caddy hash-password --plaintext '<your-password>'`), and the variable `OBS_DOMAIN`. The deploy reuses the existing `DO_USER` and `DO_SSH_KEY` secrets — no `OBS_USER` / `OBS_SSH_KEY` needed.

### Deploying

Manual trigger: GitHub → Actions → `Deploy OBS to DigitalOcean` → Run workflow → pick branch.

The workflow:
1. SSHes into the OBS droplet using `OBS_HOST` + the reused `DO_USER` / `DO_SSH_KEY` secrets.
2. Clones / fast-forwards the repo to `$HOME/defcon-game`.
3. Runs `obs/setup-swap.sh` via `sudo` (idempotent — re-running is a no-op; needs passwordless sudo).
4. Writes `obs/.env` from forwarded Variables and Secrets, mode 600.
5. `docker compose -f obs/docker-compose.yml up -d --build --remove-orphans`.
6. Smoke-checks Caddy (`:80`) and runs a VictoriaMetrics health check from inside the Grafana container.

### Verifying after deploy

- Open `https://OBS_DOMAIN/` — basic-auth prompt → enter the credential you configured. You should land in Grafana.
- Dashboards → "Node Exporter Full" — CPU/memory/disk should populate within ~30 s of scrape.
- If Grafana shows "No data", `ssh` into the OBS droplet and:
    ```bash
    docker compose -f ~/defcon-game/obs/docker-compose.yml logs victoria-metrics --tail 50
    docker compose -f ~/defcon-game/obs/docker-compose.yml logs node-exporter --tail 50
    ```
    Most common failure: missing bind mount on node-exporter → it sees only the container's `/proc` / `/sys`. Verify `obs/docker-compose.yml` matches the spec in this PR.

### Live-resize escape hatch

If the OBS VM OOMs during the event (Grafana panels fail to load, container repeatedly restarts), DO supports live resize without a rebuild:

1. DO console → the OBS droplet → Resize → "CPU and RAM only" (preserves disk).
2. Pick the next tier up (1 GB RAM / 25 GB SSD, ~$6/mo). Resize is non-destructive on this option.
3. After resize: SSH in, `docker compose -f ~/defcon-game/obs/docker-compose.yml restart` to pick up the new memory limits.

The 7-day retention plus VictoriaMetrics's lower memory profile means this should be a rare emergency button — not a planned step.

### Tearing down (e.g. post-event)

To stop running while preserving data: `docker compose -f obs/docker-compose.yml down`. Volumes persist.

To wipe everything: `docker compose -f obs/docker-compose.yml down -v`. Then destroy the droplet from the DO console.
```

- [ ] **Step 3: Verify the file is still valid Markdown**

```bash
# Basic sanity: the file should still parse as text and have the new heading.
grep -n '^## OBS observability droplet$' docs/specs/foundations/09-ci-cd-deploy.md
```

Expected: a single match.

- [ ] **Step 4: Commit**

```bash
git add docs/specs/foundations/09-ci-cd-deploy.md
git commit -m "docs(deploy): runbook section for OBS observability droplet"
```

---

## Task 13: Push branch + open PR

**Files:** none modified. PR-level integration step.

- [ ] **Step 1: Push the branch**

```bash
git push -u origin "$(git branch --show-current)"
```

- [ ] **Step 2: Open a PR**

```bash
gh pr create --title "obs(phase 1): stand up observability droplet" --body "$(cat <<'EOF'
## Summary

- New `obs/` directory holding an independent Docker Compose stack: VictoriaMetrics, Grafana, Caddy, node-exporter.
- New `.github/workflows/deploy-obs.yml` deploying it to a separate DO droplet.
- Runbook section added to `docs/specs/foundations/09-ci-cd-deploy.md` covering provisioning, deploy, verification, live-resize escape hatch.
- Phase 1 of the design at `docs/specs/observability/design.md`. Phases 2 (game-droplet exporters) and 3 (Go app instrumentation) are separate plans that come after this one ships.

## Test plan

- [ ] Local: `docker compose -f obs/docker-compose.yml --env-file obs/.env config` succeeds.
- [ ] Local: with a placeholder `obs/.env`, `docker compose -f obs/docker-compose.yml up -d` brings all four services up healthy.
- [ ] Local: `curl -u mod:localtest http://localhost/` → 302 (Grafana login redirect via Caddy + basic-auth).
- [ ] Local: `curl -u mod:localtest 'http://localhost/api/datasources/proxy/uid/prometheus/api/v1/query?query=count(up==1)'` → returns `2` (VM + node-exporter).
- [ ] Production: GitHub Secrets/Variables configured per `.github/workflows/deploy-obs.yml`.
- [ ] Production: workflow run completes successfully on a real OBS droplet.
- [ ] Production: `https://<OBS_DOMAIN>/` serves Grafana with valid TLS, basic-auth gates correctly, "Node Exporter Full" dashboard populates.
EOF
)"
```

The `gh` CLI prints the PR URL on success — share that with the team for review.

- [ ] **Step 3: After PR merges, run the deploy workflow**

GitHub → Actions → Deploy OBS to DigitalOcean → Run workflow → `main` (or the branch the PR merged to).

Verify by visiting `https://<OBS_DOMAIN>/`.

---

## Done

Phase 1 is complete when:

1. PR is merged.
2. `Deploy OBS to DigitalOcean` workflow has run green at least once on a real droplet.
3. Visiting `https://<OBS_DOMAIN>/` from a browser:
    - Prompts for basic-auth.
    - With correct credentials, lands in Grafana (anonymous Editor).
    - The "Node Exporter Full" dashboard shows non-zero CPU / memory / disk metrics for the OBS droplet itself.

After this lands, open a follow-up issue tracking Phase 2 ("Game droplet exporters") which extends the same `obs/victoria-metrics/scrape.yml` to scrape the existing game droplet via its Caddy `/_metrics/*` paths.
