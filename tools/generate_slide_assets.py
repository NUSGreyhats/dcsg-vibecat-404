#!/usr/bin/env python3
"""Generate slide-ready statistics and graphics for DEFCON Dungeon.

Outputs PNG charts plus a Markdown summary into ./slide_assets/.
The data is pulled from the current codebase, Tiled room JSON, and the
local SQLite databases if they contain leaderboard data.
"""

from __future__ import annotations

import io
import json
import math
import os
import re
import sqlite3
import subprocess
from collections import Counter, defaultdict
from dataclasses import dataclass
from pathlib import Path
from typing import Any

import matplotlib

matplotlib.use("Agg")
import matplotlib.pyplot as plt
import numpy as np
from PIL import Image, ImageDraw, ImageEnhance, ImageFont

ROOT = Path(__file__).resolve().parents[1]
OUT = ROOT / "slide_assets"

BG = "#0b1020"
PANEL = "#121a31"
TEXT = "#eef2ff"
MUTED = "#aab4d4"
ACCENT = "#66e3ff"
YELLOW = "#ffd166"
PINK = "#ef476f"
GREEN = "#06d6a0"
ORANGE = "#f97316"
PURPLE = "#8b5cf6"
BLUE = "#3b82f6"
RED = "#ef4444"

ARCHETYPE_COLORS = {
    "Grunt": "#8bd3dd",
    "Runner": "#f6c177",
    "Tank": "#c4a7e7",
    "Kamikaze": "#eb6f92",
    "Boss": "#f43f5e",
}
PICKUP_COLORS = {"Health": "#22c55e", "Ammo": "#38bdf8", "Coin": "#facc15"}
LOCKED_DOOR_GIDS = {146, 150, 154}
GID_MASK = 0x1FFFFFFF
FLIP_H = 0x80000000
FLIP_V = 0x40000000
FLIP_D = 0x20000000

FONT_REG = "/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf"
FONT_BOLD = "/usr/share/fonts/truetype/dejavu/DejaVuSans-Bold.ttf"
FONT_MONO = "/usr/share/fonts/truetype/dejavu/DejaVuSansMono.ttf"


def font(size: int, bold: bool = False, mono: bool = False) -> ImageFont.FreeTypeFont:
    path = FONT_MONO if mono else (FONT_BOLD if bold else FONT_REG)
    return ImageFont.truetype(path, size)


def read_text(path: Path) -> str:
    return path.read_text(encoding="utf-8")


@dataclass
class Weapon:
    id: int
    name: str
    damage: int
    cooldown_ms: int
    ammo_max: int
    ammo_start: int
    life_sec: float

    @property
    def dps(self) -> float:
        return self.damage * 1000.0 / self.cooldown_ms


@dataclass
class EnemySpec:
    id: int
    name: str
    hp: int
    speed: float
    aggro: float
    cooldown_ms: int
    melee: int


@dataclass
class Bug:
    bug_id: str
    name: str
    category: str
    difficulty: str
    savings: str
    status: str


@dataclass
class RoomStats:
    index: int
    width: int
    height: int
    tile_w: int
    tile_h: int
    initial: Counter
    waves: dict[int, Counter]
    pickups: Counter
    solid_cells: int
    water_cells: int
    doors: int
    player_spawn: tuple[float, float] | None
    objects: list[dict[str, Any]]

    @property
    def initial_total(self) -> int:
        return sum(self.initial.values())

    @property
    def wave_total(self) -> int:
        return sum(sum(c.values()) for c in self.waves.values())

    @property
    def total_enemies(self) -> int:
        return self.initial_total + self.wave_total

    @property
    def pickup_total(self) -> int:
        return sum(self.pickups.values())


@dataclass
class StatsBundle:
    rooms: list[RoomStats]
    weapons: list[Weapon]
    enemies: list[EnemySpec]
    bugs: list[Bug]
    shop: list[tuple[int, str, int]]
    constants: dict[str, Any]
    code_stats: dict[str, tuple[int, int]]
    db_stats: dict[str, Any]


def parse_weapons() -> list[Weapon]:
    text = read_text(ROOT / "server/internal/game/weapons.go")
    pattern = re.compile(
        r"(\d+):\s*\{Damage:\s*(\d+),\s*CooldownMs:\s*(\d+),\s*Name:\s*\"([^\"]+)\",\s*"
        r"AmmoMax:\s*(\d+),\s*AmmoStart:\s*(\d+),\s*LifeSec:\s*([0-9.]+)\}"
    )
    weapons: list[Weapon] = []
    for m in pattern.finditer(text):
        weapons.append(
            Weapon(
                id=int(m.group(1)),
                damage=int(m.group(2)),
                cooldown_ms=int(m.group(3)),
                name=m.group(4),
                ammo_max=int(m.group(5)),
                ammo_start=int(m.group(6)),
                life_sec=float(m.group(7)),
            )
        )
    return sorted(weapons, key=lambda w: w.id)


def parse_enemies() -> list[EnemySpec]:
    text = read_text(ROOT / "server/internal/game/mobai.go")
    pattern = re.compile(
        r"(\d+):\s*\{Name:\s*\"([^\"]+)\",\s*HP:\s*(\d+),\s*Speed:\s*([0-9.]+),\s*"
        r"AggroRange:\s*([0-9.]+),\s*AttackCooldownMs:\s*(\d+),\s*MeleeDamage:\s*(\d+)\}"
    )
    out: list[EnemySpec] = []
    for m in pattern.finditer(text):
        out.append(
            EnemySpec(
                id=int(m.group(1)),
                name=m.group(2),
                hp=int(m.group(3)),
                speed=float(m.group(4)),
                aggro=float(m.group(5)),
                cooldown_ms=int(m.group(6)),
                melee=int(m.group(7)),
            )
        )
    return sorted(out, key=lambda e: e.id)


