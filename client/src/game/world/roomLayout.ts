import Phaser from "phaser";

export type DoorSide = "top" | "bottom" | "left" | "right";

export interface WallSpec {
  cx: number;
  cy: number;
  w: number;
  h: number;
}

function aabbOverlap(
  ax: number,
  ay: number,
  aw: number,
  ah: number,
  bx: number,
  by: number,
  bw: number,
  bh: number,
): boolean {
  const al = ax - aw / 2;
  const ar = ax + aw / 2;
  const at = ay - ah / 2;
  const ab = ay + ah / 2;
  const bl = bx - bw / 2;
  const br = bx + bw / 2;
  const bt = by - bh / 2;
  const bb = by + bh / 2;
  return al < br && ar > bl && at < bb && ab > bt;
}

function intersectsSafeZone(
  cx: number,
  cy: number,
  w: number,
  h: number,
  safeCx: number,
  safeCy: number,
  safeW: number,
  safeH: number,
): boolean {
  return aabbOverlap(cx, cy, w, h, safeCx, safeCy, safeW, safeH);
}

function intersectsDoorZone(
  cx: number,
  cy: number,
  w: number,
  h: number,
  roomW: number,
  roomH: number,
  wallT: number,
  doorGap: number,
  doorSide: DoorSide,
): boolean {
  const pad = 16;
  let zx: number;
  let zy: number;
  let zw: number;
  let zh: number;
  switch (doorSide) {
    case "top":
      zx = roomW / 2;
      zy = wallT / 2;
      zw = doorGap + pad * 2;
      zh = wallT + pad * 2;
      break;
    case "bottom":
      zx = roomW / 2;
      zy = roomH - wallT / 2;
      zw = doorGap + pad * 2;
      zh = wallT + pad * 2;
      break;
    case "left":
      zx = wallT / 2;
      zy = roomH / 2;
      zw = wallT + pad * 2;
      zh = doorGap + pad * 2;
      break;
    case "right":
      zx = roomW - wallT / 2;
      zy = roomH / 2;
      zw = wallT + pad * 2;
      zh = doorGap + pad * 2;
      break;
  }
  return aabbOverlap(cx, cy, w, h, zx, zy, zw, zh);
}

export function buildPerimeterWalls(
  roomW: number,
  roomH: number,
  wallT: number,
  doorGap: number,
  doorSide: DoorSide,
): WallSpec[] {
  const walls: WallSpec[] = [];
  const halfG = doorGap / 2;
  const midX = roomW / 2;
  const midY = roomH / 2;

  const addTopBottomSegments = (y: number) => {
    const gx0 = midX - halfG;
    const gx1 = midX + halfG;
    const leftW = gx0;
    const leftCx = leftW / 2;
    if (leftW > wallT) walls.push({ cx: leftCx, cy: y, w: leftW, h: wallT });

    const rightW = roomW - gx1;
    const rightCx = gx1 + rightW / 2;
    if (rightW > wallT) walls.push({ cx: rightCx, cy: y, w: rightW, h: wallT });
  };

  const addLeftRightSegments = (x: number) => {
    const gy0 = midY - halfG;
    const gy1 = midY + halfG;
    const topH = gy0;
    const topCy = topH / 2;
    if (topH > wallT) walls.push({ cx: x, cy: topCy, w: wallT, h: topH });

    const botH = roomH - gy1;
    const botCy = gy1 + botH / 2;
    if (botH > wallT) walls.push({ cx: x, cy: botCy, w: wallT, h: botH });
  };

  switch (doorSide) {
    case "top":
      addTopBottomSegments(wallT / 2);
      walls.push({
        cx: midX,
        cy: roomH - wallT / 2,
        w: roomW,
        h: wallT,
      });
      walls.push({
        cx: wallT / 2,
        cy: midY,
        w: wallT,
        h: roomH - wallT * 2,
      });
      walls.push({
        cx: roomW - wallT / 2,
        cy: midY,
        w: wallT,
        h: roomH - wallT * 2,
      });
      break;
    case "bottom":
      walls.push({
        cx: midX,
        cy: wallT / 2,
        w: roomW,
        h: wallT,
      });
      addTopBottomSegments(roomH - wallT / 2);
      walls.push({
        cx: wallT / 2,
        cy: midY,
        w: wallT,
        h: roomH - wallT * 2,
      });
      walls.push({
        cx: roomW - wallT / 2,
        cy: midY,
        w: wallT,
        h: roomH - wallT * 2,
      });
      break;
    case "left":
      walls.push({
        cx: midX,
        cy: wallT / 2,
        w: roomW,
        h: wallT,
      });
      walls.push({
        cx: midX,
        cy: roomH - wallT / 2,
        w: roomW,
        h: wallT,
      });
      addLeftRightSegments(wallT / 2);
      walls.push({
        cx: roomW - wallT / 2,
        cy: midY,
        w: wallT,
        h: roomH - wallT * 2,
      });
      break;
    case "right":
      walls.push({
        cx: midX,
        cy: wallT / 2,
        w: roomW,
        h: wallT,
      });
      walls.push({
        cx: midX,
        cy: roomH - wallT / 2,
        w: roomW,
        h: wallT,
      });
      walls.push({
        cx: wallT / 2,
        cy: midY,
        w: wallT,
        h: roomH - wallT * 2,
      });
      addLeftRightSegments(roomW - wallT / 2);
      break;
  }

  return walls;
}

