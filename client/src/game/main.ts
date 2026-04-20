import { Boot } from "./scenes/Boot";
import { Game as MainGame } from "./scenes/Game";
import { GameHUD } from "./scenes/GameHUD";
import { MainMenu } from "./scenes/MainMenu";
import { PauseMenu } from "./scenes/PauseMenu";
import { WinScreen } from "./scenes/WinScreen";
import { InventoryMenu } from "./scenes/InventoryMenu";
import { ShopMenu } from "./scenes/ShopMenu";
import { AUTO, Game } from "phaser";
import { Preloader } from "./scenes/Preloader";

const config: Phaser.Types.Core.GameConfig = {
  type: AUTO,
  width: 1280,
  height: 768,
  parent: "game-container",
  physics: {
    default: "arcade",
  },
  backgroundColor: "#028af8",
  scene: [
    Boot,
    Preloader,
    MainMenu,
    MainGame,
    GameHUD,
    PauseMenu,
    WinScreen,
    InventoryMenu,
    ShopMenu,
  ],
};

const StartGame = (parent: string) => {
  return new Game({ ...config, parent });
};

export default StartGame;
