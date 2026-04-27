import { GameState } from "./GameState";
import {
  C2SType,
  type ReplayControlAction,
  type S2CEnvelope,
} from "../../net/packet";

/**
 * Read-mostly sibling of SpectatorSync. Owns its own GameState (the
 * same class — apply() is exhaustive on S2CType so player, spectator,
 * and replay viewers render identically) and connects to the game
 * server's /ws/replay endpoint.
 *
 * The ONLY outbound type is C2S_REPLAY_CONTROL (sendControl). Every
 * other C2S type is intentionally absent — adding one is a lint-visible
 * change and the server would close the WS with REPLAY_READ_ONLY anyway.
 *
 * See docs/specs/client/11-replay-viewer.md.
 */
export class ReplaySync {
  state = new GameState();
  private ws?: WebSocket;
  private connected = false;
  private nextSeq = 1;

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
        // Don't synthesize an error here — RUN_COMPLETE from the server
        // is the canonical "replay ended" signal, and a clean post-grace
        // close looks identical to a network drop at this layer.
        this.state.emit("disconnect");
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
  }

  isConnected(): boolean {
    return this.connected && this.ws?.readyState === WebSocket.OPEN;
  }

  /**
   * Send a replay control packet. The server is authoritative for the
   * playback state; this method is fire-and-forget, the next S2C_STATE
   * is the visible confirmation. `set_speed` requires speed; the others
   * ignore it.
   */
  sendControl(action: ReplayControlAction, speed?: number): void {
    if (!this.isConnected()) return;
    const data: { action: ReplayControlAction; speed?: number } = { action };
    if (action === "set_speed" && typeof speed === "number") {
      data.speed = speed;
    }
    const env = {
      type: C2SType.REPLAY_CONTROL,
      seq: this.nextSeq++,
      ts: Date.now(),
      data,
    };
    try {
      this.ws?.send(JSON.stringify(env));
    } catch {
      // The next isConnected() check will surface the failure.
    }
  }

  private onMessage(raw: string): void {
    let env: S2CEnvelope;
    try {
      env = JSON.parse(raw) as S2CEnvelope;
    } catch {
      return;
    }
    this.state.apply(env);
  }
}
