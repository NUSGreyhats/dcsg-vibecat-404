# Feature — Combat & Weapons

## Purpose

Weapons, shooting, cooldown tracking, projectile collision, damage application, and HP arithmetic. Hosts three of the four "Language tier" intentional bugs (BUG-04, BUG-07, BUG-08) because all three are artifacts of how the damage pipeline composes state within a single tick.

## Data model

### Weapon definition (authoritative on server)

```go
type WeaponSpec struct {
    Damage     int16   // int16 to compose with int16 HP arithmetic
    CooldownMs int
    Name       string
}

var weaponSpecs = map[uint8]WeaponSpec{
    1: {Damage: 1, CooldownMs: 150, Name: "pistol"},
    2: {Damage: 2, CooldownMs: 220, Name: "rifle"},
    3: {Damage: 6, CooldownMs: 520, Name: "shotgun"},
}
```

Client mirror: `client/src/game/weapons.ts`. Must stay in sync with server.

### Weapon state (per player)

Part of `PlayerState` — see [../foundations/06-game-state-conventions.md](../foundations/06-game-state-conventions.md):

```go
CurrentWeapon   uint8
UnlockedWeapons []uint8
WeaponCooldown  map[uint8]time.Time   // next-allowed-fire per weapon
SwapCooldownEnd time.Time             // when the post-swap cooldown expires
```

Ammo tracking is per-weapon, keyed alongside `WeaponCooldown` in the same map or a parallel `WeaponAmmo map[uint8]int` (TBD — not currently in spec).

### Projectile state

Lives in `RoomState.Projectiles map[uint32]*ProjectileState`:

```go
ProjectileState {
    ProjID       uint32
    X, Y         float32
    VelX, VelY   float32
    Damage       int16
    OwnerID      uint32   // 0 = player
}
```

## Data flow — a single tick

```
drain inCh → C2S_SWITCH_WEAPON / C2S_SHOOT handlers process in arrival order
    │
    ▼
physics step: advance each projectile by VelX*dt, VelY*dt
    │
    ▼
collision pass:
    for each projectile:
        for each enemy:
            if collides: enemy.HP -= effectiveDmg(projectile.Damage, enemy.armor)
    *** all collisions resolve before any death check ***
    │
    ▼
death pass: remove enemies with HP <= 0, spawn cleanup goroutines
    │
    ▼
send S2C_STATE
```

### `C2S_SHOOT` handler (the same-tick trap)

```go
func handleShoot(p *PlayerState, pkt ShootPacket, now time.Time) {
    cd := p.WeaponCooldown[p.CurrentWeapon]
    if now.Before(cd) { return }
    if now.Before(p.SwapCooldownEnd) { return }
    // spawn projectile, charge cooldown
    p.WeaponCooldown[p.CurrentWeapon] = now.Add(weaponSpecs[p.CurrentWeapon].CooldownMs * time.Millisecond)
}
```

`now` is captured **once at tick start** and reused for every drained packet — so two `C2S_SHOOT` packets drained in the same tick see the same `now`, and the first one's cooldown update lands at `now + CooldownMs` (in the future) so the second also passes the `now.Before(cd)` check if the planning is right. This is [BUG-07](#bug-07-damage-batch-overflow).

### `C2S_SWITCH_WEAPON` handler

```go
func handleSwitch(p *PlayerState, pkt SwitchPacket, now time.Time) {
    if !contains(p.UnlockedWeapons, pkt.WeaponID) { return }
    p.CurrentWeapon = pkt.WeaponID
    p.SwapCooldownEnd = now.Add(SwapCooldownMs * time.Millisecond)
}
```

When `C2S_SWITCH_WEAPON` and `C2S_SHOOT` land in the same tick (swap first, then shoot), the shoot handler reads `SwapCooldownEnd = now + SwapCooldownMs` and the check `now.Before(SwapCooldownEnd)` ... is a question of `now.Before(now + SwapCooldownMs)` which is true — the shoot is blocked.