def parse_constants() -> dict[str, Any]:
    constants_go = read_text(ROOT / "server/internal/game/constants.go")
    walls_go = read_text(ROOT / "server/internal/game/walls.go")

    def find_number(pattern: str, default: Any) -> Any:
        m = re.search(pattern, constants_go)
        if not m:
            m = re.search(pattern, walls_go)
        if not m:
            return default
        raw = m.group(1)
        return float(raw) if "." in raw else int(raw)

    return {
        "TickHz": find_number(r"TickHz\s*=\s*([0-9]+)", 20),
        "RoomWidth": find_number(r"RoomWidth\s*=\s*([0-9]+)", 1280),
        "RoomHeight": find_number(r"RoomHeight\s*=\s*([0-9]+)", 768),
        "FinalRoom": find_number(r"FinalRoom\s+uint8\s*=\s*([0-9]+)", 10),
        "BossRoom": find_number(r"BossRoom\s+uint8\s*=\s*([0-9]+)", 10),
        "ProjectileSpeed": find_number(r"ProjectileSpeed\s+float32\s*=\s*([0-9.]+)", 600),
        "PlayerSpeed": find_number(r"PlayerSpeed\s+float32\s*=\s*([0-9.]+)", 220),
        "MaxPlayerHP": find_number(r"MaxPlayerHP\s+int16\s*=\s*([0-9]+)", 100),
        "InteractRange": find_number(r"InteractRange\s*=\s*([0-9]+)", 2500),
        "GridCols": find_number(r"GridCols\s+int\s*=\s*([0-9]+)", 40),
        "GridRows": find_number(r"GridRows\s+int\s*=\s*([0-9]+)", 24),
        "TileSize": find_number(r"TileSize\s+float32\s*=\s*([0-9.]+)", 32),
        "WaveTriggers": [0.66, 0.33],
    }


def parse_shop() -> list[tuple[int, str, int]]:
    text = read_text(ROOT / "server/internal/game/shop.go")
    pattern = re.compile(r"\{ID:\s*ShopItem\w+,\s*Name:\s*\"([^\"]+)\",\s*Cost:\s*(\d+)\}")
    out = []
    for i, m in enumerate(pattern.finditer(text), start=1):
        out.append((i, m.group(1), int(m.group(2))))
    return out


def parse_bugs() -> list[Bug]:
    path = ROOT / "docs/specs/bugs.md"
    bugs: list[Bug] = []
    if not path.exists():
        return bugs
    for line in read_text(path).splitlines():
        if not line.startswith("| BUG-"):
            continue
        cells = [c.strip() for c in line.strip().strip("|").split("|")]
        if len(cells) < 6:
            continue
        bug_id, name, category, difficulty, savings, status = cells[:6]
        # Strip markdown links / HTML-ish text from the name when possible.
        name = re.sub(r"\[([^\]]+)\]\([^\)]+\)", r"\1", name)
        bugs.append(Bug(bug_id, name, category, difficulty, savings, status))
    return bugs


def marker_enemy_type(o: dict[str, Any]) -> str | None:
    for key in ("class", "type", "name"):
        value = o.get(key, "")
        if value in {"Grunt", "Runner", "Tank", "Kamikaze", "Boss"}:
            return value
    return None


def wave_slot(o: dict[str, Any]) -> int | None:
    for p in o.get("properties", []) or []:
        if p.get("name") == "wave":
            try:
                n = int(p.get("value"))
                return n if n >= 1 else None
            except (TypeError, ValueError):
                return None
    return None


def pickup_type(o: dict[str, Any]) -> str | None:
    for key in ("type", "name", "class"):
        value = o.get(key, "")
        if value in {"Health", "Ammo"}:
            return value
    return None


def water_gids(tilemap: dict[str, Any]) -> set[int]:
    out: set[int] = set()
    for ts in tilemap.get("tilesets", []):
        first = int(ts.get("firstgid", 1))
        for tile in ts.get("tiles", []) or []:
            for prop in tile.get("properties", []) or []:
                if prop.get("name") == "water" and bool(prop.get("value")):
                    out.add(int(tile["id"]) + first)
    return out


def parse_rooms() -> list[RoomStats]:
    paths = sorted(
        (ROOT / "server/internal/game/tilemaps").glob("room-*.json"),
        key=lambda p: int(re.search(r"room-(\d+)", p.name).group(1)),
    )
    rooms: list[RoomStats] = []
    for path in paths:
        index = int(re.search(r"room-(\d+)", path.name).group(1))
        data = json.loads(path.read_text(encoding="utf-8"))
        initial: Counter = Counter()
        waves: dict[int, Counter] = defaultdict(Counter)
        pickups: Counter = Counter()
        solid = 0
        water = 0
        doors = 0
        player_spawn: tuple[float, float] | None = None
        objects: list[dict[str, Any]] = []
        wgids = water_gids(data)

        for layer in data.get("layers", []):
            if layer.get("type") == "tilelayer" and layer.get("name") == "Walls":
                for raw in layer.get("data", []):
                    gid = int(raw) & GID_MASK
                    if gid == 0:
                        continue
                    if gid in wgids:
                        water += 1
                    else:
                        solid += 1
                    if gid in LOCKED_DOOR_GIDS:
                        doors += 1
            if layer.get("type") != "objectgroup":
                continue
            for obj in layer.get("objects", []) or []:
                objects.append(obj)
                name = obj.get("name", "")
                typ = obj.get("type", "")
                cls = obj.get("class", "")
                if name == "PlayerSpawn" or typ == "PlayerSpawn" or cls == "PlayerSpawn":
                    player_spawn = (float(obj.get("x", 0)), float(obj.get("y", 0)))
                etype = marker_enemy_type(obj)
                if etype:
                    slot = wave_slot(obj)
                    if slot is None:
                        initial[etype] += 1
                    else:
                        waves[slot][etype] += 1
                ptype = pickup_type(obj)
                if ptype:
                    pickups[ptype] += 1

        rooms.append(
            RoomStats(
                index=index,
                width=int(data["width"]),
                height=int(data["height"]),
                tile_w=int(data.get("tilewidth", 32)),
                tile_h=int(data.get("tileheight", 32)),
                initial=initial,
                waves=dict(waves),
                pickups=pickups,
                solid_cells=solid,
                water_cells=water,
                doors=doors,
                player_spawn=player_spawn,
                objects=objects,
            )
        )
    return rooms


