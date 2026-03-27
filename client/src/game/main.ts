import { Boot } from "./scenes/Boot";
import { Game as MainGame } from "./scenes/Game";
import { GameHUD } from "./scenes/GameHUD";
import { MainMenu } from "./scenes/MainMenu";
import { PauseMenu } from "./scenes/PauseMenu";
import { InventoryMenu } from "./scenes/InventoryMenu";
import { ShopMenu } from "./scenes/ShopMenu";
import { AUTO, Game } from "phaser";
import { Preloader } from "./scenes/Preloader";

const config: Phaser.Types.Core.GameConfig = {
  type: AUTO,
  width: 1024,
  height: 768,
  physics: {
    default: "arcade",
    arcade: {
      gravity: { x: 0, y: 0 },
    },
  },
  parent: "game-container",
  backgroundColor: "#028af8",
  scene: [
    Boot,
    Preloader,
    MainMenu,
    MainGame,
    GameHUD,
    PauseMenu,
    InventoryMenu,
    ShopMenu,
  ],
};

const StartGame = (parent: string) => {
  return new Game({ ...config, parent });
};

export default StartGame;
