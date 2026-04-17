# Assets README
## List of Assets (files: .aseprite -all, .png -all, .json -animated sprites)
1. Player [32x32] (8 directions - idle, move)
2. Player hand sprite [8x8] (not animated)
3. Weapon sprites [32x16] (idle, shoot)
   - Pistol, Rifle, Shotgun
4. Bullet sprites [8x8] (tagged by weapon)
5. Enemy sprite [32x32] (8 directions - idle, move, attack)
6. Coin sprite [16x16] (not animated)
7. Heart sprite [16x16] (not animated)
8. Title screen [1920x1080] (not animated)
9. Tileset [384x352 - 32x32 grid of 12x11 tiles]
   - tiled tilset file (.tsx) included
   - 3 tiled example map file (.tmx) included

## Notes: 
Aseprite is a software for animating / drawing pixel art and sprites (https://www.aseprite.org/)
- it is free! if u compile it yourself (https://github.com/aseprite/aseprite/blob/main/INSTALL.md)
- v tailored with pixel art tools n easy animation
- all the animated sprites i make are tagged to group frames as an animation tgt
- if editing on aseprite, spritesheet exporting instructions as follows (https://docs.phaser.io/phaser-editor/scene-editor/sprite-animations#aseprite-exporting-instructions)

Tiled is a free open-source software to edit maps with tilesets (https://www.mapeditor.org/)
- can use this to edit tilesets and give tiles custom properties (e.g. collides)
- make it much easier to edit maps! can drag and drop tiles, highly recommended
- u can load multiple tilesets at a time too while editing a map
- this is also recommended by phaser (https://docs.phaser.io/api-documentation/class/tilemaps-tilemap)