def code_line_stats() -> dict[str, tuple[int, int]]:
    ext_lang = {
        ".go": "Go",
        ".ts": "TypeScript",
        ".tsx": "TypeScript",
        ".js": "JavaScript",
        ".mjs": "JavaScript",
        ".html": "HTML",
        ".css": "CSS",
        ".md": "Markdown",
        ".json": "JSON",
        ".yml": "YAML",
        ".yaml": "YAML",
        ".py": "Python",
    }
    try:
        files = subprocess.check_output(["git", "ls-files"], cwd=ROOT, text=True).splitlines()
    except Exception:
        files = [str(p.relative_to(ROOT)) for p in ROOT.rglob("*") if p.is_file()]
    counts: Counter = Counter()
    file_counts: Counter = Counter()
    skip_names = {"package-lock.json", "yarn.lock", "go.sum", "go.work.sum"}
    for rel in files:
        path = ROOT / rel
        if not path.exists() or path.name in skip_names or "slide_assets" in path.parts:
            continue
        lang = ext_lang.get(path.suffix)
        if not lang:
            continue
        try:
            text = path.read_text(encoding="utf-8")
        except Exception:
            continue
        lines = text.count("\n") + (0 if text.endswith("\n") or not text else 1)
        counts[lang] += lines
        file_counts[lang] += 1
    return {lang: (file_counts[lang], counts[lang]) for lang in sorted(counts)}


def db_snapshot() -> dict[str, Any]:
    out: dict[str, Any] = {}
    game_db = ROOT / "server/game.db"
    ident_db = ROOT / "server/identity.db"
    if game_db.exists():
        with sqlite3.connect(game_db) as con:
            row = con.execute(
                "select count(*), count(distinct player_id), min(total_ms), avg(total_ms), max(total_ms) from completed_runs"
            ).fetchone()
            out.update(
                {
                    "completed_runs": row[0] or 0,
                    "players_with_runs": row[1] or 0,
                    "best_ms": row[2],
                    "avg_ms": row[3],
                    "slowest_ms": row[4],
                }
            )
            flagged = con.execute("select count(*) from flagged_runs").fetchone()[0]
            out["flagged_runs"] = flagged or 0
    if ident_db.exists():
        with sqlite3.connect(ident_db) as con:
            row = con.execute("select count(*), min(registered_at), max(registered_at) from players").fetchone()
            out.update(
                {
                    "registered_players": row[0] or 0,
                    "first_registration": row[1],
                    "latest_registration": row[2],
                }
            )
    return out


def collect_stats() -> StatsBundle:
    return StatsBundle(
        rooms=parse_rooms(),
        weapons=parse_weapons(),
        enemies=parse_enemies(),
        bugs=parse_bugs(),
        shop=parse_shop(),
        constants=parse_constants(),
        code_stats=code_line_stats(),
        db_stats=db_snapshot(),
    )


def setup_mpl():
    plt.rcParams.update(
        {
            "figure.facecolor": BG,
            "axes.facecolor": BG,
            "savefig.facecolor": BG,
            "axes.edgecolor": "#2a355c",
            "axes.labelcolor": TEXT,
            "xtick.color": MUTED,
            "ytick.color": MUTED,
            "text.color": TEXT,
            "font.family": "DejaVu Sans",
            "axes.titleweight": "bold",
            "axes.titlesize": 20,
            "axes.labelsize": 12,
            "legend.facecolor": PANEL,
            "legend.edgecolor": "#2a355c",
        }
    )


def savefig(path: Path):
    """Save a Matplotlib figure as an exact 16:9 slide PNG.

    The figure is first rendered with a tight bounding box so legends/labels are
    not clipped, then fitted onto a 1920×1080 dark canvas. That makes the files
    easy to drop directly into PowerPoint/Keynote/Google Slides.
    """
    buf = io.BytesIO()
    plt.savefig(buf, format="png", dpi=120, bbox_inches="tight", pad_inches=0.25)
    plt.close()
    buf.seek(0)
    chart = Image.open(buf).convert("RGB")
    canvas = Image.new("RGB", (1920, 1080), (11, 16, 32))
    margin = 34
    scale = min((1920 - 2 * margin) / chart.width, (1080 - 2 * margin) / chart.height)
    new_size = (int(chart.width * scale), int(chart.height * scale))
    chart = chart.resize(new_size, Image.Resampling.LANCZOS)
    x = (1920 - new_size[0]) // 2
    y = (1080 - new_size[1]) // 2
    canvas.paste(chart, (x, y))
    canvas.save(path)


