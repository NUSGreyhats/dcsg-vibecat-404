#!/usr/bin/env python3
"""Generate 10 Tiled-format room JSON maps for the game."""
import json, os

TILE_W, TILE_H, TILE_SIZE = 16, 12, 64
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

# ---- Room layouts ----

def room1():
    g = grid(); perimeter(g,"right")
    o = [spawn(1,128,384),
         enemy(2,600,200),enemy(3,700,500),enemy(4,500,400),
         coin(5,400,300),coin(6,600,400)]
    return make_map(g, o + exit_objs("right",7))

def room2():
    g = grid(); perimeter(g,"top")
    for c in range(3,10): g[4][c] = WALL_GID
    for c in range(6,13): g[8][c] = WALL_GID
    o = [spawn(1,128,640),
         enemy(2,300,200),enemy(3,600,200),enemy(4,400,500),enemy(5,700,600),
         coin(6,200,300),coin(7,500,650)]
    return make_map(g, o + exit_objs("top",8))

def room3():
    g = grid(); perimeter(g,"bottom")
    for r in range(4,7):
        for c in range(6,9): g[r][c] = WALL_GID
    o = [spawn(1,128,128),
         enemy(2,300,600),enemy(3,700,200),enemy(4,200,400),
         enemy(5,800,400,"robot-heavy"),
         coin(6,400,200),coin(7,600,600),coin(8,850,150)]
    return make_map(g, o + exit_objs("bottom",9))

def room4():
    g = grid(); perimeter(g,"left")
    for r in range(2,8): g[r][8] = WALL_GID
    for c in range(8,14): g[7][c] = WALL_GID
    o = [spawn(1,880,384),
         enemy(2,200,200),enemy(3,400,300),enemy(4,300,600),
         enemy(5,200,500,"robot-heavy"),enemy(6,600,150),
         coin(7,150,400),coin(8,600,300),coin(9,400,600)]
    return make_map(g, o + exit_objs("left",10))

def room5():
    g = grid(); perimeter(g,"right")
    for r in range(1,9): g[r][5] = WALL_GID
    for r in range(3,11): g[r][10] = WALL_GID
    o = [spawn(1,160,384),
         enemy(2,300,200),enemy(3,450,600),enemy(4,700,200),
         enemy(5,800,500),enemy(6,650,400,"robot-heavy"),
         enemy(7,250,500,"robot-heavy"),
         coin(8,200,300),coin(9,480,400),coin(10,750,350)]
    return make_map(g, o + exit_objs("right",11))

def room6():
    g = grid(); perimeter(g,"top")
    for c in range(4,12): g[6][c] = WALL_GID
    for r in range(3,10): g[r][8] = WALL_GID
    o = [spawn(1,128,640),
         enemy(2,250,200),enemy(3,600,150),enemy(4,800,250),
         enemy(5,300,500),enemy(6,700,500),
         enemy(7,400,300,"robot-heavy"),enemy(8,850,600,"robot-heavy"),
         coin(9,200,150),coin(10,600,300),coin(11,450,500),coin(12,850,400)]
    return make_map(g, o + exit_objs("top",13))

def room7():
    g = grid(); perimeter(g,"bottom")
    for c in range(1,8): g[3][c] = WALL_GID
    for c in range(8,15): g[5][c] = WALL_GID
    for c in range(1,8): g[7][c] = WALL_GID
    for c in range(8,15): g[9][c] = WALL_GID
    o = [spawn(1,880,128),
         enemy(2,200,150),enemy(3,600,280),enemy(4,200,430),
         enemy(5,700,430),enemy(6,300,580),
         enemy(7,800,580,"robot-heavy"),enemy(8,400,280,"robot-heavy"),
         enemy(9,150,280,"robot-heavy"),
         coin(10,700,150),coin(11,300,350),coin(12,700,500),coin(13,200,650)]
    return make_map(g, o + exit_objs("bottom",14))

def room8():
    g = grid(); perimeter(g,"left")
    for r,c in [(2,3),(2,4),(4,7),(4,8),(6,3),(3,11),(4,11),
                (7,6),(7,7),(9,10),(9,11),(5,13),(8,3),(8,4),(3,5),(9,7)]:
        if 0<r<TILE_H-1 and 0<c<TILE_W-1: g[r][c] = WALL_GID
    o = [spawn(1,880,384),
         enemy(2,200,150),enemy(3,500,200),enemy(4,300,400),
         enemy(5,600,350),enemy(6,200,600),enemy(7,700,550),
         enemy(8,400,500,"robot-heavy"),enemy(9,800,200,"robot-heavy"),
         enemy(10,550,600,"robot-heavy"),
         coin(11,150,300),coin(12,450,400),coin(13,700,250),coin(14,350,600)]
    return make_map(g, o + exit_objs("left",15))

def room9():
    g = grid(); perimeter(g,"right")
    for sr,sc in [(2,3),(2,11),(8,3),(8,11)]:
        for dr in range(2):
            for dc in range(2): g[sr+dr][sc+dc] = WALL_GID
    o = [spawn(1,128,384),
         enemy(2,300,150),enemy(3,700,150),enemy(4,300,600),
         enemy(5,700,600),enemy(6,500,300),enemy(7,500,500),
         enemy(8,200,380),enemy(9,800,380,"robot-heavy"),
         enemy(10,400,200,"robot-heavy"),enemy(11,600,500,"robot-heavy"),
         enemy(12,500,400,"robot-heavy"),
         coin(13,500,200),coin(14,500,580),coin(15,200,200),
         coin(16,800,200),coin(17,800,580)]
    return make_map(g, o + exit_objs("right",18))

def room10():
    g = grid(); perimeter(g,"top")
    for r in range(3,10): g[r][4] = WALL_GID; g[r][11] = WALL_GID
    g[5][7]=WALL_GID; g[5][8]=WALL_GID
    g[8][7]=WALL_GID; g[8][8]=WALL_GID
    o = [spawn(1,512,640),
         enemy(2,200,150),enemy(3,800,150),enemy(4,350,300),
         enemy(5,650,300),enemy(6,500,200),enemy(7,400,450),
         enemy(8,600,450),enemy(9,500,350,"robot-heavy"),
         enemy(10,350,550,"robot-heavy"),enemy(11,650,550,"robot-heavy"),
         enemy(12,200,400,"robot-heavy"),enemy(13,800,400,"robot-heavy"),
         enemy(14,500,500,"robot-heavy"),enemy(15,300,200),
         coin(16,512,300),coin(17,200,300),coin(18,800,300),
         coin(19,512,550),coin(20,350,650)]
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
