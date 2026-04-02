import { type Game } from "../scenes/Game";
import { type WeaponId, getWeaponStats } from "../weapons";

export interface GameStatePayload {
  x: number;
  y: number;
  health: number;
  currentRoom: number;
  kills: number;
  coins: number;
  ownedWeapons: string[];
  equippedWeapon: string;
  playerName: string;
  timestamp: number;
}

export interface ServerResponse {
  ok: boolean;
  message?: string;
  rollback?: GameStatePayload;
}

export default class GameStateSync {
  private ws: WebSocket | null = null;
  private url: string;
  private intervalMs: number;
  private timer: number | null = null;
  private game: Game;
  private connected = false;
  private stopped = false;
  private failCount = 0;

  constructor(game: Game, url: string, intervalMs: number) {
    this.game = game;
    this.url = url;
    this.intervalMs = intervalMs;
  }

  start() {
    this.stopped = false;
    this.connect();
    this.timer = window.setInterval(() => this.sendState(), this.intervalMs);
  }

  stop() {
    this.stopped = true;
    if (this.timer !== null) {
      window.clearInterval(this.timer);
      this.timer = null;
    }
    if (this.ws) {
      this.ws.close(1000, "client shutdown");
      this.ws = null;
    }
    this.connected = false;
  }

  setIntervalMs(ms: number) {
    this.intervalMs = ms;
    if (this.timer !== null) {
      window.clearInterval(this.timer);
      this.timer = window.setInterval(() => this.sendState(), this.intervalMs);
    }
  }

  private connect() {
    if (this.ws || this.stopped) return;

    this.ws = new WebSocket(this.url);

    this.ws.onopen = () => {
      this.connected = true;
      this.failCount = 0;
      this.game.events.emit("ws-status", true);
      console.log("[sync] connected to server");
    };

    this.ws.onmessage = (ev) => {
      try {
        const res: ServerResponse = JSON.parse(ev.data);
        if (!res.ok) {
          console.warn(`[sync] server violation: ${res.message}`);
          if (res.rollback) {
            this.applyRollback(res.rollback);
          }
        }
      } catch {
        console.warn("[sync] bad server message", ev.data);
      }
    };

    this.ws.onclose = () => {
      this.connected = false;
      this.ws = null;
      if (this.stopped) return;
      this.failCount++;
      this.game.events.emit("ws-status", false, this.failCount);
      console.log("[sync] disconnected, retrying in 3s");
      setTimeout(() => this.connect(), 3000);
    };

    this.ws.onerror = () => {
      this.ws?.close();
    };
  }

  private applyRollback(rb: GameStatePayload) {
    const p = this.game.player;
    if (!p?.active) return;

    console.warn(`[sync] rolling back to pos=(${rb.x}, ${rb.y}) room=${rb.currentRoom}`);
    p.setPosition(rb.x, rb.y);
    p.setVelocity(0, 0);
    p.health = rb.health;
    p.coins = rb.coins;

    if (rb.ownedWeapons) {
      p.ownedWeaponIds = rb.ownedWeapons as WeaponId[];
    }
    if (rb.equippedWeapon) {
      p.equippedWeaponId = rb.equippedWeapon as WeaponId;
      this.game.events.emit(
        "weapon-changed",
        getWeaponStats(p.equippedWeaponId).displayName,
      );
    }

    this.game.events.emit("player-health-changed", p.health, p.maxHealth);
    this.game.events.emit("coin-count-changed", p.coins);
  }

  sendStateNow() {
    this.sendState();
  }

  private sendState() {
    if (!this.connected || !this.ws || this.ws.readyState !== WebSocket.OPEN) return;

    const p = this.game.player;
    if (!p) return;

    const payload: GameStatePayload = {
      x: Math.round(p.x * 100) / 100,
      y: Math.round(p.y * 100) / 100,
      health: p.health,
      currentRoom: p.progress.currentRoom,
      kills: this.game.getKillCount(),
      coins: p.coins,
      ownedWeapons: [...p.ownedWeaponIds],
      equippedWeapon: p.equippedWeaponId,
      playerName: this.game.playerName,
      timestamp: Date.now(),
    };

    this.ws.send(JSON.stringify(payload));
  }
}