def make_room_difficulty_chart(stats: StatsBundle) -> Path:
    rooms = stats.rooms
    x = np.arange(len(rooms))
    labels = [str(r.index) for r in rooms]
    enemy_order = ["Grunt", "Runner", "Tank", "Kamikaze", "Boss"]
    fig, ax = plt.subplots(figsize=(16, 9))
    bottom = np.zeros(len(rooms))
    for enemy in enemy_order:
        vals = np.array([r.initial[enemy] + sum(w.get(enemy, 0) for w in r.waves.values()) for r in rooms])
        ax.bar(x, vals, bottom=bottom, label=enemy, color=ARCHETYPE_COLORS[enemy], width=0.72)
        bottom += vals

    initial = np.array([r.initial_total for r in rooms])
    ax.plot(x, initial, color=TEXT, marker="o", lw=2, ls="--", label="initial spawn count")
    for xi, total, init in zip(x, bottom, initial):
        ax.text(xi, total + 0.8, f"{int(total)}", ha="center", va="bottom", color=TEXT, fontsize=12, fontweight="bold")
        if total > init:
            ax.text(xi, init + 0.25, f"+{int(total-init)} wave", ha="center", va="bottom", color=YELLOW, fontsize=9)

    ax.set_title("Room pressure curve: the dungeon starts polite, then gets mean")
    ax.set_xlabel("Room")
    ax.set_ylabel("Enemies if every wave fires")
    ax.set_xticks(x, labels)
    ax.set_ylim(0, max(bottom) + 6)
    ax.grid(axis="y", color="#253052", alpha=0.65)
    ax.legend(ncol=6, loc="upper left", bbox_to_anchor=(0, 1.08))
    ax.text(
        0.995,
        0.02,
        "Data: server/internal/game/tilemaps/room-*.json • wave triggers at 66% and 33% living enemies",
        transform=ax.transAxes,
        ha="right",
        va="bottom",
        fontsize=10,
        color=MUTED,
    )
    path = OUT / "defcon_room_pressure_curve.png"
    savefig(path)
    return path


def make_weapon_chart(stats: StatsBundle) -> Path:
    weapons = stats.weapons
    projectile_speed = float(stats.constants.get("ProjectileSpeed", 600))
    names = [w.name.title() for w in weapons]
    dps = [w.dps for w in weapons]
    ranges = [projectile_speed * w.life_sec for w in weapons]
    colors = ["#66e3ff", "#06d6a0", "#f97316"]

    fig, (ax1, ax2) = plt.subplots(1, 2, figsize=(16, 9), gridspec_kw={"width_ratios": [1.05, 1]})
    y = np.arange(len(weapons))
    ax1.barh(y, dps, color=colors, height=0.62)
    ax1.set_yticks(y, names)
    ax1.invert_yaxis()
    ax1.set_xlabel("Theoretical server DPS")
    ax1.set_title("Weapon personality")
    ax1.grid(axis="x", color="#253052", alpha=0.65)
    for yi, w in zip(y, weapons):
        ammo = "∞" if w.ammo_max >= 9999 else str(w.ammo_max)
        ax1.text(
            w.dps + 0.6,
            yi,
            f"{w.dps:.1f} DPS  •  {w.damage} dmg / {w.cooldown_ms} ms  •  ammo {ammo}",
            va="center",
            color=TEXT,
            fontsize=12,
        )
    ax1.set_xlim(0, max(dps) * 1.45)

    sizes = [360 if w.ammo_max >= 9999 else 140 + w.ammo_max * 8 for w in weapons]
    ax2.scatter(ranges, dps, s=sizes, c=colors, alpha=0.95, edgecolors=TEXT, linewidths=1.5)
    for w, rx, dy in zip(weapons, ranges, dps):
        ax2.annotate(
            w.name.title(),
            (rx, dy),
            xytext=(10, 10),
            textcoords="offset points",
            fontsize=12,
            fontweight="bold",
            color=TEXT,
        )
    ax2.set_xlabel("Projectile lifetime range (px)")
    ax2.set_ylabel("DPS")
    ax2.set_title("Range vs damage output")
    ax2.grid(color="#253052", alpha=0.65)
    ax2.set_xlim(0, max(ranges) * 1.18)
    ax2.set_ylim(0, max(dps) * 1.25)
    ax2.text(
        0.02,
        0.04,
        f"Server projectile speed: {projectile_speed:.0f}px/s. Shotgun is intentionally close-range.",
        transform=ax2.transAxes,
        color=MUTED,
        fontsize=10,
    )
    fig.suptitle("Combat sandbox: one reliable pistol, one DPS monster, one point-blank hammer", fontsize=24, fontweight="bold")
    path = OUT / "defcon_weapon_dps_range.png"
    savefig(path)
    return path


def make_enemy_chart(stats: StatsBundle) -> Path:
    enemies = stats.enemies
    names = [e.name.title() for e in enemies]
    metrics = ["HP", "Speed", "Melee"]
    raw = np.array([[e.hp, e.speed, e.melee] for e in enemies], dtype=float)
    maxes = raw.max(axis=0)
    norm = raw / maxes
    x = np.arange(len(enemies))
    width = 0.24
    colors = ["#66e3ff", "#06d6a0", "#ef476f"]

    fig, ax = plt.subplots(figsize=(16, 9))
    for i, metric in enumerate(metrics):
        ax.bar(x + (i - 1) * width, norm[:, i], width=width, label=metric, color=colors[i])
        for xi, e, value in zip(x + (i - 1) * width, enemies, raw[:, i]):
            label = f"{int(value)}" if metric != "Speed" else f"{value:.0f}"
            ax.text(xi, norm[list(enemies).index(e), i] + 0.025, label, ha="center", va="bottom", fontsize=9, color=TEXT)

    ax.set_xticks(x, names)
    ax.set_ylabel("Normalized to strongest archetype")
    ax.set_ylim(0, 1.22)
    ax.grid(axis="y", color="#253052", alpha=0.65)
    ax.legend(ncol=3, loc="upper left", bbox_to_anchor=(0, 1.08))
    ax.set_title("Enemy archetypes: tiny kamikazes, chunky tanks, and a boss outlier")
    boss = next((e for e in enemies if e.name == "boss"), None)
    if boss:
        late_hp = int(boss.hp * 1.6)
        late_speed = boss.speed * 1.2
        ax.text(
            0.995,
            0.04,
            f"Late rooms scale enemies: HP ×1.6 and speed ×1.2. Boss room boss ≈ {late_hp} HP / {late_speed:.0f}px/s.",
            transform=ax.transAxes,
            ha="right",
            va="bottom",
            fontsize=11,
            color=YELLOW,
        )
    path = OUT / "defcon_enemy_archetypes.png"
    savefig(path)
    return path


