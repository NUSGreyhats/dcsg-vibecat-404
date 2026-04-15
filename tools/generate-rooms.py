#!/usr/bin/env python3
"""Generate 10 Tiled-format room JSON maps for the game."""
import json, os

TILE_W, TILE_H, TILE_SIZE = 20, 12, 64
FLOOR_GID = 8
WALL_GID = 182

ROOMS_DIR = os.path.join(os.path.dirname(__file__),
    "..", "client", "public", "assets", "rooms")

TILESET = {
    "columns": 27, "firstgid": 1,
    "image": "../topdown-tilesheet-extruded.png",
    "imageheight": 1320, "imagewidth": 1782,
    "margin": 1, "name": "top_down_shooter",
    "spacing": 2, "tilecount": 540,
    "tileheight": TILE_SIZE, "tilewidth": TILE_SIZE,
}

def grid():
    return [[0]*TILE_W for _ in range(TILE_H)]

def perimeter(g, exit_side, gap=2):
    for c in range(TILE_W):
        g[0][c] = WALL_GID; g[TILE_H-1][c] = WALL_GID
    for r in range(TILE_H):
        g[r][0] = WALL_GID; g[r][TILE_W-1] = WALL_GID
    mc, mr, h = TILE_W//2, TILE_H//2, gap//2
    if exit_side == "right":
        for i in range(gap): g[mr-h+i][TILE_W-1] = 0
    elif exit_side == "left":
        for i in range(gap): g[mr-h+i][0] = 0
    elif exit_side == "top":
        for i in range(gap): g[0][mc-h+i] = 0
    elif exit_side == "bottom":
        for i in range(gap): g[TILE_H-1][mc-h+i] = 0

def exit_objs(side, oid):
    mc, mr, gap, h = TILE_W//2, TILE_H//2, 2, 1
    T = TILE_SIZE
    if side == "right":
        bx,by,bw,bh = (TILE_W-1)*T,(mr-h)*T,T,gap*T
    elif side == "left":
        bx,by,bw,bh = 0,(mr-h)*T,T,gap*T
    elif side == "top":
        bx,by,bw,bh = (mc-h)*T,0,gap*T,T
    else:
        bx,by,bw,bh = (mc-h)*T,(TILE_H-1)*T,gap*T,T
    blocker = obj(oid,"DoorBlocker","DoorBlocker",bx,by,bw,bh)
    ex,ey,ew,eh = bx-16,by-16,bw+32,bh+32
    if side == "right": ex = bx-48; ew = bw+48
    elif side == "left": ex = 0; ew = bw+48
    elif side == "top": ey = 0; eh = bh+48
    else: ey = by-48; eh = bh+48
    zone = obj(oid+1,"Exit","Exit",ex,ey,ew,eh)
    return [blocker, zone]

def obj(id,name,typ,x,y,w=0,h=0,props=None):
    o = {"id":id,"name":name,"type":typ,"x":x,"y":y,
         "width":w,"height":h,"visible":True,"rotation":0}
    if props: o["properties"] = props
    return o

def enemy(id,x,y,t="robot-grunt"):
    return obj(id,"EnemySpawn","EnemySpawn",x,y,
               props=[{"name":"enemyType","type":"string","value":t}])

def coin(id,x,y): return obj(id,"CoinSpawn","CoinSpawn",x,y)
def spawn(id,x,y): return obj(id,"PlayerSpawn","PlayerSpawn",x,y)

