import { GameState } from "./GameState";
import {
  C2SType,
  type C2SEnvelope,
  type C2SDataFor,
  type S2CEnvelope,
} from "../../net/packet";

export class GameStateSync {
  state = new GameState();
  private ws?: WebSocket;
  private seq = 0;
  private connected = false;
  private queued: string[] = [];

  connect(url: string): Promise<void> {
    return new Promise<void>((resolve, reject) => {
      let settled = false;
      let ws: WebSocket;
      try {
        ws = new WebSocket(url);
      } catch (err) {
        reject(err);
        return;
      }
      this.ws = ws;

      ws.onopen = () => {
        this.connected = true;
        // Flush any messages that were queued while we were still connecting.
        if (this.queued.length > 0) {
          for (const msg of this.queued) {
            ws.send(msg);
          }
          this.queued = [];
        }
        if (!settled) {
          settled = true;
          resolve();
        }
      };

      ws.onerror = (ev) => {
        if (!settled) {
          settled = true;
          reject(ev);
        }
      };

      ws.onmessage = (ev: MessageEvent) => {
        this.onMessage(typeof ev.data === "string" ? ev.data : String(ev.data));
      };

      ws.onclose = () => {
        this.connected = false;
        const err = { code: "disconnected", message: "ws closed" };
        this.state.lastError = err;
        this.state.emit("error", err);
      };
    });
  }

  close(): void {
    if (this.ws) {
      try {
        this.ws.close();
      } catch {
        // ignore
      }
    }
    this.ws = undefined;
    this.connected = false;
    this.queued = [];
  }

  isConnected(): boolean {
    return this.connected && this.ws?.readyState === WebSocket.OPEN;
  }

  sendAuth(playerId: string): void {
    this.send(C2SType.AUTH, { player_id: playerId });
  }

  sendStartRun(startTime?: number): void {
    this.send(C2SType.START_RUN, { start_time: startTime ?? Date.now() });
  }

  sendInput(dx: number, dy: number): void {
    this.send(C2SType.INPUT, { dx, dy });
  }

  sendShoot(aimAngle: number): void {
    this.send(C2SType.SHOOT, { aim_angle: aimAngle });
  }

  sendSwitchWeapon(weaponId: number): void {
    this.send(C2SType.SWITCH_WEAPON, { weapon_id: weaponId });
  }

  sendInteract(targetId: number, claimedType: number): void {
    this.send(C2SType.INTERACT, {
      target_id: targetId,
      claimed_type: claimedType,
    });
  }

  sendShopPurchase(itemId: number): void {
    this.send(C2SType.SHOP_PURCHASE, { item_id: itemId });
  }

  sendEnterDoor(doorId: number): void {
    this.send(C2SType.ENTER_DOOR, { door_id: doorId });
  }

  sendUseItem(itemName: string): void {
    this.send(C2SType.USE_ITEM, { item_name: itemName });
  }

  private send<T extends C2SType>(type: T, data: C2SDataFor<T>): void {
    const envelope: C2SEnvelope<T> = {
      type,
      seq: ++this.seq,
      ts: Date.now(),
      data,
    };
    const payload = JSON.stringify(envelope);
    if (this.ws && this.ws.readyState === WebSocket.OPEN) {
      this.ws.send(payload);
    } else {
      this.queued.push(payload);
    }
  }

  private onMessage(raw: string): void {
    let env: S2CEnvelope;
    try {
      env = JSON.parse(raw) as S2CEnvelope;
    } catch {
      // Malformed frame — drop it. The server is authoritative; we just
      // wait for the next well-formed snapshot.
      return;
    }
    this.state.apply(env);
  }
}
