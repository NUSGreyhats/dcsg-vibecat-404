import Phaser from "phaser";
import { getSpectatorSync, setActiveState } from "../net/runContext";
import {
  CANVAS_BG,
  FONT_FAMILY,
  FONT_SIZE_BODY,
  FONT_SIZE_SECTION,
  TEXT_MUTED,
  TEXT_PRIMARY,
  hex,
} from "../ui/tokens";

const RETRY_AFTER_DEFAULT_MS = 5000;
const RETRY_MAX_ATTEMPTS = 3;

/**
 * Bridges from page load to a renderable spectator stream. Attempts to
 * connect, waits for the server's catchup sequence (RUN_STARTED → ROOM_LOAD
 * → STATE), then transitions to SpectateGame + GameHUD.
 *
 * Failure modes:
 *  - 503 capacity reached → retry up to RETRY_MAX_ATTEMPTS, honouring
 *    Retry-After if the page-level fetch picked it up. The WS API itself
 *    doesn't expose response headers on upgrade failure, so we treat any
 *    immediate close as a 503-equivalent and back off.
 *  - WS error / immediate close → SpectateEnd with the reason.
 *  - Server closes mid-handshake → SpectateEnd "player disconnected".
 */
export class SpectateConnecting extends Phaser.Scene {
  private statusText?: Phaser.GameObjects.Text;
  private playerId = "";
  private displayName = "";
  private attempt = 0;
  private retryTimer?: Phaser.Time.TimerEvent;
  private transitioned = false;

  constructor() {
    super({ key: "SpectateConnecting" });
  }

  init(data: { playerId?: string; displayName?: string }): void {
    // Init data is preferred when present (e.g. when re-entering the
    // scene from SpectateEnd), but on the initial Preloader → here
    // transition there's none, so we fall back to the registry that the
    // boot script populates.
    const regId = this.registry.get("spectatorPlayerId") as string | undefined;
    const regName = this.registry.get("spectatorDisplayName") as string | undefined;
    this.playerId = data.playerId ?? regId ?? "";
    this.displayName = data.displayName || regName || this.playerId;
    this.attempt = 0;
    this.transitioned = false;
  }

  create(): void {
    this.cameras.main.setBackgroundColor(CANVAS_BG);
    const cx = this.scale.width / 2;
    const cy = this.scale.height / 2;

    this.add
      .text(cx, cy - 40, "Spectator", {
        fontFamily: FONT_FAMILY,
        fontSize: FONT_SIZE_SECTION,
        color: hex(TEXT_MUTED),
      })
      .setOrigin(0.5);

    this.add
      .text(cx, cy, `Watching ${this.displayName}…`, {
        fontFamily: FONT_FAMILY,
        fontSize: FONT_SIZE_SECTION,
        color: hex(TEXT_PRIMARY),
      })
      .setOrigin(0.5);

    this.statusText = this.add
      .text(cx, cy + 40, "Connecting…", {
        fontFamily: FONT_FAMILY,
        fontSize: FONT_SIZE_BODY,
        color: hex(TEXT_MUTED),
      })
      .setOrigin(0.5);

    if (!this.playerId) {
      this.endWithError("missing player_id in URL");
      return;
    }

    this.events.once("shutdown", this.cleanup, this);
    this.connect();
  }

  private connect(): void {
    this.attempt += 1;
    const sync = getSpectatorSync();
    setActiveState(sync.state);

    // SpectateGame transitions on the first 'state' (i.e. catchup STATE).
    // Listen here so we know when to swap scenes; if the WS closes before
    // we get one we treat it as a 503-equivalent and retry.
    const onState = () => {
      if (this.transitioned) return;
      this.transitioned = true;
      sync.state.off("state", onState);
      sync.state.off("error", onError);
      sync.state.off("disconnect", onDisconnect);
      this.scene.start("SpectateGame", { displayName: this.displayName });
      this.scene.launch("GameHUD");
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
      // Connection dropped before catchup STATE — treat as cap-503.
      if (this.attempt < RETRY_MAX_ATTEMPTS) {
        this.scheduleRetry();
      } else {
        this.endWithError("server is full or run ended");
      }
    };

    sync.state.on("state", onState);
    sync.state.on("error", onError);
    sync.state.on("disconnect", onDisconnect);

    const wsProto = window.location.protocol === "https:" ? "wss:" : "ws:";
    const url = `${wsProto}//${window.location.host}/ws/spectate?player_id=${encodeURIComponent(this.playerId)}`;
    this.statusText?.setText(
      this.attempt === 1
        ? "Connecting…"
        : `Connecting (attempt ${this.attempt}/${RETRY_MAX_ATTEMPTS})…`,
    );
    sync.connect(url).catch(() => {
      // The actual close handler above drives the retry; reject here just
      // means the WS open() promise rejected, which is also covered by
      // the 'disconnect' state event.
    });
  }

  private scheduleRetry(): void {
    this.statusText?.setText(`Server full — retrying in ${Math.round(RETRY_AFTER_DEFAULT_MS / 1000)}s…`);
    this.retryTimer = this.time.delayedCall(RETRY_AFTER_DEFAULT_MS, () => {
      if (this.transitioned) return;
      this.connect();
    });
  }

  private endWithError(message: string): void {
    if (this.transitioned) return;
    this.transitioned = true;
    this.scene.start("SpectateEnd", {
      displayName: this.displayName,
      reason: message,
    });
  }

  private cleanup(): void {
    this.retryTimer?.remove(false);
    this.retryTimer = undefined;
  }
}

export default SpectateConnecting;