def make_map(wg, objs):
    return {
        "compressionlevel":-1,"height":TILE_H,"infinite":False,
        "layers":[
            {"data":[FLOOR_GID]*(TILE_W*TILE_H),"height":TILE_H,"id":1,
             "name":"Ground","opacity":1,"type":"tilelayer","visible":True,
             "width":TILE_W,"x":0,"y":0},
            {"data":[wg[r][c] for r in range(TILE_H) for c in range(TILE_W)],
             "height":TILE_H,"id":2,"name":"Walls","opacity":1,
             "type":"tilelayer","visible":True,"width":TILE_W,"x":0,"y":0},
            {"draworder":"topdown","id":3,"name":"Objects","objects":objs,
             "opacity":1,"type":"objectgroup","visible":True,"x":0,"y":0}
        ],
        "nextlayerid":4,
        "nextobjectid":max(o["id"] for o in objs)+1 if objs else 1,
        "orientation":"orthogonal","renderorder":"right-down",
        "tiledversion":"1.11.2","tileheight":TILE_SIZE,
        "tilesets":[TILESET],"tilewidth":TILE_SIZE,
        "type":"map","version":"1.10","width":TILE_W
    }

# ---- Room layouts (20x12 tiles = 1280x768) ----

def room1():
    g = grid(); perimeter(g,"right")
    o = [spawn(1,160,384),
         enemy(2,750,200),enemy(3,880,500),enemy(4,620,400),
         coin(5,500,300),coin(6,750,400)]
    return make_map(g, o + exit_objs("right",7))

def room2():
    g = grid(); perimeter(g,"top")
    for c in range(3,12): g[4][c] = WALL_GID
    for c in range(8,17): g[8][c] = WALL_GID
    o = [spawn(1,160,640),
         enemy(2,380,200),enemy(3,750,200),enemy(4,500,500),enemy(5,880,600),
         coin(6,250,300),coin(7,620,650)]
    return make_map(g, o + exit_objs("top",8))

def room3():
    g = grid(); perimeter(g,"bottom")
    for r in range(4,7):
        for c in range(8,11): g[r][c] = WALL_GID
    o = [spawn(1,160,128),
         enemy(2,380,600),enemy(3,880,200),enemy(4,250,400),
         enemy(5,1000,400,"robot-heavy"),
         coin(6,500,200),coin(7,750,600),coin(8,1060,150)]
    return make_map(g, o + exit_objs("bottom",9))

def room4():
    g = grid(); perimeter(g,"left")
    for r in range(2,8): g[r][10] = WALL_GID
    for c in range(10,18): g[7][c] = WALL_GID
    o = [spawn(1,1100,384),
         enemy(2,250,200),enemy(3,500,300),enemy(4,380,600),
         enemy(5,250,500,"robot-heavy"),enemy(6,750,150),
         coin(7,190,400),coin(8,750,300),coin(9,500,600)]
    return make_map(g, o + exit_objs("left",10))

def room5():
    g = grid(); perimeter(g,"right")
    for r in range(1,9): g[r][6] = WALL_GID
    for r in range(3,11): g[r][13] = WALL_GID
    o = [spawn(1,200,384),
         enemy(2,380,200),enemy(3,560,600),enemy(4,880,200),
         enemy(5,1000,500),enemy(6,810,400,"robot-heavy"),
         enemy(7,310,500,"robot-heavy"),
         coin(8,250,300),coin(9,600,400),coin(10,940,350)]
    return make_map(g, o + exit_objs("right",11))

def room6():
    g = grid(); perimeter(g,"top")
    for c in range(5,15): g[6][c] = WALL_GID
    for r in range(3,10): g[r][10] = WALL_GID
    o = [spawn(1,160,640),
         enemy(2,310,200),enemy(3,750,150),enemy(4,1000,250),
         enemy(5,380,500),enemy(6,880,500),
         enemy(7,500,300,"robot-heavy"),enemy(8,1060,600,"robot-heavy"),
         coin(9,250,150),coin(10,750,300),coin(11,560,500),coin(12,1060,400)]
    return make_map(g, o + exit_objs("top",13))

