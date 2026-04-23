import Phaser from "phaser";
import { ROOM_WIDTH, ROOM_HEIGHT } from "./constants";
import { Boot } from "./scenes/Boot";
import { Preloader } from "./scenes/Preloader";
import { MainMenu } from "./scenes/MainMenu";
import { Game as MainGame } from "./scenes/Game";
import { GameHUD } from "./scenes/GameHUD";
import { InventoryMenu } from "./scenes/InventoryMenu";
import { WinScreen } from "./scenes/WinScreen";
import { hex, CANVAS_BG } from "./ui/tokens";

export function startGame(parentId: string): Phaser.Game {
  const config: Phaser.Types.Core.GameConfig = {
    type: Phaser.AUTO,
    width: ROOM_WIDTH,
    height: ROOM_HEIGHT,
    parent: parentId,
    backgroundColor: hex(CANVAS_BG),
    pixelArt: true,
    roundPixels: true,
    physics: {
      default: "arcade",
      arcade: {
        gravity: { x: 0, y: 0 },
        debug: false,
      },
    },
    scale: {
      mode: Phaser.Scale.FIT,
      autoCenter: Phaser.Scale.CENTER_BOTH,
    },
    scene: [
      Boot,
      Preloader,
      MainMenu,
      MainGame,
      GameHUD,
      InventoryMenu,
      WinScreen,
    ],
  };

  return new Phaser.Game(config);
}