def make_bug_ladder(stats: StatsBundle) -> Path:
    active = [b for b in stats.bugs if b.status.lower().startswith("active")]
    diff_counts = Counter(b.difficulty for b in active)
    difficulties = ["Easy", "Medium", "Hard"]
    counts = [diff_counts.get(d, 0) for d in difficulties]

    # A slide-friendly estimate based on docs/specs/bugs.md's stacking model,
    # with BUG-08 omitted because it is disabled in the current shipping code.
    labels = [
        "Honest run",
        "BUG-03\nDoor skip",
        "BUG-05\nInteract range",
        "BUG-04\nSwap cancel",
        "BUG-07\nDamage burst",
        "BUG-01\nSeed routing",
        "BUG-06\nSettle race",
        "BUG-11\nKill log race",
        "BUG-02\nDrift stretch",
    ]
    times = np.array([150, 135, 130, 122, 110, 95, 85, 80, 85], dtype=float)
    # Last point is shown as the official current reachable band, not a strict arithmetic chain.

    fig = plt.figure(figsize=(16, 9))
    gs = fig.add_gridspec(1, 2, width_ratios=[1.45, 0.8], wspace=0.22)
    ax = fig.add_subplot(gs[0, 0])
    ax.plot(range(len(times) - 1), times[:-1], color=ACCENT, lw=3, marker="o", markersize=9)
    ax.fill_between([len(times) - 2.15, len(times) - 1.0], 80, 90, color=GREEN, alpha=0.22)
    ax.scatter([len(times) - 1], [85], s=180, color=GREEN, edgecolor=TEXT, zorder=5)
    for i, (lab, t) in enumerate(zip(labels, times)):
        ax.text(i, t + 4.5, lab, ha="center", va="bottom", fontsize=9, color=TEXT, fontweight="bold")
        time_label = f"~{int(t//60)}:{int(t%60):02d}" if i != len(times) - 1 else "~1:20–1:30"
        ax.text(i, t - 6.2, time_label, ha="center", va="top", fontsize=10, color=YELLOW)
    ax.set_xlim(-0.5, len(times) - 0.5)
    ax.set_ylim(60, 162)
    ax.set_xticks([])
    ax.set_ylabel("Estimated run time")
    ax.set_title("The real game: stack tiny protocol bugs into a speedrun route")
    ax.grid(axis="y", color="#253052", alpha=0.65)
    ax.text(
        0.01,
        0.02,
        "Current spec: BUG-08 disabled; BUG-09/10 retired. Estimates are slide/talking-point ranges.",
        transform=ax.transAxes,
        color=MUTED,
        fontsize=10,
    )

    ax2 = fig.add_subplot(gs[0, 1])
    bars = ax2.bar(difficulties, counts, color=["#06d6a0", "#f6c177", "#ef476f"], width=0.62)
    for bar, count in zip(bars, counts):
        ax2.text(bar.get_x() + bar.get_width() / 2, count + 0.08, str(count), ha="center", color=TEXT, fontsize=20, fontweight="bold")
    ax2.set_ylim(0, max(counts) + 1.5)
    ax2.set_ylabel("Active bugs")
    ax2.set_title("Discovery difficulty")
    ax2.grid(axis="y", color="#253052", alpha=0.65)

    pill_y = max(counts) + 0.8
    ax2.text(
        0.5,
        -0.18,
        f"{len(active)} active exploit mechanics\n+ {sum('disabled' in b.status.lower() for b in stats.bugs)} disabled preserved spec",
        transform=ax2.transAxes,
        ha="center",
        va="top",
        color=MUTED,
        fontsize=12,
    )
    fig.suptitle("DEFCON Dungeon is a speedrun and a security puzzle", fontsize=24, fontweight="bold")
    path = OUT / "defcon_exploit_stack_ladder.png"
    savefig(path)
    return path


def get_tileset_image(tilemap: dict[str, Any], room_json_path: Path) -> tuple[Image.Image, dict[str, Any]]:
    ts = tilemap["tilesets"][0]
    image_rel = ts.get("image", "../tilemap.png")
    image_path = (room_json_path.parent / image_rel).resolve()
    if not image_path.exists():
        # Server tilemaps do not carry the image; client tilemaps do.
        image_path = ROOT / "client/public/assets/tilemap.png"
    return Image.open(image_path).convert("RGBA"), ts


def transform_tile(tile: Image.Image, raw_gid: int) -> Image.Image:
    # Tiled flip flags. This is enough for thumbnail rendering; gameplay collision
    # uses the authoritative server parser.
    if raw_gid & FLIP_D:
        tile = tile.transpose(Image.Transpose.TRANSPOSE)
    if raw_gid & FLIP_H:
        tile = tile.transpose(Image.Transpose.FLIP_LEFT_RIGHT)
    if raw_gid & FLIP_V:
        tile = tile.transpose(Image.Transpose.FLIP_TOP_BOTTOM)
    return tile