def room7():
    g = grid(); perimeter(g,"bottom")
    for c in range(1,10): g[3][c] = WALL_GID
    for c in range(10,19): g[5][c] = WALL_GID
    for c in range(1,10): g[7][c] = WALL_GID
    for c in range(10,19): g[9][c] = WALL_GID
    o = [spawn(1,1100,128),
         enemy(2,250,150),enemy(3,750,280),enemy(4,250,430),
         enemy(5,880,430),enemy(6,380,580),
         enemy(7,1000,580,"robot-heavy"),enemy(8,500,280,"robot-heavy"),
         enemy(9,190,280,"robot-heavy"),
         coin(10,880,150),coin(11,380,350),coin(12,880,500),coin(13,250,650)]
    return make_map(g, o + exit_objs("bottom",14))

def room8():
    g = grid(); perimeter(g,"left")
    for r,c in [(2,3),(2,5),(4,9),(4,10),(6,4),(3,14),(4,14),
                (7,8),(7,9),(9,13),(9,14),(5,16),(8,4),(8,5),(3,6),(9,9)]:
        if 0<r<TILE_H-1 and 0<c<TILE_W-1: g[r][c] = WALL_GID
    o = [spawn(1,1100,384),
         enemy(2,250,150),enemy(3,620,200),enemy(4,380,400),
         enemy(5,750,350),enemy(6,250,600),enemy(7,880,550),
         enemy(8,500,500,"robot-heavy"),enemy(9,1000,200,"robot-heavy"),
         enemy(10,690,600,"robot-heavy"),
         coin(11,190,300),coin(12,560,400),coin(13,880,250),coin(14,440,600)]
    return make_map(g, o + exit_objs("left",15))

def room9():
    g = grid(); perimeter(g,"right")
    for sr,sc in [(2,4),(2,14),(8,4),(8,14)]:
        for dr in range(2):
            for dc in range(2): g[sr+dr][sc+dc] = WALL_GID
    o = [spawn(1,160,384),
         enemy(2,380,150),enemy(3,880,150),enemy(4,380,600),
         enemy(5,880,600),enemy(6,620,300),enemy(7,620,500),
         enemy(8,250,380),enemy(9,1000,380,"robot-heavy"),
         enemy(10,500,200,"robot-heavy"),enemy(11,750,500,"robot-heavy"),
         enemy(12,620,400,"robot-heavy"),
         coin(13,620,200),coin(14,620,580),coin(15,250,200),
         coin(16,1000,200),coin(17,1000,580)]
    return make_map(g, o + exit_objs("right",18))

def room10():
    g = grid(); perimeter(g,"top")
    for r in range(3,10): g[r][5] = WALL_GID; g[r][14] = WALL_GID
    g[5][9]=WALL_GID; g[5][10]=WALL_GID
    g[8][9]=WALL_GID; g[8][10]=WALL_GID
    o = [spawn(1,640,640),
         enemy(2,250,150),enemy(3,1000,150),enemy(4,440,300),
         enemy(5,810,300),enemy(6,620,200),enemy(7,500,450),
         enemy(8,750,450),enemy(9,620,350,"robot-heavy"),
         enemy(10,440,550,"robot-heavy"),enemy(11,810,550,"robot-heavy"),
         enemy(12,250,400,"robot-heavy"),enemy(13,1000,400,"robot-heavy"),
         enemy(14,620,500,"robot-heavy"),enemy(15,380,200),
         coin(16,640,300),coin(17,250,300),coin(18,1000,300),
         coin(19,640,550),coin(20,440,650)]
    return make_map(g, o + exit_objs("top",21))

ROOMS = [room1,room2,room3,room4,room5,room6,room7,room8,room9,room10]

if __name__ == "__main__":
    os.makedirs(ROOMS_DIR, exist_ok=True)
    for i, gen in enumerate(ROOMS, 1):
        path = os.path.join(ROOMS_DIR, f"room-{i}.json")
        with open(path, 'w') as f:
            json.dump(gen(), f, indent=2)
        print(f"Generated room-{i}.json")
    print("Done! 10 rooms generated.")
