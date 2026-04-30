#!/usr/bin/env python3
"""Generate developer/behind-the-scenes slide assets from Git history.

Outputs 16:9 PNGs and a Markdown summary under ./slide_assets/dev_insights/.
The charts are intentionally presentation-friendly rather than forensic: they
answer "what did development feel like?" — cadence, crunch windows, authors,
subsystems, and intentional bug lifecycle.
"""

from __future__ import annotations

import io
import math
import re
import subprocess
import textwrap
from collections import Counter, defaultdict
from dataclasses import dataclass, field
from datetime import date, datetime, timedelta
from pathlib import Path
from zoneinfo import ZoneInfo

import matplotlib

matplotlib.use("Agg")
import matplotlib.dates as mdates
import matplotlib.pyplot as plt
import numpy as np
from PIL import Image, ImageDraw, ImageEnhance, ImageFont

ROOT = Path(__file__).resolve().parents[1]
OUT = ROOT / "slide_assets" / "dev_insights"
SGT = ZoneInfo("Asia/Singapore")

BG = "#0b1020"
PANEL = "#121a31"
PANEL2 = "#17213d"
TEXT = "#eef2ff"
MUTED = "#aab4d4"
GRID = "#263458"
ACCENT = "#66e3ff"
YELLOW = "#ffd166"
GREEN = "#06d6a0"
RED = "#ef476f"
ORANGE = "#f97316"
PURPLE = "#8b5cf6"
BLUE = "#3b82f6"
GRAY = "#64748b"

FONT_REG = "/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf"
FONT_BOLD = "/usr/share/fonts/truetype/dejavu/DejaVuSans-Bold.ttf"
FONT_MONO = "/usr/share/fonts/truetype/dejavu/DejaVuSansMono.ttf"

AREA_COLORS = {
    "server/game": "#66e3ff",
    "client/ui": "#06d6a0",
    "levels/art": "#f97316",
    "lobby/admin": "#8b5cf6",
    "docs/spec": "#ffd166",
    "observability": "#ef476f",
    "deploy/ci": "#3b82f6",
    "shared/pkg": "#14b8a6",
    "tools": "#a78bfa",
    "slides": "#94a3b8",
    "merge": "#475569",
    "other": "#94a3b8",
}

BUG_STATUS_COLORS = {
    "Active in HEAD": "#06d6a0",
    "Disabled payload removed": "#f97316",
    "Retired subsystem removed": "#64748b",
}
BUG_DIFFICULTY_COLORS = {"Easy": "#06d6a0", "Medium": "#ffd166", "Hard": "#ef476f"}


def font(size: int, bold: bool = False, mono: bool = False) -> ImageFont.FreeTypeFont:
    return ImageFont.truetype(FONT_MONO if mono else (FONT_BOLD if bold else FONT_REG), size)


def setup_mpl() -> None:
    plt.rcParams.update(
        {
            "figure.facecolor": BG,
            "axes.facecolor": BG,
            "savefig.facecolor": BG,
            "axes.edgecolor": GRID,
            "axes.labelcolor": TEXT,
            "xtick.color": MUTED,
            "ytick.color": MUTED,
            "text.color": TEXT,
            "font.family": "DejaVu Sans",
            "axes.titleweight": "bold",
            "axes.titlesize": 20,
            "axes.labelsize": 12,
            "legend.facecolor": PANEL,
            "legend.edgecolor": GRID,
        }
    )