def render_room_image(room_index: int) -> Image.Image:
    path = ROOT / f"client/public/assets/rooms/room-{room_index}.json"
    data = json.loads(path.read_text(encoding="utf-8"))
    width = int(data["width"])
    height = int(data["height"])
    tile_w = int(data.get("tilewidth", 32))
    tile_h = int(data.get("tileheight", 32))
    img = Image.new("RGBA", (width * tile_w, height * tile_h), (10, 14, 28, 255))
    tileset, ts = get_tileset_image(data, path)
    firstgid = int(ts.get("firstgid", 1))
    columns = int(ts.get("columns", max(1, tileset.width // tile_w)))

    for layer in data.get("layers", []):
        if layer.get("type") != "tilelayer":
            continue
        layer_img = Image.new("RGBA", img.size, (0, 0, 0, 0))
        opacity = float(layer.get("opacity", 1))
        for idx, raw in enumerate(layer.get("data", [])):
            raw = int(raw)
            gid = raw & GID_MASK
            if gid == 0:
                continue
            local = gid - firstgid
            if local < 0:
                continue
            sx = (local % columns) * tile_w
            sy = (local // columns) * tile_h
            tile = tileset.crop((sx, sy, sx + tile_w, sy + tile_h))
            tile = transform_tile(tile, raw)
            if opacity < 1:
                alpha = tile.getchannel("A").point(lambda a: int(a * opacity))
                tile.putalpha(alpha)
            x = (idx % width) * tile_w
            y = (idx // width) * tile_h
            layer_img.alpha_composite(tile, (x, y))
        img.alpha_composite(layer_img)

    draw = ImageDraw.Draw(img, "RGBA")
    f_small = font(18, bold=True)
    f_tiny = font(14, bold=True)

    # Door markers from locked door tiles.
    for layer in data.get("layers", []):
        if layer.get("type") == "tilelayer" and layer.get("name") == "Walls":
            for idx, raw in enumerate(layer.get("data", [])):
                gid = int(raw) & GID_MASK
                if gid in LOCKED_DOOR_GIDS:
                    x = (idx % width) * tile_w
                    y = (idx // width) * tile_h
                    draw.rounded_rectangle((x + 3, y + 3, x + tile_w - 3, y + tile_h - 3), radius=5, outline=(255, 209, 102, 255), width=4)

    for layer in data.get("layers", []):
        if layer.get("type") != "objectgroup":
            continue
        for obj in layer.get("objects", []) or []:
            x = float(obj.get("x", 0))
            y = float(obj.get("y", 0))
            name = obj.get("name", "")
            typ = obj.get("type", "")
            cls = obj.get("class", "")
            etype = marker_enemy_type(obj)
            slot = wave_slot(obj)
            ptype = pickup_type(obj)
            if name == "PlayerSpawn" or typ == "PlayerSpawn" or cls == "PlayerSpawn":
                r = 12
                draw.ellipse((x - r, y - r, x + r, y + r), fill=(102, 227, 255, 190), outline=(238, 242, 255, 255), width=3)
                draw.text((x + 11, y - 23), "P", fill=(238, 242, 255, 255), font=f_tiny)
            elif etype:
                r = 12 if etype != "Boss" else 20
                color_hex = ARCHETYPE_COLORS[etype]
                rgb = tuple(int(color_hex[i : i + 2], 16) for i in (1, 3, 5))
                if slot:
                    draw.ellipse((x - r, y - r, x + r, y + r), outline=rgb + (255,), width=4)
                    draw.ellipse((x - 4, y - 4, x + 4, y + 4), fill=rgb + (240,))
                else:
                    draw.ellipse((x - r, y - r, x + r, y + r), fill=rgb + (190,), outline=(255, 255, 255, 220), width=2)
                letter = {"Grunt": "G", "Runner": "R", "Tank": "T", "Kamikaze": "K", "Boss": "B"}[etype]
                draw.text((x - 5, y - 9), letter, fill=(15, 18, 32, 255), font=f_tiny)
            elif ptype:
                rgb = tuple(int(PICKUP_COLORS[ptype][i : i + 2], 16) for i in (1, 3, 5))
                r = 10
                poly = [(x, y - r), (x + r, y), (x, y + r), (x - r, y)]
                draw.polygon(poly, fill=rgb + (215,), outline=(255, 255, 255, 230))
                draw.text((x + 9, y - 18), "H" if ptype == "Health" else "A", fill=(238, 242, 255, 255), font=f_tiny)

    # Label strip.
    draw.rounded_rectangle((14, 12, 300, 54), radius=10, fill=(11, 16, 32, 210))
    draw.text((28, 21), f"Room {room_index}", font=f_small, fill=(238, 242, 255, 255))
    return img


def make_room_montage(stats: StatsBundle) -> Path:
    W, H = 1920, 1080
    canvas = Image.new("RGB", (W, H), (11, 16, 32))
    draw = ImageDraw.Draw(canvas, "RGBA")
    # Soft background grid.
    for y in range(0, H, 32):
        draw.line((0, y, W, y), fill=(35, 48, 86, 45), width=1)
    for x in range(0, W, 32):
        draw.line((x, 0, x, H), fill=(35, 48, 86, 35), width=1)

    draw.text((52, 34), "Room atlas: 10 handcrafted arenas", font=font(54, bold=True), fill=(238, 242, 255, 255))
    draw.text((55, 96), "Markers: player spawn, pickups, doors, initial mobs, and hollow wave spawns", font=font(24), fill=(170, 180, 212, 255))

    margin_x = 46
    gap_x = 18
    thumb_w = int((W - 2 * margin_x - 4 * gap_x) / 5)
    thumb_h = int(thumb_w * 768 / 1280)
    start_y = 158
    gap_y = 58
    for idx, room in enumerate(stats.rooms):
        row = idx // 5
        col = idx % 5
        x = margin_x + col * (thumb_w + gap_x)
        y = start_y + row * (thumb_h + gap_y)
        rendered = render_room_image(room.index).resize((thumb_w, thumb_h), Image.Resampling.LANCZOS).convert("RGB")
        # Card shadow + border.
        draw.rounded_rectangle((x - 5, y - 5, x + thumb_w + 5, y + thumb_h + 5), radius=18, fill=(0, 0, 0, 110))
        canvas.paste(rendered, (x, y))
        draw.rounded_rectangle((x, y, x + thumb_w, y + thumb_h), radius=10, outline=(102, 227, 255, 150), width=2)
        label = f"R{room.index}: {room.total_enemies} mobs"
        if room.wave_total:
            label += f" ({room.wave_total} wave)"
        draw.rounded_rectangle((x + 10, y + thumb_h + 8, x + thumb_w - 10, y + thumb_h + 42), radius=10, fill=(18, 26, 49, 230))
        draw.text((x + 20, y + thumb_h + 14), label, font=font(18, bold=True), fill=(238, 242, 255, 255))

    # Legend.
    legend_y = 915
    items = [
        ("Player", (102, 227, 255)),
        ("Health", (34, 197, 94)),
        ("Ammo", (56, 189, 248)),
        ("Door", (255, 209, 102)),
        ("Initial mob", (235, 111, 146)),
        ("Wave mob", (246, 193, 119)),
        ("Boss", (244, 63, 94)),
    ]
    x = 58
    for label, rgb in items:
        draw.ellipse((x, legend_y, x + 22, legend_y + 22), fill=rgb + (220,), outline=(238, 242, 255, 220), width=2)
        draw.text((x + 32, legend_y - 2), label, font=font(21), fill=(238, 242, 255, 255))
        x += 190 if label != "Initial mob" else 220
    draw.text(
        (58, 982),
        "Tip for the slide: use this as the visual overview, then punch in with the room-pressure chart.",
        font=font(22),
        fill=(170, 180, 212, 255),
    )
    path = OUT / "defcon_room_atlas_montage.png"
    canvas.save(path)
    return path


def round_rect(draw: ImageDraw.ImageDraw, box: tuple[int, int, int, int], radius: int, fill: tuple[int, int, int, int], outline=None, width: int = 1):
    draw.rounded_rectangle(box, radius=radius, fill=fill, outline=outline, width=width)


def make_stats_cards(stats: StatsBundle) -> Path:
    W, H = 1920, 1080
    # Use title art as a subtle background if present.
    bg_path = ROOT / "client/public/assets/title.png"
    if bg_path.exists():
        bg = Image.open(bg_path).convert("RGB").resize((W, H), Image.Resampling.LANCZOS)
        bg = ImageEnhance.Brightness(bg).enhance(0.24)
    else:
        bg = Image.new("RGB", (W, H), (11, 16, 32))
    overlay = Image.new("RGBA", (W, H), (5, 8, 18, 95))
    img = Image.alpha_composite(bg.convert("RGBA"), overlay)
    draw = ImageDraw.Draw(img, "RGBA")

    rooms = stats.rooms
    total_initial = sum(r.initial_total for r in rooms)
    total_waves = sum(r.wave_total for r in rooms)
    total_enemies = total_initial + total_waves
    pickups = sum(r.pickup_total for r in rooms)
    doors = sum(r.doors for r in rooms)
    active_bugs = sum(1 for b in stats.bugs if b.status.lower().startswith("active"))
    disabled_bugs = sum(1 for b in stats.bugs if "disabled" in b.status.lower())
    tile_total = sum(r.width * r.height for r in rooms)
    boss_room = next((r for r in rooms if r.index == stats.constants.get("BossRoom")), rooms[-1])
    max_dps = max((w.dps for w in stats.weapons), default=0)

    draw.text((70, 50), "DEFCON Dungeon", font=font(70, bold=True), fill=(238, 242, 255, 255))
    draw.text((74, 128), "by the numbers", font=font(40, bold=True), fill=(102, 227, 255, 255))
    draw.text((74, 180), "Slide-ready stats pulled from code, Tiled maps, specs, and local DBs", font=font(23), fill=(170, 180, 212, 255))

    cards = [
        ("10", "authored rooms", f"{doors} locked doors + boss finale"),
        (str(total_enemies), "possible enemy kills", f"{total_initial} initial / {total_waves} wave spawns"),
        (str(boss_room.total_enemies), "boss-room mobs", "includes final 10-kamikaze wave"),
        (str(active_bugs), "active exploit bugs", f"+{disabled_bugs} disabled preserved spec"),
        (str(len(stats.weapons)), "weapons", f"rifle peaks at {max_dps:.1f} server DPS"),
        (str(pickups), "static pickups", "plus every kill drops a coin"),
        (f"{stats.constants.get('TickHz')} Hz", "server tick loop", "50 ms packet rhythm"),
        (f"{tile_total:,}", "map tiles", "10 rooms × 40 × 24"),
    ]
    card_w, card_h = 405, 202
    start_x, start_y = 74, 280
    gap_x, gap_y = 50, 44
    for i, (big, label, sub) in enumerate(cards):
        row, col = divmod(i, 4)
        x = start_x + col * (card_w + gap_x)
        y = start_y + row * (card_h + gap_y)
        round_rect(draw, (x + 8, y + 10, x + card_w + 8, y + card_h + 10), 26, (0, 0, 0, 90))
        round_rect(draw, (x, y, x + card_w, y + card_h), 26, (18, 26, 49, 220), outline=(102, 227, 255, 90), width=2)
        draw.text((x + 28, y + 24), big, font=font(58, bold=True), fill=(255, 209, 102, 255))
        draw.text((x + 30, y + 96), label.upper(), font=font(22, bold=True), fill=(238, 242, 255, 255))
        draw.text((x + 30, y + 132), sub, font=font(20), fill=(170, 180, 212, 255))

    db_runs = stats.db_stats.get("completed_runs", 0)
    db_players = stats.db_stats.get("registered_players", 0)
    footer = f"Local leaderboard snapshot: {db_runs} completed runs, {db_players} registered players. Re-run tools/generate_slide_assets.py after playtests for live-performance charts."
    round_rect(draw, (70, 922, W - 70, 990), 20, (11, 16, 32, 210), outline=(42, 53, 92, 180), width=1)
    draw.text((96, 944), footer, font=font(22), fill=(170, 180, 212, 255))
    path = OUT / "defcon_stats_cards.png"
    img.convert("RGB").save(path)
    return path


def make_summary(stats: StatsBundle, image_paths: list[Path]) -> Path:
    rooms = stats.rooms
    total_initial = sum(r.initial_total for r in rooms)
    total_waves = sum(r.wave_total for r in rooms)
    total_enemies = total_initial + total_waves
    total_pickups = sum(r.pickup_total for r in rooms)
    total_doors = sum(r.doors for r in rooms)
    total_solid = sum(r.solid_cells for r in rooms)
    total_water = sum(r.water_cells for r in rooms)
    active_bugs = [b for b in stats.bugs if b.status.lower().startswith("active")]

    lines: list[str] = []
    lines.append("# DEFCON Dungeon — slide stats\n")
    lines.append("Generated by `python3 tools/generate_slide_assets.py`.\n")
    lines.append("## Copy/paste talking points\n")
    lines.append(f"- **10 authored rooms** at **40×24 tiles** each (**{sum(r.width*r.height for r in rooms):,} map tiles** total).")
    lines.append(f"- **{total_enemies} possible enemy kills** if every reinforcement wave fires: **{total_initial} initial** enemies + **{total_waves} wave** enemies.")
    lines.append(f"- **Room 10 is the spike:** {rooms[-1].total_enemies} mobs, including the boss and a final 10-kamikaze wave.")
    lines.append(f"- **{len(stats.enemies)} enemy archetypes:** grunt, runner, tank, kamikaze, boss; late rooms scale HP ×1.6 and speed ×1.2.")
    lines.append(f"- **{len(stats.weapons)} weapons:** pistol, rifle, shotgun. The rifle has the highest theoretical server DPS at **{max(w.dps for w in stats.weapons):.1f}**.")
    lines.append(f"- **{total_pickups} authored health/ammo pickups**, plus **every kill drops a coin** for shop routing.")
    lines.append(f"- **{len(active_bugs)} active intentional exploit mechanics** turn it from a dungeon crawler into a protocol speedrun puzzle.")
    lines.append(f"- **Server rhythm:** {stats.constants.get('TickHz')} Hz ticks, 50 ms per tick, {stats.constants.get('MaxPlayerHP')} HP player, {stats.constants.get('PlayerSpeed')} px/s base speed.")
    if stats.db_stats.get("completed_runs", 0) == 0:
        lines.append("- **Leaderboard DB is currently empty** in this checkout, so the charts are build/design stats rather than player-performance stats.")
    lines.append("\n## Generated graphics\n")
    for path in image_paths:
        lines.append(f"- `{path.relative_to(ROOT)}`")

    lines.append("\n## Room breakdown\n")
    lines.append("| Room | Initial | Waves | Total mobs | Pickups | Doors | Solid cells | Water cells |")
    lines.append("| ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |")
    for r in rooms:
        lines.append(f"| {r.index} | {r.initial_total} | {r.wave_total} | {r.total_enemies} | {r.pickup_total} | {r.doors} | {r.solid_cells} | {r.water_cells} |")
    lines.append(f"| **Total** | **{total_initial}** | **{total_waves}** | **{total_enemies}** | **{total_pickups}** | **{total_doors}** | **{total_solid}** | **{total_water}** |")

    lines.append("\n## Enemy totals by archetype\n")
    enemy_totals = Counter()
    for r in rooms:
        enemy_totals.update(r.initial)
        for wave in r.waves.values():
            enemy_totals.update(wave)
    lines.append("| Archetype | Count across full route | Base HP | Base speed | Melee damage |")
    lines.append("| --- | ---: | ---: | ---: | ---: |")
    enemy_by_name = {e.name.title(): e for e in stats.enemies}
    for name in ["Grunt", "Runner", "Tank", "Kamikaze", "Boss"]:
        e = enemy_by_name.get(name)
        if e:
            lines.append(f"| {name} | {enemy_totals[name]} | {e.hp} | {e.speed:.0f} | {e.melee} |")

    lines.append("\n## Weapon table\n")
    projectile_speed = float(stats.constants.get("ProjectileSpeed", 600))
    lines.append("| Weapon | Damage | Cooldown | DPS | Ammo cap | Range |")
    lines.append("| --- | ---: | ---: | ---: | ---: | ---: |")
    for w in stats.weapons:
        ammo = "∞" if w.ammo_max >= 9999 else str(w.ammo_max)
        lines.append(f"| {w.name.title()} | {w.damage} | {w.cooldown_ms} ms | {w.dps:.1f} | {ammo} | {projectile_speed*w.life_sec:.0f} px |")

    lines.append("\n## Intentional bug snapshot\n")
    diff_counts = Counter(b.difficulty for b in active_bugs)
    lines.append(f"- Active bugs by difficulty: " + ", ".join(f"{k}: {v}" for k, v in sorted(diff_counts.items())))
    lines.append("| ID | Name | Difficulty | Savings | Status |")
    lines.append("| --- | --- | --- | --- | --- |")
    for b in stats.bugs:
        lines.append(f"| {b.bug_id} | {b.name} | {b.difficulty} | {b.savings} | {b.status} |")

    lines.append("\n## Codebase size snapshot\n")
    lines.append("Excludes lockfiles and Go sums.\n")
    lines.append("| Type | Files | Lines |")
    lines.append("| --- | ---: | ---: |")
    for lang, (files, loc) in sorted(stats.code_stats.items(), key=lambda kv: kv[1][1], reverse=True):
        lines.append(f"| {lang} | {files} | {loc:,} |")

    path = OUT / "slide_stats_summary.md"
    path.write_text("\n".join(lines) + "\n", encoding="utf-8")
    return path


def main() -> None:
    OUT.mkdir(exist_ok=True)
    setup_mpl()
    stats = collect_stats()
    image_paths = [
        make_stats_cards(stats),
        make_room_montage(stats),
        make_room_difficulty_chart(stats),
        make_weapon_chart(stats),
        make_enemy_chart(stats),
        make_bug_ladder(stats),
    ]
    summary = make_summary(stats, image_paths)
    print("Generated slide assets:")
    for p in image_paths:
        print(f"  {p.relative_to(ROOT)}")
    print(f"  {summary.relative_to(ROOT)}")


if __name__ == "__main__":
    main()
