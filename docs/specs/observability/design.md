# Admin Panel Observability — Design

> Tracks GitHub issue [#75 "Admin panel observability"](https://github.com/NUSGreyhats/DEFCON-Game/issues/75).

**Status:** Phases 1, 2, 3 shipped. Phase 4 (alerting) deferred.
**Date:** 2026-04-27
**Target event:** DEF CON SG (3-day window)

## Goal

Give moderators a live ops dashboard during the 3-day DEF CON SG event covering: host CPU / memory / disk, per-container resource usage, and app-level signals (active players, queue depth, request rate, error rate, latency, runs completed). Built incrementally so each phase ships working software.

Out of scope for this spec: alerting (Phase 4, separate future issue) and any public/wall-display surface (the original issue's "B" extension).

## Architecture

Three droplets. Observability runs on a **separate VM** so a saturated game host doesn't take the dashboard down at the moment mods need it most. The two game droplets (dev + prod) are scraped by the same OBS instance and distinguished by an `env` label injected at scrape time.

```
┌────────────────────────────┐
│  GAME DROPLET — DEV        │ ─┐
│  (DO_HOST)                 │  │           ┌─────────────────────────────┐
│  client (caddy) :80        │  │           │   OBS DROPLET ($4/mo)       │
│  /_metrics/{lobby,game,    │  │           │                             │
│   node} → loopback         │  │           │  caddy :80,:443 ─► grafana  │
└────────────────────────────┘  │           │   (basic-auth + LE TLS)     │
                                │ HTTP      │                             │
┌────────────────────────────┐  ├──────────►│  victoria-metrics :8428     │
│  GAME DROPLET — PROD       │  │ +         │   scrape every 15s, every   │
│  (DO_HOST_PROD)            │  │ src-IP    │   series labelled with      │
│  client (caddy) :80        │  │ gate      │   env={obs|dev|prod}        │
│  /_metrics/{lobby,game,    │  │           │                             │
│   node} → loopback         │ ─┘           │  node-exp :9100 (env=obs)   │
└────────────────────────────┘              └─────────────────────────────┘
                                                    │
                                                    ▼
                                             https://observability.<domain>/

Source-IP gate for /_metrics/* lives in Caddy on each game droplet
(both dev and prod forward OBS_HOST). DO Cloud Firewall on OBS
allowlists :22 to team IPs and exposes :80/:443 to the world.
```

**Tooling choices** (decisions log at the bottom of this doc):

- **VictoriaMetrics single-node** for metrics storage. Prometheus-compatible (same scrape config, same query API, same exposition format). Chosen specifically for ~2-3× lower memory footprint than Prometheus at our scale, which matters on a 512 MB VM. Drop-in: Grafana speaks to it via the existing "Prometheus" datasource type.
- **Grafana, self-hosted on the OBS VM** (not Grafana Cloud — keeps everything inside the existing Docker Compose deploy pattern, no external account).
- **`prometheus/client_golang`** for app instrumentation (industry standard, idiomatic Go; emits the same exposition format VictoriaMetrics scrapes).
- **node-exporter** for host metrics on each VM. (cAdvisor for per-container metrics is in the decisions log — currently skipped due to a Docker incompatibility.)
- **Caddy** in front of Grafana on the OBS VM (TLS via Let's Encrypt, basic-auth gate). Same pattern as the existing client front-door.

## Sizing — DigitalOcean $4/mo Basic droplet

512 MB RAM / 10 GB SSD / 1 vCPU. Workable, not luxurious. Mitigations baked in.

### Series count estimate

| Source | Active series |
|---|---|
| node-exporter on game droplet (default + minor trim) | ~400 |
| node-exporter on OBS VM (self-monitoring) | ~400 |
| App metrics: `lobby` + `game-server` | ~200 |
| VictoriaMetrics self-scrape | ~100 |
| Grafana self-metrics | ~50 |
| Headroom (label drift, future metrics) | ~200 |
| **Total** | **~1,500** |

### Storage at 7-day retention

VictoriaMetrics compression averages ~1.0-1.5 bytes per sample on real workloads (better than Prometheus's ~2 bytes thanks to its mergeset format).

```
1,500 series × 5,760 samples/day × 1.2 bytes  =  ~10 MB/day
7 days                                         =   ~70 MB VM data
+ index / overhead                             =   ~30 MB
+ Grafana SQLite (dashboards, sessions)        =   ~50 MB
+ Docker images                                =  ~1.0 GB
+ OS, logs, swap                               =  ~2.5 GB
─────────────────────────────────────────────────────────
Total used                                     =  ~3.7 GB
Free on 10 GB disk                             =  ~6.3 GB
```

Disk is comfortable. Retention can grow to 30 days if we want without meaningful disk pressure.

### Memory — the real constraint (and why we picked VictoriaMetrics)

VictoriaMetrics's per-series overhead is markedly lower than Prometheus's, mainly because it doesn't keep a 2-hour in-memory head block per series and uses a more compact in-memory index.

```
victoria-metrics RSS (idle, 1,500 series)      ~50-100 MB
victoria-metrics during expensive query        spikes to ~150-200 MB
Grafana baseline                               ~100-150 MB
Caddy (TLS + basic-auth + reverse-proxy)       ~30-50 MB
node-exporter (on OBS itself)                  ~10 MB
System (kernel, sshd, Docker daemon)           ~80 MB
─────────────────────────────────────────────────────────
Typical working set                            ~280-400 MB
Worst case during dashboard load               ~490 MB  (under 512 MB ✓)
```

For comparison: Prometheus at the same scale would be ~400-500 MB typical and ~600 MB worst case (over budget). VictoriaMetrics keeps us inside the 512 MB envelope without relying on swap.

### 512 MB mitigations — all baked into Phase 1

- **1 GB swapfile** on the OBS VM (defensive, not load-bearing — VictoriaMetrics fits under the budget at this scale; swap is just belt-and-suspenders for unexpected cardinality growth).
- VictoriaMetrics flags: `-retentionPeriod=7d`, `-storageDataPath=/storage`, `-search.maxSeries=10000` (caps a single query's series scan), `-memory.allowedPercent=70` (caps VM's adaptive memory ceiling so it leaves room for Grafana on this small box).
- **Single Grafana org, no plugins**, no embedded image renderer.
- **Live-resize escape hatch:** if the OBS VM OOMs during the event, DO supports live resize to 1 GB ($2 extra for the month) without a rebuild — documented in the runbook. Less likely with VictoriaMetrics than it would have been with Prometheus, but still worth knowing.

## Networking and security

### Scrape access from OBS → game droplet

VictoriaMetrics on OBS must reach three `/metrics` endpoints on the game droplet: node-exporter, `lobby`, `game-server`.

**Approach: route all four through the existing `client` Caddy on `:80`, gated by source-IP match.** This stays consistent with the existing pattern (everything on the game droplet enters via Caddy; backends bind to loopback or the Docker network only).

Path convention — underscore-prefixed to mark internal-only:

| Caddy path             | Backend                          |
|------------------------|----------------------------------|
| `/_metrics/lobby`      | `lobby:3001/metrics`             |
| `/_metrics/game`       | `game-server:8080/metrics`       |
| `/_metrics/node`       | `node-exporter:9100/metrics`     |

The Caddy `@metrics_node` matcher includes a `remote_ip` directive limited to the OBS droplet IP. Mismatched IPs fall through Caddy's matcher chain to the static `file_server`, which serves the SPA index — no metrics leak. node-exporter binds to the Docker network only and is *not* reachable from outside Caddy.

**Source-IP preservation under Docker is deployment-dependent.** Docker's default bridge networking with `userland-proxy=true` rewrites incoming source IPs to the bridge gateway, which would break this gate (Caddy would never see the OBS droplet's real IP). Two contingencies:

- If the game droplet runs Docker with `userland-proxy=false` (set in `/etc/docker/daemon.json`) or its kernel/iptables stack preserves source IP via DNAT, the gate works as designed.
- If not, a Phase 2.5 follow-up swaps `remote_ip` for HTTP basic-auth on the metrics paths (a shared `OBS_SCRAPE_TOKEN` secret in both `client/Caddyfile`'s `basic_auth` directive and OBS's scrape config). Same threat coverage; works regardless of Docker's NAT behavior.

We ship the `remote_ip` version first because it's simpler and most modern Linux Docker setups preserve source IPs. We'll know which is the case from the first scrape after deploy: if the OBS-side dashboards populate, source IPs are preserved.

**DO Cloud Firewall — game droplet:** *no change*. Caddy's `remote_ip` directive does the gating in-app; attaching a new Cloud Firewall now would risk breaking the existing public traffic if a rule is forgotten (Cloud Firewall replaces the default-allow policy when attached). The metrics endpoints are already off the public address space — they only exist as Caddy paths reachable from the OBS IP.

**DO Cloud Firewall — OBS droplet:** new firewall, three rules. Inbound `80`, `443` open to the world (Grafana behind Caddy + basic-auth + Let's Encrypt); inbound `22` allowlisted to the team's IPs; everything else denied.

The metrics endpoints expose telemetry, not credentials. A leak via misconfigured matcher is low-severity but the source-IP gate is cheap insurance.

**Scrape interval:** `15s` (matches the Prometheus ecosystem default, which VictoriaMetrics uses too). Sufficient for a live ops view (mods perceive "real-time"); no reason to go faster and no reason to go slower.

### Grafana auth

Caddy in front of Grafana on the OBS VM:

- Let's Encrypt TLS on `observability.nusgreyhats.org`.
- HTTP basic-auth gate (single shared mod credential, hashed via `caddy hash-password`, stored in env). Single shared credential is a deliberate trade-off — mods are a small, trusted set, and per-mod accounts add operational overhead with no real security gain at this scale. Documented in the decisions log below.
- Grafana itself in `auth.anonymous = true`, single-org **`Editor`** role — anyone past the Caddy gate can view *and* edit dashboards without a second login.
- Grafana's admin login is **disabled** (`GF_AUTH_DISABLE_LOGIN_FORM=true`) and basic-auth API is off (`GF_AUTH_BASIC_ENABLED=false`). The internal admin user still exists in Grafana's SQLite (Grafana always has one) but it's dormant — there's no way to authenticate as it through any HTTP surface. Provisioning still works because it bypasses HTTP auth entirely.
- `allowUiUpdates: true` on the dashboard provisioner — UI edits persist in Grafana's SQLite. The JSON files in `obs/grafana/provisioning/dashboards/` are seed values; once Grafana saves an edit, the in-DB version is the source of truth. Wiping the `grafana-data` volume (e.g. `docker compose down -v`) loses UI edits.
- A "Metrics" link added to the existing `/admin/` panel nav (Phase 3) points at `https://observability.nusgreyhats.org/`.

The Telegram-OTP `/admin/` gate is *not* shared with Grafana — they're separate auth surfaces. Acceptable because the only credential mods share for the OBS path is the Caddy basic-auth password.

### Secrets and variables

New repo `Variables` (non-secret, version-controlled-elsewhere) and `Secrets` in GitHub Actions:

- `OBS_HOST` (secret) — public IPv4 of the OBS VM. Used both as the SSH/deploy target *and* (in Phases 2 and 3) as the source-IP matcher in the game droplet's Caddy `/_metrics/*` rules. Stored as a Secret to match the existing `DO_HOST` precedent.
- `OBS_DOMAIN` (variable) — `observability.nusgreyhats.org`. Used by Caddy on OBS for the LE TLS cert.
- `OBS_BASIC_AUTH_HASH` (secret) — bcrypt hash from `caddy hash-password`. The username is hardcoded to `nusgreyhats` in the Caddyfile and isn't managed as a secret. **The only credential mods type day-to-day.**

- `OBS_SSH_KEY` (secret) — private SSH key whose public counterpart is in the OBS droplet's `~/.ssh/authorized_keys`. Verified empirically: `DO_SSH_KEY` does not authenticate against OBS, so OBS gets its own deploy key. Game droplet untouched.

**Phase 2 additions** (existing secrets reused, no new ones to configure):

- `OBS_HOST` (already present) — game droplet's `deploy.yml` now also forwards this so `client/Caddyfile`'s `remote_ip {$OBS_HOST}` directive can substitute it.
- `DO_HOST` and `DO_HOST_PROD` (already present) — OBS droplet's `deploy-obs.yml` reads them as `GAME_HOST_DEV` and `GAME_HOST_PROD`, aliased to `game-droplet-dev` and `game-droplet-prod` in the VM container's `/etc/hosts` so `scrape.yml` can reference them by hostname. Each scrape job has two static targets, one per env, with an `env={dev|prod}` label.

The OBS deploy hardcodes `root` as the SSH user (it doesn't reuse `DO_USER`, which on this repo's existing deploy points to a non-root account that doesn't exist on the OBS droplet). Both DO droplets ship with `root` available by default; OBS uses it directly. `setup-swap.sh` runs as root without needing `sudo`. Also forwards `secrets.GITHUB_TOKEN` (auto-issued, run-scoped) so the droplet can `git clone` a private repo.

## Metric inventory

### Phase 2 — Infra (free, automatic via exporters)

From **node-exporter** on each VM:
- CPU per-mode (idle, user, system, iowait)
- Memory available / used / cached / swap
- Disk: filesystem size / free / used per mount, IOPS via `node_disk_*`
- Network: bytes in/out per interface, conntrack count, dropped packets
- Load average (1, 5, 15)
- File descriptor count

### Phase 3 — App-level (instrumented in Go)

**Latency:**
- `http_request_duration_seconds` histogram, labels `{service, method, route, status}` — lobby HTTP API
- `game_tick_duration_seconds` histogram — game-server's 20Hz loop
- `ws_message_handler_duration_seconds` histogram, label `{packet_type}` — game-server WS

**Traffic:**
- `http_requests_total` counter, labels `{service, method, route, status}` — lobby
- `ws_messages_total` counter, labels `{direction, packet_type}` — game-server
- `ws_connections_active` gauge — game-server

**Saturation:**
- `go_goroutines` gauge per service (emitted by `client_golang` automatically — no code we write)
- `process_resident_memory_bytes`, `process_open_fds` (also free from `client_golang`)
- `slot_semaphore_in_use` gauge — game-server, equals `MAX_CONCURRENT - free`
- `queue_depth` gauge — lobby
- `queue_dwell_time_seconds` histogram — lobby, time spent in queue before admission

**Errors:**
- `errors_total` counter, labels `{service, kind}` where `kind ∈ {ws_auth_fail, ws_parse_fail, db_write, panic, telegram, ...}`

**Game-domain (the "live event vibe" stuff):**
- `active_players` gauge — game-server
- `runs_completed_total` counter — game-server (rate gives runs/min)
- `run_duration_seconds` histogram — completion times distribution
- `auth_mode_info` gauge — Prometheus info-metric convention: constant value `1` with label `{mode="open"|"identity"|"token"}`. Set once at startup; changes require restart, which matches reality.
- `concurrent_cap` gauge — game-server's current `MAX_CONCURRENT`. (The lobby's admin-tunable queue admission cap is separate; if mods change it from the panel we'll add a `lobby_admission_cap` gauge in the lobby — easy follow-up if useful.)
- `replay_viewers_active` gauge — game-server
- `spectator_connections_active` gauge — game-server
- `room_split_duration_seconds` histogram — per-room time spent, label `{room=<index>}`. Postmortem-friendly; cardinality is bounded (rooms 0..FinalRoom).
- `weapon_shots_total` counter — accepted shots by weapon name. Shotgun pellets count as one shot, not three.

**Explicitly excluded as YAGNI for this issue:**
- Per-bug exploit detection (interesting, but it's an *analysis pipeline*, not a metric — needs replay-log statistical comparison, not a counter. Scope separately.)
- Distributed tracing (no value at this scale).

## Phasing

Each phase ships working, useful software on its own. One spec, three plans (this doc + three to be written by `writing-plans`).

### Phase 1 — Stand up the OBS VM (foundation)

**Demo:** open `https://obs.<domain>/`, basic-auth, see the OBS VM's own CPU / memory / disk in a Grafana node-exporter dashboard.

**Deliverables:**
- New `obs/` directory at repo root containing:
  - `docker-compose.yml` — VictoriaMetrics, Grafana, Caddy, node-exporter
  - `victoria-metrics/scrape.yml` — Prometheus-format scrape config (initially: VM self-scrape + local node-exporter)
  - `grafana/provisioning/datasources/prometheus.yml` — Grafana "Prometheus" datasource pointing at `http://victoria-metrics:8428` (VM serves the Prometheus query API at that path)
  - `grafana/provisioning/dashboards/node-exporter.json` — host metrics for OBS itself
  - `Caddyfile` — TLS, basic-auth, reverse-proxy to Grafana
  - `setup-swap.sh` — one-shot 1 GB swapfile creation, idempotent
- **node-exporter container config gotcha:** node-exporter inside a container only sees its own `/proc` and `/sys` by default — useless for host monitoring. The compose service must:
  - bind-mount `/proc:/host/proc:ro`, `/sys:/host/sys:ro`, `/:/rootfs:ro`
  - pass flags `--path.procfs=/host/proc --path.sysfs=/host/sys --path.rootfs=/rootfs`
  - run with `pid: host` so it can see all host processes
  - Without these the dashboard shows the container's own (mostly empty) view, not the host's.
- New `.github/workflows/deploy-obs.yml` — same shape as the existing droplet deploy: rsync the `obs/` tree, write `.env` from secrets, `docker compose up -d --build`.
- New `obs/.env.example` documenting the OBS-specific variables used by the deploy workflow (separate from the repo-root `.env.example` which is for the game stack).
- Runbook section in `docs/specs/foundations/09-ci-cd-deploy.md` covering: provisioning the droplet, the swap setup, the live-resize escape hatch.

**Validates:** TLS chain, basic-auth, provisioning, deploy pipeline, swap, retention flags. If any of these are wrong, find out before instrumenting any code.

### Phase 2 — Game droplet exporters (infra visibility)

**Demo:** mods open Grafana mid-event, see real-time CPU / mem / disk / network for the game droplet host and per-container resource usage.

**Deliverables:**
- Add `node-exporter` service to the existing root `docker-compose.yml`. **No public ports** — it binds to the Docker network only and is reachable exclusively through the existing `client` Caddy.
- **node-exporter container config:** bind-mount `/proc:/host/proc:ro`, `/sys:/host/sys:ro`, `/:/rootfs:ro`; pass `--path.procfs=/host/proc --path.sysfs=/host/sys --path.rootfs=/rootfs`; run with `pid: host`. (Without these, node-exporter reports the container's own `/proc` / `/sys` — mostly empty.)
- Extend `client/Caddyfile` with one matcher-and-handle block:
  ```
  @metrics_node {
      path /_metrics/node
      remote_ip {$OBS_HOST}
  }
  handle @metrics_node {
      rewrite * /metrics
      reverse_proxy node-exporter:9100
  }
  ```
  Non-matching source IPs fall through to the static `file_server` (returns the SPA index — no metrics leak).
- No DO Cloud Firewall change on the game droplet — the in-Caddy source-IP gate is sufficient.
- Update `obs/victoria-metrics/scrape.yml` to add a `node-exporter-game` job (`metrics_path: /_metrics/node`, target `game-droplet:80`). The `game-droplet` host is aliased to the game droplet's IP via `extra_hosts` on the VM container, sourced from the existing `DO_HOST` secret as `GAME_HOST`.
- The standard "Node Exporter Full" dashboard provisioned in Phase 1 already shows both `host=obs` and `host=game` — no new dashboard JSON needed.

**Validates:** the in-Caddy source-IP gate, two-VM scraping over the existing client Caddy.

### Phase 3 — App instrumentation (the live-event vibe stuff)

**Demo:** mods open Grafana, see "12 active players, queue depth 3, P99 tick latency 4 ms, 0.2 errors/min, 14 runs completed in last hour".

**Deliverables:**
- Add `github.com/prometheus/client_golang/prometheus` and `.../promhttp` to `lobby/go.mod` and `server/go.mod`.
- Wire the metric inventory above into both services. Recommended pattern: a small `metrics` internal package per service that owns the registrations and exposes typed accessor functions, so call sites do `metrics.WSMessages.WithLabelValues("c2s","auth").Inc()` rather than touching the registry directly.
- Expose `/metrics` endpoint on each service via `promhttp.Handler()`. Both services already bind to loopback only — no port-binding changes.
- Extend `client/Caddyfile` with two more matcher-and-handle blocks following the same shape as Phase 2 (named matcher with `path` + `remote_ip`, then a `handle` block that rewrites to `/metrics` and reverse-proxies). Routes: `@metrics_lobby` → `lobby:3001/metrics`; `@metrics_game` → `game-server:8080/metrics`. Both gated by `remote_ip {$OBS_HOST}`.
- Update OBS scrape config (`obs/victoria-metrics/scrape.yml`) to add the two app endpoints to the game-droplet scrape job.
- Add provisioned Grafana dashboards: "Golden signals" (latency / traffic / saturation / errors) and "Game-domain" (active players, queue depth, runs, etc.).
- Add a "Metrics" link in the `/admin/` panel nav pointing at `https://obs.<domain>/`. The link is rendered in the existing nav template — single line of HTML.

**Validates:** Go instrumentation idioms, that the metric set actually answers the questions mods ask, that `/admin/` integration is acceptable.

### Phase 4 (deferred to future issue) — Alerting + Telegram

Add `vmalert` (VictoriaMetrics's drop-in replacement for Prometheus's alerting layer — same rule syntax) and `alertmanager` to the OBS docker-compose, point Alertmanager at the existing Telegram bot via webhook (reuse `TELEGRAM_BOT_TOKEN` and `TELEGRAM_ADMIN_CHAT_ID`), define rules in `obs/victoria-metrics/rules.yml`. Touches none of Phases 1–3.

Tracked in this doc only as a forward-looking note; **not** part of the current implementation.

## Testing approach

### Phase 1
- CI: `docker compose -f obs/docker-compose.yml config` to validate compose syntax. No full boot in CI — too slow and fragile for the value.
- Manual after deploy: hit `https://obs.<domain>/`, basic-auth in, see the OBS-VM node-exporter dashboard render with non-zero CPU/mem/disk values. As a sanity check, from inside the OBS Docker network: `curl -s 'http://victoria-metrics:8428/api/v1/query?query=count(up==1)'` returns at least 1 (i.e., at least one scrape target is healthy). The exact job names will be defined in `scrape.yml` during implementation; this assertion is intentionally generic to avoid coupling the test to those names.

### Phase 2
- CI: compose-validate the updated root `docker-compose.yml` plus a `caddy validate` of the updated Caddyfile.
- Manual after deploy:
  - From the OBS VM (`ssh` in): `curl -sf http://<game-droplet>/_metrics/node | head` returns Prometheus exposition format.
  - From a laptop or any non-OBS IP: same `curl` returns HTTP 403.
  - Open Grafana, scroll the "Game host" dashboard during a normal play session; verify CPU/mem update and per-container series exist for `game-server`, `lobby`, `client`.

### Phase 3
- Unit tests around the `metrics` package on each service: registration is idempotent, label cardinality is bounded, accessor functions emit the expected series names. Use `prometheus/testutil` for `CollectAndCompare`.
- Integration: spin up `lobby` + `game-server` locally, hit `/metrics` on each, assert the documented metric names exist.
- Manual: play a run end-to-end with the Phase 3 OBS scrape live; verify `active_players`, `runs_completed_total`, `queue_depth` move correctly on the dashboard.

## Decisions log

- **VictoriaMetrics over Prometheus:** the choice was reopened during review when the team asked whether VM would help with the 512 MB memory budget. It does — VM idles at ~50-100 MB RSS vs Prometheus's ~150-200 MB at this cardinality, and peaks at ~150-200 MB during queries vs Prometheus's ~300 MB. That ~100 MB delta is meaningful when the budget is 512 MB. The compatibility surface (PromQL queries, scrape config syntax, exposition format from `prometheus/client_golang`, Grafana datasource type) is unchanged — VictoriaMetrics is a drop-in. The earlier "Prom has more StackOverflow hits" argument was overweight: the actual debugging surface (scrape config, PromQL, Grafana panels) is identical between the two; only storage internals differ, and we're unlikely to debug those at this scale. Storage is also slightly better with VM (~1.0-1.5 bytes/sample vs ~2 for Prom) but disk is not the binding constraint here.
- **Local Grafana over Grafana Cloud:** the team already runs everything in Docker Compose with deploy via GitHub Actions writing `.env` on the droplet. Adding Cloud means another account, API keys, and dependence on free-tier policy not changing before the event. Provisioning dashboards as YAML/JSON in the repo also makes them reviewable code, not personal-account state.
- **Native `/admin/` dashboard rejected** in favour of Grafana: the team explicitly chose the metrics-stack path for the broader feature set (PromQL, ad-hoc queries, future alerting). Grafana lives behind its own basic-auth, with a "Metrics" link in `/admin/` to keep the user journey tight.
- **Separate VM over same-droplet observability:** the original issue commenter's saturation argument applies — if the game droplet falls over, mods need the dashboard *more*, not less. $4/mo extra is a small price for that.
- **$4/mo over $6/mo:** the team chose cheapest tier; 512 MB is workable with the swap + retention + cardinality mitigations above and a documented live-resize escape hatch. Absolute cost difference is small (~$2/mo); the principle ("do we actually need 1 GB?") is sound at this scale.
- **Single shared basic-auth credential for Grafana:** mods are a small, trusted set; Grafana data is read-only; per-mod accounts add operational overhead with no security gain. Documented as a known trade-off rather than a defect — if the credential ever leaks, rotate `OBS_BASIC_AUTH_HASH` and redeploy.
- **Metrics paths exposed via existing client Caddy with source-IP gate, not as separate public ports:** stays consistent with the existing pattern (single public entry point per droplet, all reverse-proxied through Caddy). Avoids new firewall holes and avoids duplicating TLS termination on the game droplet.
- **7-day retention over 30:** team explicitly does not need a long postmortem window. 7 days covers the 3-day event plus a few days of buffer for any post-event surprises.
- **cAdvisor dropped from Phase 2:** every released cAdvisor version (v0.49 through v0.55) fails to read container layer metadata when Docker uses the `overlayfs` storage driver via containerd-snapshotter — the default on the game droplet's Docker 28+ install. cAdvisor expects `/var/lib/docker/image/overlay2/layerdb/...` but the actual data lives at `/var/lib/containerd/io.containerd.snapshotter.v1.*`. See [cadvisor#3643](https://github.com/google/cadvisor/issues/3643), [cadvisor#3860](https://github.com/google/cadvisor/issues/3860), fix in [PR #3709](https://github.com/google/cadvisor/pull/3709) (unreleased). Alternatives considered: switch the game droplet to `overlay2` storage driver (disruptive — requires recreating all container images) or keep iterating cAdvisor flags (we tried; nothing works without upstream changes). Decision: skip cAdvisor; Phase 3's app metrics + node-exporter's host metrics cover the use cases that matter for the event. Revisit when upstream releases a fix.
- **No alerts in initial scope:** alerting is its own design (thresholds, runbooks, escalation). Phase 4 captured for later but explicitly deferred.

## Open questions

- **Grafana embedded panel inside `/admin/`:** Phase 3 ships a "Metrics" link out. Whether to upgrade to an embedded iframe panel later is open; depends on whether mods actually want it (ask after Phase 3 ships).
- **Public/wall-display extension (the original "B" option):** explicit out-of-scope here. If the team wants it after the event, the metrics will already exist; only the presentation surface needs new work.