def save_fig_slide(path: Path) -> None:
    """Save current Matplotlib figure into an exact 1920×1080 canvas."""
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
    canvas.paste(chart, ((1920 - new_size[0]) // 2, (1080 - new_size[1]) // 2))
    canvas.save(path)


def rounded(draw: ImageDraw.ImageDraw, box, radius=20, fill=(18, 26, 49, 230), outline=None, width=1):
    draw.rounded_rectangle(box, radius=radius, fill=fill, outline=outline, width=width)


@dataclass
class FileStat:
    insertions: int | None
    deletions: int | None
    path: str

    @property
    def churn(self) -> int:
        if self.insertions is None or self.deletions is None:
            return 0
        return self.insertions + self.deletions


@dataclass
class Commit:
    full_hash: str
    short_hash: str
    dt: datetime
    dt_sgt: datetime
    author: str
    email: str
    subject: str
    files: list[FileStat] = field(default_factory=list)

    @property
    def is_merge(self) -> bool:
        return self.subject.startswith("Merge")

    @property
    def pr_merge(self) -> bool:
        return "Merge pull request #" in self.subject

    @property
    def insertions(self) -> int:
        return sum(f.insertions or 0 for f in self.files)

    @property
    def deletions(self) -> int:
        return sum(f.deletions or 0 for f in self.files)

    @property
    def churn(self) -> int:
        return self.insertions + self.deletions

    @property
    def paths(self) -> list[str]:
        return [f.path for f in self.files]


def run_git(args: list[str]) -> str:
    return subprocess.check_output(["git", *args], cwd=ROOT, text=True)


def parse_git_history() -> list[Commit]:
    raw = run_git([
        "log",
        "HEAD",
        "--date=iso-strict",
        "--numstat",
        "--pretty=format:@@@%H%x09%h%x09%ad%x09%an%x09%ae%x09%s",
    ])
    commits: list[Commit] = []
    cur: Commit | None = None
    for line in raw.splitlines():
        if line.startswith("@@@"):
            if cur is not None:
                commits.append(cur)
            parts = line[3:].split("\t", 5)
            full_hash, short_hash, dt_str, author, email, subject = parts
            dt = datetime.fromisoformat(dt_str)
            cur = Commit(full_hash, short_hash, dt, dt.astimezone(SGT), author, email, subject)
            continue
        if not line.strip() or cur is None:
            continue
        a, b, path = line.split("\t", 2)
        if a == "-" or b == "-":
            cur.files.append(FileStat(None, None, path))
        else:
            cur.files.append(FileStat(int(a), int(b), path))
    if cur is not None:
        commits.append(cur)
    return commits


def classify_path(path: str) -> str:
    if path.startswith("slide_assets/"):
        return "slides"
    if path.startswith("server/internal/game/tilemaps/") or path.startswith("client/public/assets/rooms/") or path.startswith("art/"):
        return "levels/art"
    if path.startswith("server/"):
        return "server/game"
    if path.startswith("client/"):
        return "client/ui"
    if path.startswith("lobby/"):
        return "lobby/admin"
    if path.startswith("obs/"):
        return "observability"
    if path.startswith("docs/") or path in {"README.md", "SPECIFICATIONS.md"}:
        return "docs/spec"
    if path.startswith(".github/") or path.startswith("docker-compose") or path.endswith("Dockerfile") or path.endswith("Caddyfile"):
        return "deploy/ci"
    if path.startswith("pkg/"):
        return "shared/pkg"
    if path.startswith("tools/"):
        return "tools"
    return "other"


def primary_area(c: Commit) -> str:
    if c.is_merge and not c.files:
        return "merge"
    weights: Counter[str] = Counter()
    for fs in c.files:
        area = classify_path(fs.path)
        weights[area] += max(fs.churn, 1)
    if not weights:
        return "merge" if c.is_merge else "other"
    return weights.most_common(1)[0][0]


def date_range(commits: list[Commit]) -> list[date]:
    start = min(c.dt_sgt.date() for c in commits)
    end = max(c.dt_sgt.date() for c in commits)
    days = []
    d = start
    while d <= end:
        days.append(d)
        d += timedelta(days=1)
    return days


def safe_pct(n: int, d: int) -> str:
    return f"{100*n/d:.0f}%" if d else "0%"


def fmt_day(d: date) -> str:
    return d.strftime("%b %-d") if "% -" not in "% -" else d.strftime("%b %d")


def history_metrics(commits: list[Commit]) -> dict[str, object]:
    commits_chrono = sorted(commits, key=lambda c: c.dt_sgt)
    days = date_range(commits)
    per_day = Counter(c.dt_sgt.date() for c in commits)
    per_hour = Counter(c.dt_sgt.hour for c in commits)
    per_weekday = Counter(c.dt_sgt.strftime("%A") for c in commits)
    authors = Counter(c.author for c in commits)
    total = len(commits)
    weekend = sum(1 for c in commits if c.dt_sgt.weekday() >= 5)
    after_hours = sum(1 for c in commits if c.dt_sgt.hour < 9 or c.dt_sgt.hour >= 18)
    night = sum(1 for c in commits if c.dt_sgt.hour >= 22 or c.dt_sgt.hour < 4)
    gaps = [(commits_chrono[i].dt_sgt - commits_chrono[i - 1].dt_sgt, commits_chrono[i - 1], commits_chrono[i]) for i in range(1, len(commits_chrono))]
    longest_gap = max(gaps, key=lambda x: x[0]) if gaps else None

    def rolling_window(hours: int) -> tuple[int, datetime, datetime]:
        best = (0, commits_chrono[0].dt_sgt, commits_chrono[0].dt_sgt)
        j = 0
        window = timedelta(hours=hours)
        for i, c in enumerate(commits_chrono):
            while c.dt_sgt - commits_chrono[j].dt_sgt > window:
                j += 1
            count = i - j + 1
            if count > best[0]:
                best = (count, commits_chrono[j].dt_sgt, c.dt_sgt)
        return best

    unique_files = {p for c in commits for p in c.paths}
    binary_touches = sum(1 for c in commits for f in c.files if f.insertions is None)
    return {
        "commits_chrono": commits_chrono,
        "days": days,
        "per_day": per_day,
        "per_hour": per_hour,
        "per_weekday": per_weekday,
        "authors": authors,
        "total": total,
        "start": commits_chrono[0].dt_sgt,
        "end": commits_chrono[-1].dt_sgt,
        "calendar_days": (commits_chrono[-1].dt_sgt.date() - commits_chrono[0].dt_sgt.date()).days + 1,
        "weekend": weekend,
        "after_hours": after_hours,
        "night": night,
        "longest_gap": longest_gap,
        "rolling_1h": rolling_window(1),
        "rolling_6h": rolling_window(6),
        "rolling_24h": rolling_window(24),
        "insertions": sum(c.insertions for c in commits),
        "deletions": sum(c.deletions for c in commits),
        "churn": sum(c.churn for c in commits),
        "unique_files": len(unique_files),
        "binary_touches": binary_touches,
        "merge_commits": sum(1 for c in commits if c.is_merge),
        "pr_merges": sum(1 for c in commits if c.pr_merge),
    }


def make_dev_overview_cards(commits: list[Commit], metrics: dict[str, object]) -> Path:
    W, H = 1920, 1080
    bg_path = ROOT / "client/public/assets/title.png"
    if bg_path.exists():
        bg = Image.open(bg_path).convert("RGB").resize((W, H), Image.Resampling.LANCZOS)
        bg = ImageEnhance.Brightness(bg).enhance(0.20)
    else:
        bg = Image.new("RGB", (W, H), (11, 16, 32))
    img = Image.alpha_composite(bg.convert("RGBA"), Image.new("RGBA", (W, H), (5, 8, 18, 115)))
    draw = ImageDraw.Draw(img, "RGBA")

    per_day: Counter[date] = metrics["per_day"]  # type: ignore[assignment]
    peak_day, peak_count = per_day.most_common(1)[0]
    rolling_6h = metrics["rolling_6h"]  # type: ignore[assignment]
    rolling_24h = metrics["rolling_24h"]  # type: ignore[assignment]
    top_author, top_author_count = metrics["authors"].most_common(1)[0]  # type: ignore[index, union-attr]

    draw.text((70, 54), "Behind the scenes", font=font(68, bold=True), fill=(238, 242, 255, 255))
    draw.text((74, 132), "Git history shows a very real hackathon curve", font=font(34, bold=True), fill=(102, 227, 255, 255))
    draw.text((76, 184), f"Current branch HEAD: {commits[0].short_hash} • timestamps converted to Singapore time", font=font(22), fill=(170, 180, 212, 255))

    cards = [
        (f"{metrics['total']}", "commits", f"from {metrics['start'].strftime('%b %-d')} to {metrics['end'].strftime('%b %-d')}"),
        (f"{metrics['calendar_days']}", "calendar days", f"{metrics['calendar_days']/7:.1f} weeks end-to-end"),
        (f"{len(metrics['authors'])}", "author identities", f"top: {top_author} ({top_author_count})"),
        (f"{metrics['churn']//1000}k", "text-line churn", f"+{metrics['insertions']:,} / −{metrics['deletions']:,}"),
        (f"{metrics['unique_files']}", "unique files touched", f"{metrics['binary_touches']} binary asset touches"),
        (f"{peak_count}", "peak SGT day", f"{peak_day.strftime('%b %-d')} crunch spike"),
        (f"{rolling_6h[0]}", "commits in 6 hours", f"{rolling_6h[1].strftime('%b %-d %H:%M')}→{rolling_6h[2].strftime('%H:%M')}"),
        (f"{rolling_24h[0]}", "commits in 24 hours", f"peak rolling window"),
        (safe_pct(metrics["after_hours"], metrics["total"]), "after-hours commits", f"{metrics['night']} landed 22:00–03:59"),
        (safe_pct(metrics["weekend"], metrics["total"]), "weekend commits", "Saturday/Sunday crunch"),
        (f"{metrics['merge_commits']}", "merge commits", f"{metrics['pr_merges']} GitHub PR merges"),
        ("29d", "longest quiet gap", "then April sprint exploded"),
    ]

    card_w, card_h = 400, 150
    start_x, start_y = 72, 260
    gap_x, gap_y = 52, 40
    for i, (big, label, sub) in enumerate(cards):
        row, col = divmod(i, 4)
        x = start_x + col * (card_w + gap_x)
        y = start_y + row * (card_h + gap_y)
        rounded(draw, (x + 8, y + 10, x + card_w + 8, y + card_h + 10), 24, (0, 0, 0, 90))
        rounded(draw, (x, y, x + card_w, y + card_h), 24, (18, 26, 49, 225), outline=(102, 227, 255, 90), width=2)
        draw.text((x + 26, y + 18), str(big), font=font(45, bold=True), fill=(255, 209, 102, 255))
        draw.text((x + 28, y + 77), label.upper(), font=font(18, bold=True), fill=(238, 242, 255, 255))
        draw.text((x + 28, y + 108), sub, font=font(17), fill=(170, 180, 212, 255))

    # Sparkline of daily commits in footer.
    days: list[date] = metrics["days"]  # type: ignore[assignment]
    vals = [per_day[d] for d in days]
    x0, y0, w, h = 88, 890, 1745, 105
    rounded(draw, (x0 - 18, y0 - 28, x0 + w + 18, y0 + h + 42), 20, (11, 16, 32, 215), outline=(42, 53, 92, 180))
    maxv = max(vals) or 1
    bar_w = max(2, w / len(days))
    for i, v in enumerate(vals):
        bx = x0 + i * bar_w
        bh = h * v / maxv
        color = (102, 227, 255, 230) if v else (71, 85, 105, 70)
        if v >= maxv * 0.8:
            color = (239, 71, 111, 240)
        draw.rectangle((bx, y0 + h - bh, bx + max(1, bar_w - 1), y0 + h), fill=color)
    draw.text((x0, y0 - 22), "Daily commit sparkline — the April 26/27 wall is the story", font=font(22, bold=True), fill=(238, 242, 255, 255))
    draw.text((x0, y0 + h + 12), f"{days[0].strftime('%b %-d')} → {days[-1].strftime('%b %-d, %Y')}", font=font(18), fill=(170, 180, 212, 255))

    path = OUT / "defcon_dev_overview_cards.png"
    img.convert("RGB").save(path)
    return path


def make_commit_timeline(commits: list[Commit], metrics: dict[str, object]) -> Path:
    days: list[date] = metrics["days"]  # type: ignore[assignment]
    areas = ["server/game", "client/ui", "levels/art", "lobby/admin", "docs/spec", "observability", "deploy/ci", "merge", "other", "slides"]
    daily_area: dict[str, list[int]] = {a: [] for a in areas}
    for d in days:
        cs = [c for c in commits if c.dt_sgt.date() == d]
        counts = Counter(primary_area(c) for c in cs)
        for a in areas:
            daily_area[a].append(counts.get(a, 0))
    daily_total = np.array([sum(daily_area[a][i] for a in areas) for i in range(len(days))])
    cumulative = np.cumsum(daily_total)
    x = [datetime.combine(d, datetime.min.time()) for d in days]

    fig, (ax1, ax2) = plt.subplots(2, 1, figsize=(18, 10), sharex=True, gridspec_kw={"height_ratios": [2.2, 1]})
    bottom = np.zeros(len(days))
    for a in areas:
        vals = np.array(daily_area[a])
        if vals.sum() == 0:
            continue
        ax1.bar(x, vals, bottom=bottom, width=0.82, color=AREA_COLORS.get(a, "#94a3b8"), label=a)
        bottom += vals
    ax1.set_title("Development schedule: long fuse, then a compressed April shipping sprint")
    ax1.set_ylabel("Commits per day")
    ax1.grid(axis="y", color=GRID, alpha=0.65)
    ax1.legend(ncol=5, loc="upper left", bbox_to_anchor=(0, 1.18), fontsize=9)

    # Key annotations from actual history.
    annotations = [
        (date(2026, 2, 25), "repo\nscaffold"),
        (date(2026, 4, 21), "spec\ndraft"),
        (date(2026, 4, 23), "server/client\nrewrite"),
        (date(2026, 4, 25), "shop + bug\npruning"),
        (date(2026, 4, 26), "levels/lobby\ncrunch"),
        (date(2026, 4, 27), "peak\nintegration"),
        (date(2026, 4, 28), "prod + obs"),
        (date(2026, 4, 30), "replay\nfixes"),
    ]
    max_total = max(daily_total) or 1
    for d, label in annotations:
        if days[0] <= d <= days[-1]:
            xdt = datetime.combine(d, datetime.min.time())
            ax1.axvline(xdt, color="#ffffff", alpha=0.16, lw=1)
            ax1.text(xdt, max_total * 1.05, label, ha="center", va="bottom", fontsize=9, color=YELLOW)

    ax2.plot(x, cumulative, color=ACCENT, lw=3)
    ax2.fill_between(x, cumulative, color=ACCENT, alpha=0.18)
    ax2.set_ylabel("Cumulative")
    ax2.grid(axis="y", color=GRID, alpha=0.65)
    ax2.xaxis.set_major_locator(mdates.WeekdayLocator(interval=1))
    ax2.xaxis.set_major_formatter(mdates.DateFormatter("%b %d"))
    ax2.text(0.995, 0.08, "Stacked bars use each commit's primary touched subsystem", transform=ax2.transAxes, ha="right", color=MUTED, fontsize=10)
    fig.autofmt_xdate(rotation=30)
    path = OUT / "defcon_commit_timeline_schedule.png"
    save_fig_slide(path)
    return path


def make_commit_heatmap(commits: list[Commit], metrics: dict[str, object]) -> Path:
    matrix = np.zeros((7, 24), dtype=int)
    for c in commits:
        matrix[c.dt_sgt.weekday(), c.dt_sgt.hour] += 1
    weekdays = ["Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"]

    fig, ax = plt.subplots(figsize=(18, 9))
    im = ax.imshow(matrix, aspect="auto", cmap="magma")
    ax.set_title("Commit-time graph: when the team actually shipped code (Singapore time)")
    ax.set_xlabel("Hour of day")
    ax.set_ylabel("Day of week")
    ax.set_xticks(np.arange(24), [f"{h:02d}" for h in range(24)])
    ax.set_yticks(np.arange(7), weekdays)
    for r in range(7):
        for h in range(24):
            v = matrix[r, h]
            if v:
                ax.text(h, r, str(v), ha="center", va="center", color="white" if v > matrix.max() * 0.45 else "#111827", fontsize=8, fontweight="bold")
    cbar = fig.colorbar(im, ax=ax, fraction=0.025, pad=0.02)
    cbar.set_label("Commits")
    peak_idx = np.unravel_index(matrix.argmax(), matrix.shape)
    ax.scatter([peak_idx[1]], [peak_idx[0]], s=260, facecolors="none", edgecolors=ACCENT, linewidths=3)
    ax.text(
        0.01,
        -0.14,
        f"After-hours: {metrics['after_hours']} / {metrics['total']} ({safe_pct(metrics['after_hours'], metrics['total'])}); "
        f"weekend: {metrics['weekend']} ({safe_pct(metrics['weekend'], metrics['total'])}); "
        f"peak cell: {weekdays[peak_idx[0]]} {peak_idx[1]:02d}:00 with {matrix.max()} commits.",
        transform=ax.transAxes,
        color=MUTED,
        fontsize=12,
    )
    path = OUT / "defcon_commit_time_heatmap.png"
    save_fig_slide(path)
    return path


def make_author_churn_chart(commits: list[Commit], metrics: dict[str, object]) -> Path:
    authors: Counter[str] = metrics["authors"]  # type: ignore[assignment]
    top = authors.most_common(10)
    names = [n for n, _ in top][::-1]
    vals = [v for _, v in top][::-1]
    churn_by_author = Counter()
    files_by_author = Counter()
    for c in commits:
        churn_by_author[c.author] += c.churn
        files_by_author[c.author] += len(c.files)

    fig, (ax1, ax2) = plt.subplots(1, 2, figsize=(18, 10), gridspec_kw={"width_ratios": [1.05, 1]})
    y = np.arange(len(names))
    colors = plt.cm.viridis(np.linspace(0.25, 0.95, len(names)))
    ax1.barh(y, vals, color=colors)
    ax1.set_yticks(y, names)
    ax1.set_xlabel("Commits")
    ax1.set_title("Top author identities by commit count")
    ax1.grid(axis="x", color=GRID, alpha=0.65)
    for yi, name, v in zip(y, names, vals):
        ax1.text(v + 1, yi, f"{v}", va="center", fontsize=12, fontweight="bold")
    ax1.set_xlim(0, max(vals) * 1.22)

    xs = [authors[a] for a in authors]
    ys = [churn_by_author[a] for a in authors]
    sizes = [80 + min(files_by_author[a], 800) for a in authors]
    colors2 = [AREA_COLORS.get("client/ui") if a in {"mug1wara26", "Dylan-Liew", "Ge Shuqing"} else AREA_COLORS.get("server/game") for a in authors]
    ax2.scatter(xs, ys, s=sizes, c=colors2, alpha=0.82, edgecolors=TEXT, linewidths=1.0)
    for a in authors:
        if authors[a] >= 12 or churn_by_author[a] > 20000:
            ax2.annotate(a, (authors[a], churn_by_author[a]), xytext=(8, 7), textcoords="offset points", fontsize=9, fontweight="bold")
    ax2.set_xlabel("Commits")
    ax2.set_ylabel("Text-line churn (+/−)")
    ax2.set_title("Commit count vs. code churn")
    ax2.grid(color=GRID, alpha=0.65)
    ax2.text(0.02, 0.04, "Bubble size ≈ file touches. Generated dashboards/specs can create large churn spikes.", transform=ax2.transAxes, color=MUTED, fontsize=10)
    fig.suptitle("Who moved the repo: many small commits, a few huge churn bursts", fontsize=24, fontweight="bold")
    path = OUT / "defcon_author_commit_churn.png"
    save_fig_slide(path)
    return path


def make_subsystem_churn_chart(commits: list[Commit], metrics: dict[str, object]) -> Path:
    commits_by_area = Counter(primary_area(c) for c in commits)
    churn_by_area = Counter()
    ins_by_area = Counter()
    del_by_area = Counter()
    files_by_area = Counter()
    for c in commits:
        seen = set()
        for fs in c.files:
            a = classify_path(fs.path)
            if fs.insertions is not None:
                ins_by_area[a] += fs.insertions
                del_by_area[a] += fs.deletions or 0
                churn_by_area[a] += fs.churn
            files_by_area[a] += 1
            seen.add(a)
    areas = [a for a, _ in (commits_by_area + churn_by_area).most_common() if a != "other"]
    if "other" in commits_by_area or "other" in churn_by_area:
        areas.append("other")
    areas = areas[:10]

    fig, (ax1, ax2) = plt.subplots(1, 2, figsize=(18, 10), gridspec_kw={"width_ratios": [1, 1.15]})
    labels = areas[::-1]
    vals = [commits_by_area[a] for a in labels]
    ax1.barh(np.arange(len(labels)), vals, color=[AREA_COLORS.get(a, "#94a3b8") for a in labels])
    ax1.set_yticks(np.arange(len(labels)), labels)
    ax1.set_xlabel("Primary-area commits")
    ax1.set_title("Where commits landed")
    ax1.grid(axis="x", color=GRID, alpha=0.65)
    for yi, a, v in zip(np.arange(len(labels)), labels, vals):
        ax1.text(v + 1, yi, str(v), va="center", fontweight="bold")

    x = np.arange(len(areas))
    ins = np.array([ins_by_area[a] for a in areas])
    dele = np.array([del_by_area[a] for a in areas])
    ax2.bar(x, ins / 1000, color=GREEN, label="insertions")
    ax2.bar(x, -dele / 1000, color=RED, label="deletions")
    ax2.axhline(0, color=TEXT, lw=1, alpha=0.45)
    ax2.set_xticks(x, areas, rotation=35, ha="right")
    ax2.set_ylabel("Thousands of lines")
    ax2.set_title("Subsystem churn: build-up and rewrites")
    ax2.legend(ncol=2)
    ax2.grid(axis="y", color=GRID, alpha=0.65)
    for xi, a in zip(x, areas):
        ax2.text(xi, ins_by_area[a] / 1000 + 1, f"{churn_by_area[a]//1000}k", ha="center", fontsize=9, color=YELLOW)
    fig.suptitle("The repo was not one thing: game loop, UI, maps, lobby, observability, deploy", fontsize=22, fontweight="bold")
    path = OUT / "defcon_subsystem_churn.png"
    save_fig_slide(path)
    return path


def make_burst_timeline(commits: list[Commit], metrics: dict[str, object]) -> Path:
    cs: list[Commit] = metrics["commits_chrono"]  # type: ignore[assignment]
    times = [c.dt_sgt for c in cs]
    rolling6 = []
    j = 0
    for i, t in enumerate(times):
        while t - times[j] > timedelta(hours=6):
            j += 1
        rolling6.append(i - j + 1)
    gaps_min = [(times[i] - times[i - 1]).total_seconds() / 60 for i in range(1, len(times))]

    fig, (ax1, ax2) = plt.subplots(2, 1, figsize=(18, 10), gridspec_kw={"height_ratios": [2, 1]})
    ax1.plot(times, rolling6, color=ACCENT, lw=2)
    ax1.scatter(times, rolling6, s=18, color=ACCENT, alpha=0.85)
    best = metrics["rolling_6h"]  # type: ignore[assignment]
    ax1.axvspan(best[1], best[2], color=RED, alpha=0.18)
    ax1.text(best[2], best[0] + 2, f"{best[0]} commits / 6h", color=YELLOW, fontweight="bold", ha="right")
    ax1.set_title("Crunch detector: rolling six-hour commit bursts")
    ax1.set_ylabel("Commits in previous 6h")
    ax1.grid(color=GRID, alpha=0.65)
    ax1.xaxis.set_major_locator(mdates.WeekdayLocator(interval=1))
    ax1.xaxis.set_major_formatter(mdates.DateFormatter("%b %d"))

    bins = [0, 2, 5, 10, 20, 30, 60, 120, 360, 720, 1440, 10080, max(max(gaps_min) + 1, 10081)]
    ax2.hist(gaps_min, bins=bins, color=PURPLE, edgecolor=TEXT, alpha=0.85)
    ax2.set_xscale("symlog", linthresh=60)
    ax2.set_xlabel("Minutes since previous commit (symlog scale)")
    ax2.set_ylabel("Count")
    ax2.set_title("Inter-commit gaps: lots of tiny loops, one long pre-sprint pause")
    ax2.grid(axis="y", color=GRID, alpha=0.65)
    longest = metrics["longest_gap"]  # type: ignore[assignment]
    if longest:
        ax2.text(0.99, 0.82, f"Longest gap: {longest[0].days}d {longest[0].seconds//3600}h", transform=ax2.transAxes, ha="right", color=YELLOW, fontsize=12, fontweight="bold")
    fig.autofmt_xdate(rotation=30)
    path = OUT / "defcon_commit_burst_detector.png"
    save_fig_slide(path)
    return path


def make_calendar_heatmap(commits: list[Commit], metrics: dict[str, object]) -> Path:
    W, H = 1920, 1080
    img = Image.new("RGB", (W, H), (11, 16, 32))
    draw = ImageDraw.Draw(img, "RGBA")
    draw.text((64, 48), "Development calendar heatmap", font=font(54, bold=True), fill=(238, 242, 255, 255))
    draw.text((68, 112), "Each square is a day; brighter means more commits. The late-April wall is unmistakable.", font=font(24), fill=(170, 180, 212, 255))

    per_day: Counter[date] = metrics["per_day"]  # type: ignore[assignment]
    months = [(2026, 2), (2026, 3), (2026, 4)]
    start_x = 80
    month_w = 560
    cell = 30
    gap = 6
    top = 225
    maxv = max(per_day.values()) or 1
    month_names = ["February", "March", "April"]
    weekdays = ["M", "T", "W", "T", "F", "S", "S"]
    for mi, (year, month) in enumerate(months):
        x0 = start_x + mi * (month_w + 20)
        draw.text((x0, top - 58), f"{month_names[mi]} {year}", font=font(30, bold=True), fill=(238, 242, 255, 255))
        for wd, label in enumerate(weekdays):
            draw.text((x0 - 32, top + wd * (cell + gap) + 3), label, font=font(18, bold=True), fill=(170, 180, 212, 255))
        first = date(year, month, 1)
        next_month = date(year + (month == 12), 1 if month == 12 else month + 1, 1)
        d = first
        while d < next_month:
            week = (d.day + first.weekday() - 1) // 7
            wd = d.weekday()
            x = x0 + week * (cell + gap)
            y = top + wd * (cell + gap)
            v = per_day.get(d, 0)
            if v == 0:
                fill = (30, 41, 67, 160)
            else:
                intensity = min(1.0, math.log1p(v) / math.log1p(maxv))
                fill = (int(20 + 235 * intensity), int(80 + 130 * (1 - intensity)), int(130 + 80 * (1 - intensity)), 230)
                if v > 20:
                    fill = (239, 71, 111, 235)
            rounded(draw, (x, y, x + cell, y + cell), 6, fill)
            if v:
                draw.text((x + cell / 2 - 5, y + 5), str(v), font=font(12, bold=True), fill=(255, 255, 255, 255))
            d += timedelta(days=1)

    # Busiest days callout list.
    top_days = per_day.most_common(6)
    rounded(draw, (86, 650, 1834, 956), 28, (18, 26, 49, 225), outline=(102, 227, 255, 90), width=2)
    draw.text((118, 682), "Busiest days", font=font(34, bold=True), fill=(255, 209, 102, 255))
    x = 124
    for i, (d, v) in enumerate(top_days):
        bx = x + i * 285
        rounded(draw, (bx, 742, bx + 250, 905), 20, (23, 33, 61, 235), outline=(71, 85, 105, 180))
        draw.text((bx + 24, 760), str(v), font=font(48, bold=True), fill=(239, 71, 111, 255))
        draw.text((bx + 24, 824), d.strftime("%b %-d"), font=font(25, bold=True), fill=(238, 242, 255, 255))
        draw.text((bx + 24, 862), d.strftime("%A"), font=font(19), fill=(170, 180, 212, 255))
    path = OUT / "defcon_development_calendar_heatmap.png"
    img.save(path)
    return path


@dataclass
class PlannedBug:
    bug_id: str
    name: str
    tier: str
    difficulty: str
    savings: str
    head_status: str
    evidence: str
    patched_by: str = ""


def planned_bugs() -> list[PlannedBug]:
    return [
        PlannedBug("BUG-01", "Mob Seed Optimization", "Systems", "Hard", "~3–5 s/room", "Active in HEAD", "MobRNG seed chosen from C2S_START_RUN start_time; spawn RNG preserved."),
        PlannedBug("BUG-02", "Drift Tick Stretching", "Systems", "Hard", "~15–20% total", "Active in HEAD", "Per-packet ±2s timestamp window; no cumulative drift ledger."),
        PlannedBug("BUG-03", "Door Skip / No Proximity", "Surface", "Easy", "~2–3 s/room", "Active in HEAD", "HandleEnterDoor ignores PlayerState distance (`_ = p`)."),
        PlannedBug("BUG-04", "Swap Cancel Animation", "Protocol", "Easy", "~1–2 s/room", "Active in HEAD", "Tick-shared `now` and swap/shoot batching remain."),
        PlannedBug("BUG-05", "Interact Range Bug", "Surface", "Easy", "~1–2 s/pickup", "Active in HEAD", "InteractRange = 2500 and axis-square check."),
        PlannedBug("BUG-06", "Concurrent Room Settle Race", "Systems", "Hard", "~3–5 s/room", "Active in HEAD", "Door transition loads next room during input drain; no shooting gate on !Settled."),
        PlannedBug("BUG-07", "Damage Batch Overflow", "Protocol", "Medium", "~2–3 s/room", "Active in HEAD", "ResolveCollisions applies all hits before death pass."),
        PlannedBug("BUG-08", "HP Integer Overflow", "Language", "Medium", "~5–10 s/boss", "Disabled payload removed", "HP is still int16, but PickupHeart / +30000 heal route is gone.", "041d346 trim pickups"),
        PlannedBug("BUG-09", "JSON Case-Insensitive Field Smuggling", "Language", "Medium", "unlock hidden items", "Retired subsystem removed", "C2S_USE_ITEM, UseItemPacket, and inventory dispatch no longer exist.", "e720683 debug items; 6fec927 inventory"),
        PlannedBug("BUG-10", "Rune vs Byte Item Name Bypass", "Language", "Medium", "~3–5 s/room", "Retired subsystem removed", "Item-name validation file deleted with inventory subsystem.", "6fec927 inventory"),
        PlannedBug("BUG-11", "Kill Log Race Condition", "Language", "Hard", "~5–8 s total", "Active in HEAD", "ProcessEnemyDeathAsync appends to KillLog without a mutex under !norace."),
        PlannedBug("BUG-12", "Pickup Type Confusion", "Protocol", "Easy", "~10–20 s/run", "Active in HEAD", "InteractPacket.ClaimedType is trusted by applyPickup."),
    ]


def audit_bug_presence() -> dict[str, bool]:
    """Lightweight HEAD audit for the bug roster. True means reachable substrate exists."""
    files = {
        "constants": (ROOT / "server/internal/game/constants.go").read_text(),
        "rooms": (ROOT / "server/internal/game/rooms.go").read_text(),
        "combat": (ROOT / "server/internal/game/combat.go").read_text(),
        "pickups": (ROOT / "server/internal/game/pickups.go").read_text(),
        "loop": (ROOT / "server/internal/game/loop.go").read_text(),
        "packets": (ROOT / "server/internal/protocol/packets.go").read_text(),
        "envelope": (ROOT / "server/internal/protocol/envelope.go").read_text(),
        "killlog": (ROOT / "server/internal/game/killlog.go").read_text(),
        "state": (ROOT / "server/internal/game/state.go").read_text(),
    }
    items_go_exists = (ROOT / "server/internal/game/items.go").exists()
    all_text = "\n".join(files.values())
    return {
        "BUG-01": "SeedMob" in all_text and "StartTime" in all_text,
        "BUG-02": "No cumulative drift" in all_text or "no cumulative drift" in all_text,
        "BUG-03": "_ = p" in files["rooms"] and "HandleEnterDoor" in files["rooms"],
        "BUG-04": "BUG-04" in files["combat"] and "tick-shared" in files["loop"],
        "BUG-05": "InteractRange = 2500" in files["constants"] and "math.Abs" in files["pickups"],
        "BUG-06": "transitionToRoom" in files["loop"] and "Settled" in files["loop"] and "HandleShoot" in files["loop"],
        "BUG-07": "all hits, no early death culling" in files["combat"],
        "BUG-08": "PickupHeart" in all_text or "ApplyHeal(p, 30000)" in all_text,
        "BUG-09": "C2SUseItem" in files["envelope"] or "UseItemPacket" in files["packets"],
        "BUG-10": items_go_exists and "ValidateItemName" in ((ROOT / "server/internal/game/items.go").read_text() if items_go_exists else ""),
        "BUG-11": "append(r.KillLog" in files["killlog"] and "WITHOUT a mutex" in files["killlog"],
        "BUG-12": "ClaimedType" in files["packets"] and "claimedType" in files["pickups"],
    }


def make_bug_roster_matrix(commits: list[Commit]) -> Path:
    bugs = planned_bugs()
    audit = audit_bug_presence()
    W, H = 1920, 1080
    img = Image.new("RGB", (W, H), (11, 16, 32))
    draw = ImageDraw.Draw(img, "RGBA")
    draw.text((58, 42), "Intentional bug roster: planned vs current HEAD", font=font(48, bold=True), fill=(238, 242, 255, 255))
    draw.text((62, 100), "The fun part of the game was also a moving target. Current HEAD keeps 9 active exploit substrates; 3 planned bugs are unreachable.", font=font(22), fill=(170, 180, 212, 255))

    active = sum(1 for b in bugs if b.head_status == "Active in HEAD")
    disabled = sum(1 for b in bugs if b.head_status == "Disabled payload removed")
    retired = sum(1 for b in bugs if b.head_status == "Retired subsystem removed")
    summary = [("Active", active, GREEN), ("Disabled", disabled, ORANGE), ("Retired", retired, GRAY)]
    x = 1180
    for label, count, color in summary:
        rounded(draw, (x, 36, x + 210, 116), 18, tuple(int(color[i : i + 2], 16) for i in (1, 3, 5)) + (45,), outline=tuple(int(color[i : i + 2], 16) for i in (1, 3, 5)) + (190,), width=2)
        draw.text((x + 22, 48), str(count), font=font(34, bold=True), fill=tuple(int(color[i : i + 2], 16) for i in (1, 3, 5)) + (255,))
        draw.text((x + 72, 60), label, font=font(22, bold=True), fill=(238, 242, 255, 255))
        x += 225

    cols = [58, 188, 620, 820, 1005, 1320]
    header_y = 155
    headers = ["ID", "Planned bug", "Tier", "Difficulty", "Current HEAD", "Evidence"]
    for x, h in zip(cols, headers):
        draw.text((x, header_y), h.upper(), font=font(17, bold=True), fill=(102, 227, 255, 255))
    y = 190
    row_h = 68
    for i, b in enumerate(bugs):
        fill = (18, 26, 49, 215) if i % 2 == 0 else (23, 33, 61, 215)
        rounded(draw, (42, y - 8, 1878, y + row_h - 12), 14, fill)
        status_color = BUG_STATUS_COLORS[b.head_status]
        diff_color = BUG_DIFFICULTY_COLORS[b.difficulty]
        draw.text((cols[0], y + 8), b.bug_id, font=font(22, bold=True), fill=(238, 242, 255, 255))
        draw.text((cols[1], y + 8), b.name, font=font(20, bold=True), fill=(238, 242, 255, 255))
        draw.text((cols[2], y + 10), b.tier, font=font(18), fill=(170, 180, 212, 255))
        rounded(draw, (cols[3], y + 3, cols[3] + 118, y + 36), 14, tuple(int(diff_color[j:j+2], 16) for j in (1,3,5)) + (60,), outline=tuple(int(diff_color[j:j+2], 16) for j in (1,3,5)) + (200,))
        draw.text((cols[3] + 15, y + 8), b.difficulty, font=font(16, bold=True), fill=(238, 242, 255, 255))
        reachable = audit[b.bug_id]
        mark = "✓" if reachable else "✕"
        rounded(draw, (cols[4], y + 3, cols[4] + 260, y + 36), 14, tuple(int(status_color[j:j+2], 16) for j in (1,3,5)) + (55,), outline=tuple(int(status_color[j:j+2], 16) for j in (1,3,5)) + (190,))
        draw.text((cols[4] + 14, y + 7), f"{mark} {b.head_status}", font=font(16, bold=True), fill=(238, 242, 255, 255))
        evidence = textwrap.shorten(b.evidence, width=62, placeholder="…")
        draw.text((cols[5], y + 4), evidence, font=font(16), fill=(170, 180, 212, 255))
        if b.patched_by:
            draw.text((cols[5], y + 30), f"Patch-away: {b.patched_by}", font=font(14, bold=True), fill=(255, 209, 102, 255))
        y += row_h

    draw.text((58, 1010), "Note: BUG-08's int16 arithmetic residue still exists, but the +30000 Heart pickup payload is gone, so it is not player-reachable in HEAD.", font=font(19), fill=(170, 180, 212, 255))
    path = OUT / "defcon_bug_roster_head_audit.png"
    img.save(path)
    return path


def make_bug_patch_timeline() -> Path:
    events = [
        (date(2026, 4, 21), "76a07d6", "Initial bug-spec draft", "BUG-01→BUG-11 concept chain appears in specs."),
        (date(2026, 4, 23), "0e84f09", "Server rewrite", "Core protocol/game-loop substrates land."),
        (date(2026, 4, 25), "041d346", "BUG-08 payload removed", "Heart pickup and +30000 heal path disappear."),
        (date(2026, 4, 25), "e720683", "BUG-09/10 debug path removed", "DebugItems, ParseItemCommand, !skip_room removed."),
        (date(2026, 4, 25), "92c32f5", "BUG-12 carrier added", "Shop + claimed_type pickup confusion implemented."),
        (date(2026, 4, 27), "6fec927", "Inventory deleted", "C2S_USE_ITEM + item validation removed; BUG-09/10 retired."),
        (date(2026, 4, 30), "HEAD", "Current audit", "9 active, 1 disabled payload, 2 retired subsystems."),
    ]
    W, H = 1920, 1080
    img = Image.new("RGB", (W, H), (11, 16, 32))
    draw = ImageDraw.Draw(img, "RGBA")
    draw.text((64, 50), "Bug lifecycle: the planned exploit suite evolved while shipping", font=font(48, bold=True), fill=(238, 242, 255, 255))
    draw.text((68, 108), "This is the slide for “we accidentally patched away some fun.”", font=font(25), fill=(170, 180, 212, 255))

    x0, x1 = 180, 1740
    y = 470
    draw.line((x0, y, x1, y), fill=(102, 227, 255, 190), width=5)
    start, end = events[0][0], events[-1][0]
    span = (end - start).days or 1
    for i, (d, commit, title, desc) in enumerate(events):
        x = x0 + int((d - start).days / span * (x1 - x0))
        color = GREEN
        if "removed" in title.lower() or "deleted" in title.lower():
            color = ORANGE
        if "retired" in desc.lower():
            color = GRAY
        if commit == "HEAD":
            color = ACCENT
        rgb = tuple(int(color[j:j+2], 16) for j in (1, 3, 5))
        draw.ellipse((x - 18, y - 18, x + 18, y + 18), fill=rgb + (255,), outline=(238, 242, 255, 255), width=3)
        up = i % 2 == 0
        box_y = y - 260 if up else y + 58
        rounded(draw, (x - 145, box_y, x + 145, box_y + 178), 20, (18, 26, 49, 230), outline=rgb + (180,), width=2)
        draw.text((x - 122, box_y + 18), d.strftime("%b %-d"), font=font(23, bold=True), fill=(255, 209, 102, 255))
        draw.text((x - 122, box_y + 50), commit, font=font(18, mono=True), fill=(102, 227, 255, 255))
        wrapped_title = textwrap.wrap(title, width=20)
        draw.text((x - 122, box_y + 80), "\n".join(wrapped_title[:2]), font=font(18, bold=True), fill=(238, 242, 255, 255))
        wrapped_desc = textwrap.wrap(desc, width=28)
        draw.text((x - 122, box_y + 122), "\n".join(wrapped_desc[:2]), font=font(14), fill=(170, 180, 212, 255))
        draw.line((x, y + (-18 if up else 18), x, box_y + (178 if up else 0)), fill=rgb + (120,), width=2)

    # Status legend.
    legend = [(GREEN, "substrate added/active"), (ORANGE, "payload/debug path removed"), (GRAY, "subsystem retired"), (ACCENT, "current audit")]
    lx = 72
    for color, label in legend:
        rgb = tuple(int(color[j:j+2], 16) for j in (1, 3, 5))
        draw.ellipse((lx, 930, lx + 22, 952), fill=rgb + (255,))
        draw.text((lx + 32, 926), label, font=font(20), fill=(238, 242, 255, 255))
        lx += 365
    path = OUT / "defcon_bug_patchaway_timeline.png"
    img.save(path)
    return path


def make_bug_synergy_network() -> Path:
    bugs = {b.bug_id: b for b in planned_bugs()}
    nodes = {
        "HONEST": (0.06, 0.52, "Honest\n~2:30", RED),
        "BUG-03": (0.19, 0.65, "BUG-03\nDoor skip", GREEN),
        "BUG-05": (0.31, 0.65, "BUG-05\nInteract", GREEN),
        "BUG-12": (0.43, 0.82, "BUG-12\nType confusion", GREEN),
        "BUG-04": (0.43, 0.50, "BUG-04\nSwap cancel", GREEN),
        "BUG-07": (0.55, 0.50, "BUG-07\nDamage batch", GREEN),
        "BUG-09": (0.67, 0.70, "BUG-09\nJSON smuggle\nretired", GRAY),
        "BUG-10": (0.79, 0.70, "BUG-10\nRune/byte\nretired", GRAY),
        "BUG-01": (0.67, 0.32, "BUG-01\nSeed routing", GREEN),
        "BUG-06": (0.79, 0.32, "BUG-06\nSettle race", GREEN),
        "BUG-08": (0.91, 0.50, "BUG-08\nHP overflow\ndisabled", ORANGE),
        "BUG-11": (0.91, 0.22, "BUG-11\nKill-log race", GREEN),
        "BUG-02": (0.91, 0.83, "BUG-02\nDrift stretch", GREEN),
    }
    edges = [
        ("HONEST", "BUG-03", "−15s"), ("BUG-03", "BUG-05", "pickup/door"), ("BUG-05", "BUG-04", "setup"),
        ("BUG-04", "BUG-07", "burst"), ("BUG-07", "BUG-01", "clusters"), ("BUG-01", "BUG-06", "predict spawns"),
        ("BUG-06", "BUG-08", "old path"), ("BUG-08", "BUG-11", "old path"), ("BUG-11", "BUG-02", "multiplier"),
        ("BUG-05", "BUG-12", "any pickup → coin"), ("BUG-12", "BUG-07", "fund stacks"),
        ("BUG-07", "BUG-09", "historical"), ("BUG-09", "BUG-10", "historical"), ("BUG-10", "BUG-01", "historical"),
        ("BUG-01", "BUG-07", "clustering"), ("BUG-07", "BUG-11", "multi-kill"), ("BUG-02", "BUG-03", "multiplies"), ("BUG-02", "BUG-06", "multiplies"),
    ]

    fig, ax = plt.subplots(figsize=(18, 10))
    ax.set_title("Exploit synergy map: green nodes still work; gray/orange nodes were patched out of reach")
    ax.set_xlim(0, 1)
    ax.set_ylim(0, 1)
    ax.axis("off")
    for a, b, label in edges:
        x1, y1, _, c1 = nodes[a]
        x2, y2, _, c2 = nodes[b]
        historical = c1 == GRAY or c2 == GRAY or c1 == ORANGE or c2 == ORANGE
        color = "#94a3b8" if historical else ACCENT
        alpha = 0.38 if historical else 0.85
        ax.annotate("", xy=(x2, y2), xytext=(x1, y1), arrowprops=dict(arrowstyle="->", lw=2.0, color=color, alpha=alpha, shrinkA=25, shrinkB=25))
        mx, my = (x1 + x2) / 2, (y1 + y2) / 2
        if label:
            ax.text(mx, my, label, fontsize=8, color=MUTED if historical else YELLOW, ha="center", va="center", bbox=dict(boxstyle="round,pad=0.2", fc=BG, ec="none", alpha=0.65))
    for key, (x, y, label, color) in nodes.items():
        rgb = color
        if key == "HONEST":
            fc = RED
        else:
            fc = color
        ax.scatter([x], [y], s=2100 if key != "HONEST" else 1800, c=fc, edgecolors=TEXT, linewidths=2.2, alpha=0.95)
        ax.text(x, y, label, ha="center", va="center", fontsize=10, color="#08111f" if color in {GREEN, YELLOW, ACCENT, ORANGE} else TEXT, fontweight="bold")
    ax.text(0.02, 0.03, "Patched-away nodes at HEAD: BUG-08 payload, BUG-09 inventory smuggling, BUG-10 item-name bypass.", transform=ax.transAxes, color=MUTED, fontsize=12)
    path = OUT / "defcon_bug_synergy_network.png"
    save_fig_slide(path)
    return path


def make_bug_code_audit() -> Path:
    bugs = planned_bugs()
    audit = audit_bug_presence()
    W, H = 1920, 1080
    img = Image.new("RGB", (W, H), (11, 16, 32))
    draw = ImageDraw.Draw(img, "RGBA")
    draw.text((64, 50), "HEAD code audit: which planned bugs still have a reachable substrate?", font=font(44, bold=True), fill=(238, 242, 255, 255))
    draw.text((68, 106), "This is a lightweight source-pattern audit, not an exploit proof. It is meant as a slide-friendly checklist.", font=font(22), fill=(170, 180, 212, 255))

    x0, y0 = 86, 185
    row_h = 66
    for i, b in enumerate(bugs):
        y = y0 + i * row_h
        reachable = audit[b.bug_id]
        if reachable:
            color = GREEN
            status = "reachable"
            icon = "✓"
        elif b.bug_id == "BUG-08":
            color = ORANGE
            status = "arithmetic remains, payload gone"
            icon = "!"
        else:
            color = GRAY
            status = "removed with subsystem"
            icon = "✕"
        rgb = tuple(int(color[j:j+2], 16) for j in (1,3,5))
        rounded(draw, (x0, y, x0 + 1748, y + 52), 16, (18, 26, 49, 225) if i % 2 == 0 else (23, 33, 61, 225))
        draw.ellipse((x0 + 18, y + 10, x0 + 50, y + 42), fill=rgb + (255,))
        draw.text((x0 + 29 - (4 if icon != "✓" else 7), y + 12), icon, font=font(20, bold=True), fill=(11, 16, 32, 255))
        draw.text((x0 + 72, y + 12), b.bug_id, font=font(21, bold=True), fill=(238, 242, 255, 255))
        draw.text((x0 + 185, y + 12), b.name, font=font(20, bold=True), fill=(238, 242, 255, 255))
        rounded(draw, (x0 + 670, y + 10, x0 + 940, y + 42), 14, rgb + (45,), outline=rgb + (185,))
        draw.text((x0 + 688, y + 15), status, font=font(15, bold=True), fill=(238, 242, 255, 255))
        ev = textwrap.shorten(b.evidence, width=78, placeholder="…")
        draw.text((x0 + 980, y + 14), ev, font=font(15), fill=(170, 180, 212, 255))

    path = OUT / "defcon_bug_code_audit_checklist.png"
    img.save(path)
    return path


def make_summary(commits: list[Commit], metrics: dict[str, object], paths: list[Path]) -> Path:
    per_day: Counter[date] = metrics["per_day"]  # type: ignore[assignment]
    authors: Counter[str] = metrics["authors"]  # type: ignore[assignment]
    peak_day, peak_count = per_day.most_common(1)[0]
    rolling_6h = metrics["rolling_6h"]  # type: ignore[assignment]
    rolling_24h = metrics["rolling_24h"]  # type: ignore[assignment]
    bugs = planned_bugs()
    active = [b for b in bugs if b.head_status == "Active in HEAD"]
    removed = [b for b in bugs if b.head_status != "Active in HEAD"]

    area_commits = Counter(primary_area(c) for c in commits)
    area_churn = Counter()
    for c in commits:
        for fs in c.files:
            area_churn[classify_path(fs.path)] += fs.churn

    lines = []
    lines.append("# DEFCON Dungeon — developer-insight slide stats\n")
    lines.append("Generated by `python3 tools/generate_developer_insights.py`. Commit timestamps are converted to Singapore time.\n")
    lines.append("## Copy/paste behind-the-scenes talking points\n")
    lines.append(f"- **{metrics['total']} commits** over **{metrics['calendar_days']} calendar days** ({metrics['start'].strftime('%b %-d')} → {metrics['end'].strftime('%b %-d, %Y')}).")
    lines.append(f"- The project was quiet for **{metrics['longest_gap'][0].days} days**, then became a late-April sprint.")
    lines.append(f"- **Peak day:** {peak_day.strftime('%b %-d')} with **{peak_count} commits**. Peak rolling window: **{rolling_24h[0]} commits in 24h**.")
    lines.append(f"- **Peak six-hour crunch:** **{rolling_6h[0]} commits** from {rolling_6h[1].strftime('%b %-d %H:%M')} to {rolling_6h[2].strftime('%H:%M')}. ")
    lines.append(f"- **After-hours:** {metrics['after_hours']} commits (**{safe_pct(metrics['after_hours'], metrics['total'])}**) landed before 09:00 or after 18:00; **{metrics['night']}** landed from 22:00–03:59.")
    lines.append(f"- **Weekend load:** {metrics['weekend']} commits (**{safe_pct(metrics['weekend'], metrics['total'])}**) landed on Saturday/Sunday.")
    lines.append(f"- **Churn:** **{metrics['insertions']:,} insertions**, **{metrics['deletions']:,} deletions**, **{metrics['churn']:,} text-line churn**, touching **{metrics['unique_files']} unique files**.")
    lines.append(f"- **Collaboration shape:** {len(authors)} Git author identities, **{metrics['merge_commits']} merge commits**, **{metrics['pr_merges']} PR merge commits**.")
    lines.append(f"- **Intentional bug suite:** **12 planned bugs** total; at HEAD, **{len(active)} active/reachable**, **1 disabled payload**, **2 retired with inventory removal**.")
    lines.append("- **Patched-away at HEAD:** BUG-08 (Heart/+30000 payload removed), BUG-09 (UseItem/JSON smuggling route removed), BUG-10 (item-name validation bypass removed with inventory).")

    lines.append("\n## Generated graphics\n")
    for p in paths:
        lines.append(f"- `{p.relative_to(ROOT)}`")

    lines.append("\n## Top author identities\n")
    lines.append("| Author identity | Commits |")
    lines.append("| --- | ---: |")
    for name, count in authors.most_common():
        lines.append(f"| {name} | {count} |")

    lines.append("\n## Busiest days\n")
    lines.append("| Date | Commits |")
    lines.append("| --- | ---: |")
    for d, count in per_day.most_common(10):
        lines.append(f"| {d.strftime('%Y-%m-%d %A')} | {count} |")

    lines.append("\n## Subsystem summary\n")
    lines.append("| Area | Primary commits | Text churn |")
    lines.append("| --- | ---: | ---: |")
    for area, count in area_commits.most_common():
        lines.append(f"| {area} | {count} | {area_churn[area]:,} |")

    lines.append("\n## Planned bug roster at HEAD\n")
    lines.append("| ID | Name | Tier | Difficulty | Current HEAD | Patch-away commit |")
    lines.append("| --- | --- | --- | --- | --- | --- |")
    for b in bugs:
        lines.append(f"| {b.bug_id} | {b.name} | {b.tier} | {b.difficulty} | {b.head_status} | {b.patched_by or '—'} |")

    path = OUT / "developer_insights_summary.md"
    path.write_text("\n".join(lines) + "\n", encoding="utf-8")
    return path


def main() -> None:
    OUT.mkdir(parents=True, exist_ok=True)
    setup_mpl()
    commits = parse_git_history()
    metrics = history_metrics(commits)
    paths = [
        make_dev_overview_cards(commits, metrics),
        make_commit_timeline(commits, metrics),
        make_commit_heatmap(commits, metrics),
        make_author_churn_chart(commits, metrics),
        make_subsystem_churn_chart(commits, metrics),
        make_burst_timeline(commits, metrics),
        make_calendar_heatmap(commits, metrics),
        make_bug_roster_matrix(commits),
        make_bug_patch_timeline(),
        make_bug_synergy_network(),
        make_bug_code_audit(),
    ]
    summary = make_summary(commits, metrics, paths)
    print("Generated developer insight assets:")
    for p in paths:
        print(f"  {p.relative_to(ROOT)}")
    print(f"  {summary.relative_to(ROOT)}")


if __name__ == "__main__":
    main()
