import type { Game } from "../scenes/Game";
import { type WeaponId, getWeaponStats } from "../weapons";

export interface EnemyStateData {
  id: number;
  x: number;
  y: number;
  hp: number;
  type: string;
}

export interface GameStateJSON {
  x: number;
  y: number;
  health: number;
  currentRoom: number;
  kills: number;
  coins: number;
  ownedWeapons: string[];
  equippedWeapon: string;
  playerName: string;
  enemies: EnemyStateData[];
  timestamp: number;
}

export class GameState {
  x: number;
  y: number;
  health: number;
  currentRoom: number;
  kills: number;
  coins: number;
  ownedWeapons: string[];
  equippedWeapon: string;
  playerName: string;
  enemies: EnemyStateData[];
  timestamp: number;

  constructor(data: GameStateJSON) {
    this.x = data.x;
    this.y = data.y;
    this.health = data.health;
    this.currentRoom = data.currentRoom;
    this.kills = data.kills;
    this.coins = data.coins;
    this.ownedWeapons = [...data.ownedWeapons];
    this.equippedWeapon = data.equippedWeapon;
    this.playerName = data.playerName;
    this.enemies = [...data.enemies];
    this.timestamp = data.timestamp;
  }

  static fromGame(game: Game): GameState {
    const p = game.player;
    return new GameState({
      x: Math.round(p.x * 100) / 100,
      y: Math.round(p.y * 100) / 100,
      health: p.health,
      currentRoom: p.progress.currentRoom,
      kills: game.getKillCount(),
      coins: p.coins,
      ownedWeapons: [...p.ownedWeaponIds],
      equippedWeapon: p.equippedWeaponId,
      playerName: game.playerName,
      enemies: game.getEnemyStates(),
      timestamp: Date.now(),
    });
  }

  static fromJSON(data: GameStateJSON): GameState {
    return new GameState(data);
  }

  toJSON(): GameStateJSON {
    return {
      x: this.x,
      y: this.y,
      health: this.health,
      currentRoom: this.currentRoom,
      kills: this.kills,
      coins: this.coins,
      ownedWeapons: [...this.ownedWeapons],
      equippedWeapon: this.equippedWeapon,
      playerName: this.playerName,
      enemies: [...this.enemies],
      timestamp: this.timestamp,
    };
  }

  applyToGame(game: Game) {
    if (!game.player?.active) return;

    game.applyRollbackState(this.currentRoom, this.kills, this.enemies);

    const p = game.player;
    p.setPosition(this.x, this.y);
    p.setVelocity(0, 0);
    p.health = this.health;
    p.coins = this.coins;
    p.ownedWeaponIds = this.ownedWeapons as WeaponId[];
    p.equippedWeaponId = this.equippedWeapon as WeaponId;
    game.playerName = this.playerName || game.playerName;

    game.events.emit(
      "weapon-changed",
      getWeaponStats(p.equippedWeaponId).displayName,
    );
    game.events.emit("player-health-changed", p.health, p.maxHealth);
    game.events.emit("coin-count-changed", p.coins);
  }
}