export function getDoorBlockerSpec(
  roomW: number,
  roomH: number,
  wallT: number,
  doorGap: number,
  doorSide: DoorSide,
): WallSpec {
  const midX = roomW / 2;
  const midY = roomH / 2;
  switch (doorSide) {
    case "top":
      return { cx: midX, cy: wallT / 2, w: doorGap - 4, h: wallT };
    case "bottom":
      return { cx: midX, cy: roomH - wallT / 2, w: doorGap - 4, h: wallT };
    case "left":
      return { cx: wallT / 2, cy: midY, w: wallT, h: doorGap - 4 };
    case "right":
      return { cx: roomW - wallT / 2, cy: midY, w: wallT, h: doorGap - 4 };
  }
}

export function getExitZoneCenter(
  roomW: number,
  roomH: number,
  wallT: number,
  doorSide: DoorSide,
): { cx: number; cy: number; w: number; h: number } {
  const depth = 48;
  const span = 100;
  switch (doorSide) {
    case "top":
      return { cx: roomW / 2, cy: wallT + depth / 2, w: span, h: depth };
    case "bottom":
      return {
        cx: roomW / 2,
        cy: roomH - wallT - depth / 2,
        w: span,
        h: depth,
      };
    case "left":
      return { cx: wallT + depth / 2, cy: roomH / 2, w: depth, h: span };
    case "right":
      return {
        cx: roomW - wallT - depth / 2,
        cy: roomH / 2,
        w: depth,
        h: span,
      };
  }
}

export function getPlayerSpawnForEntry(
  roomW: number,
  roomH: number,
  entryFrom: DoorSide | null,
  padding: number,
): { x: number; y: number } {
  if (!entryFrom) {
    return { x: roomW / 2, y: roomH / 2 };
  }
  switch (entryFrom) {
    case "top":
      return { x: roomW / 2, y: padding + 40 };
    case "bottom":
      return { x: roomW / 2, y: roomH - padding - 40 };
    case "left":
      return { x: padding + 40, y: roomH / 2 };
    case "right":
      return { x: roomW - padding - 40, y: roomH / 2 };
  }
}

export function oppositeSide(side: DoorSide): DoorSide {
  switch (side) {
    case "top":
      return "bottom";
    case "bottom":
      return "top";
    case "left":
      return "right";
    case "right":
      return "left";
  }
}

export function circleIntersectsWallSpec(
  px: number,
  py: number,
  radius: number,
  wall: WallSpec,
): boolean {
  const halfW = wall.w / 2;
  const halfH = wall.h / 2;
  const left = wall.cx - halfW;
  const right = wall.cx + halfW;
  const top = wall.cy - halfH;
  const bottom = wall.cy + halfH;
  const nx = Phaser.Math.Clamp(px, left, right);
  const ny = Phaser.Math.Clamp(py, top, bottom);
  const dx = px - nx;
  const dy = py - ny;
  return dx * dx + dy * dy < radius * radius;
}

export function isClearOfWalls(
  px: number,
  py: number,
  radius: number,
  walls: WallSpec[],
): boolean {
  for (const w of walls) {
    if (circleIntersectsWallSpec(px, py, radius, w)) return false;
  }
  return true;
}

export function generateInteriorWalls(
  roomW: number,
  roomH: number,
  wallT: number,
  doorGap: number,
  doorSide: DoorSide,
  minCount: number,
  maxCount: number,
  safeCx: number,
  safeCy: number,
  safeW: number,
  safeH: number,
): WallSpec[] {
  const count = Phaser.Math.Between(minCount, maxCount);
  const out: WallSpec[] = [];
  const margin = wallT + 40;
  let attempts = 0;
  const maxAttempts = count * 80;

  while (out.length < count && attempts < maxAttempts) {
    attempts++;
    const horizontal = Math.random() < 0.55;
    let w: number;
    let h: number;
    if (horizontal) {
      w = Phaser.Math.Between(96, 260);
      h = wallT;
    } else {
      w = wallT;
      h = Phaser.Math.Between(96, 240);
    }
    const cx = Phaser.Math.Between(margin + w / 2, roomW - margin - w / 2);
    const cy = Phaser.Math.Between(margin + h / 2, roomH - margin - h / 2);

    if (intersectsSafeZone(cx, cy, w, h, safeCx, safeCy, safeW, safeH)) continue;
    if (
      intersectsDoorZone(
        cx,
        cy,
        w,
        h,
        roomW,
        roomH,
        wallT,
        doorGap,
        doorSide,
      )
    )
      continue;

    let hit = false;
    for (const o of out) {
      if (aabbOverlap(cx, cy, w, h, o.cx, o.cy, o.w, o.h)) {
        hit = true;
        break;
      }
    }
    if (hit) continue;

    out.push({ cx, cy, w, h });
  }

  return out;
}
