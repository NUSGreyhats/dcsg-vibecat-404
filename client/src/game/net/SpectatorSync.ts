import { GameState } from "./GameState";
import type { S2CEnvelope } from "../../net/packet";

/**
 * Read-only sibling of GameStateSync. Owns its own GameState (the same
 * class — apply() is exhaustive on S2CType so spectator and player render
 * identically) and connects to the game server's /ws/spectate endpoint.
 *
 * Deliberately has no send* methods. The spectator client must never
 * emit a frame; if anything tries, it doesn't compile. Any inbound frame
 * the server sees from a spectator is treated as a programming error
 * and the connection is closed with SPECTATOR_READ_ONLY.
 *
 * See docs/specs/client/09-spectator-mode.md.
 */
export class SpectatorSync {
  state = new GameState();
  private ws?: WebSocket;
  private connected = false;

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
        // Don't synthesize an error here — runComplete from the server is
        // the canonical "watch ended" signal, and a clean post-grace close
        // looks identical to a network drop at this layer.
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
