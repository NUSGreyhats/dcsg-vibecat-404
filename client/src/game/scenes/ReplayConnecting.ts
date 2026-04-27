import Phaser from "phaser";
import { getReplaySync, resetReplaySync, setActiveState } from "../net/runContext";
import {
  CANVAS_BG,
  FONT_FAMILY,
  FONT_SIZE_BODY,
  FONT_SIZE_SECTION,
  TEXT_MUTED,
  TEXT_PRIMARY,
  hex,
} from "../ui/tokens";

type ReplayMeta = {
  replay_id: string;
  display_name: string;
  total_ms: number;
  seed: number;
  splits: { room_index: number; time_ms: number; kills: number }[];
  submitted_at: string;
};

/**
 * Bridges from page load → admin meta fetch → admin ticket fetch →
 * /ws/replay open → first S2C_STATE → ReplayGame transition.
 *
 * On 401 from the meta or ticket fetch, redirects to /admin/login —
 * the admin session has expired since the page loaded.
 *
 * On any other failure, transitions to ReplayEnd with a human-readable
 * reason. The data layer is the gate: a non-admin who somehow lands on
 * the /admin/replay/:id/view URL gets the meta 401 and is bounced to
 * login before any replay data renders.
 */
export class ReplayConnecting extends Phaser.Scene {
  private statusText?: Phaser.GameObjects.Text;
  private titleText?: Phaser.GameObjects.Text;
  private replayId = "";
  private meta?: ReplayMeta;
  private transitioned = false;

  constructor() {
    super({ key: "ReplayConnecting" });
  }

  init(data: { replayId?: string; meta?: ReplayMeta }): void {
    const regId = this.registry.get("replayId") as string | undefined;
    this.replayId = data.replayId ?? regId ?? "";
    // Meta is preserved across Restart so the connecting screen can
    // skip the meta fetch on subsequent passes; the ticket is still
    // refreshed every time.
    this.meta = data.meta ?? this.meta;
    this.transitioned = false;
  }

  create(): void {
    // Always start with a fresh sync — Restart re-enters this scene
    // and we don't want to re-use a closed WebSocket from the prior pass.
    resetReplaySync();

    this.cameras.main.setBackgroundColor(CANVAS_BG);
    const cx = this.scale.width / 2;
    const cy = this.scale.height / 2;

    this.add
      .text(cx, cy - 40, "Replay", {
        fontFamily: FONT_FAMILY,
        fontSize: FONT_SIZE_SECTION,
        color: hex(TEXT_MUTED),
      })
      .setOrigin(0.5);

    this.titleText = this.add
      .text(cx, cy, "Loading…", {
        fontFamily: FONT_FAMILY,
        fontSize: FONT_SIZE_SECTION,
        color: hex(TEXT_PRIMARY),
      })
      .setOrigin(0.5);

    this.statusText = this.add
      .text(cx, cy + 40, "Fetching metadata…", {
        fontFamily: FONT_FAMILY,
        fontSize: FONT_SIZE_BODY,
        color: hex(TEXT_MUTED),
      })
      .setOrigin(0.5);

    if (!this.replayId) {
      this.endWithError("missing replay_id in URL");
      return;
    }

    void this.handshake();
  }

  private async handshake(): Promise<void> {
    if (!this.meta) {
      const meta = await this.fetchMeta();
      if (!meta) return;
      this.meta = meta;
      this.titleText?.setText(`Watching ${meta.display_name}`);
    } else {
      this.titleText?.setText(`Watching ${this.meta.display_name}`);
    }

    this.statusText?.setText("Minting ticket…");
    const ticket = await this.fetchTicket();
    if (!ticket) return;

    this.statusText?.setText("Connecting…");
    const sync = getReplaySync();
    setActiveState(sync.state);

    const onState = () => {
      if (this.transitioned) return;
      this.transitioned = true;
      sync.state.off("state", onState);
      sync.state.off("error", onError);
      sync.state.off("disconnect", onDisconnect);
      this.scene.start("ReplayGame", {
        meta: this.meta,
      });
      this.scene.launch("GameHUD");
      this.scene.launch("ReplayControls");
    };
    const onError = (err: { code: string; message: string }) => {
      if (this.transitioned) return;
      sync.state.off("state", onState);
      sync.state.off("error", onError);
      sync.state.off("disconnect", onDisconnect);
      this.endWithError(`${err.code}: ${err.message}`);
    };
    const onDisconnect = () => {
      if (this.transitioned) return;
      sync.state.off("state", onState);
      sync.state.off("error", onError);
      sync.state.off("disconnect", onDisconnect);
      this.endWithError("Connection lost");
    };

    sync.state.on("state", onState);
    sync.state.on("error", onError);
    sync.state.on("disconnect", onDisconnect);

    const wsProto = window.location.protocol === "https:" ? "wss:" : "ws:";
    const url =
      `${wsProto}//${window.location.host}/ws/replay` +
      `?replay_id=${encodeURIComponent(this.replayId)}` +
      `&ticket=${encodeURIComponent(ticket)}`;
    sync.connect(url).catch(() => {
      // The 'disconnect' handler above will route the failure.
    });
  }

  private async fetchMeta(): Promise<ReplayMeta | undefined> {
    try {
      const res = await fetch(`/admin/replay/${encodeURIComponent(this.replayId)}/meta`, {
        method: "GET",
        credentials: "same-origin",
      });
      if (res.status === 401) {
        this.redirectToLogin();
        return undefined;
      }
      if (res.status === 404) {
        this.endWithError("Replay not found");
        return undefined;
      }
      if (!res.ok) {
        this.endWithError(
          res.status === 500 ? "Replay integrity check failed" : `meta ${res.status}`,
        );
        return undefined;
      }
      return (await res.json()) as ReplayMeta;
    } catch (err) {
      this.endWithError(`meta fetch failed: ${(err as Error).message}`);
      return undefined;
    }
  }

  private async fetchTicket(): Promise<string | undefined> {
    try {
      const res = await fetch(`/admin/replay/${encodeURIComponent(this.replayId)}/ticket`, {
        method: "POST",
        credentials: "same-origin",
      });
      if (res.status === 401) {
        this.redirectToLogin();
        return undefined;
      }
      if (!res.ok) {
        this.endWithError(`ticket ${res.status}`);
        return undefined;
      }
      const body = (await res.json()) as { ticket: string };
      return body.ticket;
    } catch (err) {
      this.endWithError(`ticket fetch failed: ${(err as Error).message}`);
      return undefined;
    }
  }

  private redirectToLogin(): void {
    if (this.transitioned) return;
    this.transitioned = true;
    const ret = encodeURIComponent(window.location.pathname);
    window.location.href = `/admin/login?return=${ret}`;
  }

  private endWithError(message: string): void {
    if (this.transitioned) return;
    this.transitioned = true;
    this.scene.start("ReplayEnd", {
      meta: this.meta,
      reason: message,
    });
  }
}

export default ReplayConnecting;