UNLESS the order or the cooldown value is set such that the check passes. The actual bug: the cooldown check is against the *pre-switch* `SwapCooldownEnd` (zero value, or the previous switch's expiry). The switch handler sets `SwapCooldownEnd` on the same tick, but the shoot handler already ran its check against the old value. Implementation must be: both checks read from state in a non-obvious order. See [BUG-04 body](#bug-04-swap-cancel) for the precise sequence.

### Damage / HP arithmetic

```go
func (p *PlayerState) ApplyHeal(amount int16) {
    p.HP += amount  // no overflow check — Go wraps silently
    if p.HP > p.MaxHP {
        p.HP = p.MaxHP
    }
}

func (e *EnemyState) TakeDamage(amount int16) {
    reduction := e.computeArmor()          // returns int16
    effectiveDmg := amount - reduction     // can underflow if armor > damage
    e.HP -= effectiveDmg                   // subtracting negative = adding HP... unless it wraps
}

// Enemy death — part of the post-collision death pass.
if e.HP <= 0 {
    e.AIState = AIDead
}

// Player death — separate tick pass, runs after enemy-death pass.
// See ../foundations/05-server-runtime.md#player-death-pass.
if p.HP <= 0 {
    finishRun(now, elapsedMs, RunFailed)
}
```

Both `PlayerState.HP` and `EnemyState.HP` are `int16`. Go performs silent wraparound. The clamp `if HP > MaxHP` doesn't catch negative values from a wrap. See [BUG-08](#bug-08-hp-integer-overflow-healing-past-max).

## Client interactions

- `client/src/game/weapons.ts` — mirrors `weaponSpecs` for UI (weapon icons, cooldown bars).
- `client/src/game/entities/Bullet.ts` — renders projectiles from `ProjSnapshot`.
- Fire input → `C2S_SHOOT { aim_angle }`. No client-side cooldown enforcement beyond visual polish; the server is authoritative.

## Implementation notes

- Package: `internal/game/combat` (target).
- `now` captured at tick entry as `tickNow := time.Now()` and passed into every handler invoked during drain. This is load-bearing for BUG-04 + BUG-07 — do not replace with per-handler `time.Now()` calls.
- Armor computation in `EnemyState.computeArmor()` returns `int16`. Debuffs can drive armor below zero; this is the ingress point for the BUG-08 double-overflow exploit.

## Implementation status

Current `server/main.go`:

- `weaponSpecs` defined for pistol/rifle/shotgun with `Damage int` (not `int16`) and `CooldownMs int`.
- No projectile state; damage is inferred from enemy HP deltas reported by the client.
- No `WeaponCooldown` map — current model is client-side.
- No `int16` HP — the current model uses `int` throughout.

Migration to server-authoritative combat with `int16` HP + same-tick batching is later work. The bugs below assume that migration has happened.

## Intentional bugs in this area

### BUG-04 Swap Cancel

**Design choice enabling the bug:** `SWITCH_WEAPON` and `SHOOT` packets drained in the same tick read cooldown state with a shared `tickNow`, and the shoot handler's check runs against `SwapCooldownEnd` as seen *before* the switch handler's write (or as seen with the same `tickNow` such that the cooldown math is ~0 ms).

**Root cause sketch:**

```go
// within one tick drain:
for pkt := range drainedPackets {
    switch pkt.Type {
    case C2S_SHOOT:    handleShoot(p, pkt.Data, tickNow)
    case C2S_SWITCH:   handleSwitch(p, pkt.Data, tickNow)
    }
}
```

When the client sends `SHOOT` then `SWITCH` in the same tick, or vice versa, the cooldown check sees a stale or equal timestamp and allows the shot. The exact ordering and what fires first is the exploit surface.

**Exploit path:** Send `C2S_SWITCH_WEAPON` + `C2S_SHOOT` back-to-back so both land in the same 50 ms drain window. Fire instantly after swap — the usual inter-weapon cooldown bar is skipped.

**Bound:** Only applies when drained in the same tick. Between-tick switches incur full cooldown.

**Detection:** Replay reproduces identically. Do NOT replace the shared `tickNow` capture with per-handler `time.Now()`.

**Synergies:** Makes burst-weapon chains (pistol → shotgun → pistol) possible mid-combat. Pairs with [BUG-07](#bug-07-damage-batch-overflow) for even higher burst.

**Savings:** ~1–2 s per room.

---

### BUG-07 Damage Batch Overflow

**Design choice enabling the bug:**

1. All projectile↔enemy collisions resolve **before any death check**.
2. The fire-rate limiter shares a single `tickNow` across all packets drained that tick.

**Root cause sketch:**

Multiple `C2S_SHOOT` packets arrive in the same 50 ms window → drained together → each runs `handleShoot(p, pkt, tickNow)`. Because `now.Before(cd)` is evaluated against the cooldown set by the *previous* shot (which pushed `cd` into the future by `CooldownMs`), the naive implementation might allow subsequent shots if `CooldownMs` is short enough that... wait, that doesn't work — cooldown pushes forward.

The actual same-tick bypass: the handler reads `cd`, decides to pass, and writes `cd = tickNow + CooldownMs`. The **next** shoot packet reads `cd` which is now in the future of `tickNow`, so it's blocked. BUT: if the client reorders its packet pacing so that multiple `C2S_SHOOT` packets are paced to arrive in different ticks minus a trick — actually, the simplest form is this: the damage resolution pass runs **all collisions** before **any death** check. So 2–3 shots fired on consecutive ticks (legal) all score damage against the same enemy within the same collision pass because nothing has been marked dead. Even when the enemy's HP would have hit zero on shot 2, shot 3's damage still applies because the death pass is after the whole collision batch.

For enemies with HP slightly above the single-shot damage, this effectively doubles DPS: overkill damage from the second shot is "wasted" in the bookkeeping but the enemy is removed in the death pass either way. Practically this matters when the player can concentrate fire such that multiple projectiles collide in the same tick — e.g. shotgun pellets (one `C2S_SHOOT` spawning N projectiles) against a line of enemies, or close-range chain shots.

**Exploit path:** Queue 2–3 shots per tick (close-range bursts with a fast weapon, or shotgun spread) → all damage applies in one collision pass → effective DPS doubles against clumped enemies.

**Bound:** Limited by projectile spawn rate and how many collisions can fit in one tick. Not unbounded.

**Detection:** Replay is deterministic. Do NOT split the collision pass into per-projectile death checks — that "fixes" the bug.

**Synergies:** Pairs with [BUG-04](#bug-04-swap-cancel) for even higher burst. Triggers [BUG-11](../foundations/05-server-runtime.md#bug-11-kill-log-append-race) when multi-kills happen on the same tick.

**Savings:** ~2–3 s per room.

---

### BUG-08 HP Integer Overflow (Healing Past Max)

**Source:** [100 Go Mistakes #18 — Neglecting integer overflows](https://100go.co/#neglecting-integer-overflows-18). Go performs **silent integer wraparound** on overflow — no panic, no error.

**Design choice enabling the bug:**

1. Player and enemy HP are `int16` (max 32,767).
2. `ApplyHeal` has no overflow check — `p.HP += amount` wraps silently.
3. Armor reduction is `effectiveDmg := amount - reduction` with no clamp — can underflow or wrap.
4. A specific high-value healing pickup exists (`+30000 HP`) in the boss room.

**Root cause sketch:**

```go
func (p *PlayerState) ApplyHeal(amount int16) {
    p.HP += amount  // no overflow check — Go wraps silently
    if p.HP > p.MaxHP {
        p.HP = p.MaxHP
    }
}
```

If `p.HP` is high and `amount` is large, `p.HP += amount` wraps past `32767` to a **large negative** number. The `> MaxHP` clamp never fires (negative is not > MaxHP). The player now has negative HP. The per-tick **player-death pass** (signed `HP <= 0` on `int16`) catches the wrap and transitions `Status → Failed`. See [../foundations/05-server-runtime.md#player-death-pass](../foundations/05-server-runtime.md#player-death-pass).

**Twist:** the Heart pickup applies `ApplyHeal(+30000)` during `C2S_INTERACT` drain. If `HP > 2767` at pickup, the `int16` add wraps `32768 → -32768`, the `MaxHP` clamp misses (negative is not `> MaxHP`), and the player-death pass later in the same tick fails the run. Savvy players avoid the Heart unless `HP ≤ 2767`.

For the enemy side:

```go
func (e *EnemyState) TakeDamage(amount int16) {
    reduction := e.computeArmor()          // returns int16
    effectiveDmg := amount - reduction     // can underflow if armor > damage
    e.HP -= effectiveDmg                   // subtracting negative = adding HP... unless it wraps
}
```

If `amount = 10` and `reduction = 50`, `effectiveDmg = -40`. `e.HP -= (-40)` → `e.HP += 40` (enemy heals). If a crafted scenario pushes the arithmetic further: `amount = 100, reduction = -32700` (achievable via a debuff stack that corrupts armor to large negative), `effectiveDmg = 100 - (-32700) = 32800` → wraps to `-32736` as int16. `e.HP -= (-32736)` → enemy HP increases by 32736... and if that overflows *enemy* `int16 HP` → wraps negative → **enemy dies**.

**Exploit path:** Stack a specific debuff on the boss that corrupts its armor value to a large negative `int16`, then hit it with a normal attack. The damage arithmetic chain double-overflows, instantly killing the boss.

**Bound:**
- Requires finding the specific debuff + attack combination.
- Only works on the boss (regular enemies die fast enough that the setup isn't worth it).
- Player must avoid accidentally killing self with the `+30000` heal pickup.

**Detection:** Replay reproduces identically. Do NOT change HP to a wider integer type — that fixes the bug for both players (safer) but also removes the intended skill-ceiling mechanic. Do NOT collapse the player-death pass into the enemy-death pass — they read different state (post-drain heals vs. post-collision enemy HP) and sharing the pass would either miss the BUG-08 wrap or force a second collision walk.

**Synergies:** Faster setup via [BUG-04 swap cancel](#bug-04-swap-cancel). Enabled faster by [BUG-09 `instakill`](../foundations/02-protocol.md#bug-09-json-case-insensitive-field-smuggling) for adds cleanup before the boss.

**Savings:** ~5–10 s on boss fight.

## Packets used

- `C2S_SHOOT`, `C2S_SWITCH_WEAPON` (see [../foundations/02-protocol.md](../foundations/02-protocol.md)).
- `S2C_STATE` (snapshots projectiles + enemy HP).
- `S2C_ENTITY_DIED` (enemy kill notifications).

## Cross-references

- [../foundations/02-protocol.md](../foundations/02-protocol.md) — packet envelope + types.
- [../foundations/05-server-runtime.md](../foundations/05-server-runtime.md) — tick drain + tick-shared `now`.
- [../foundations/06-game-state-conventions.md](../foundations/06-game-state-conventions.md) — `WeaponCooldown`, `PlayerState.HP int16`, `EnemyState`.
- [../client/05-entities-rendering.md](../client/05-entities-rendering.md) — `Bullet` rendering + `client/src/game/weapons.ts` mirror.
- [../bugs.md](../bugs.md) — central index.
