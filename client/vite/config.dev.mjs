import { defineConfig } from 'vite';

// Game server (WebSocket gameplay) on :8080; lobby (identity HTTP +
// leaderboard + admin) on :3001. By default everything proxies to the
// game server, which works when you run it with DEV_MOUNT_IDENTITY=1
// (mounts /api/login etc. on :8080) — single-binary dev. Set
// LOBBY_SERVER to point identity/leaderboard/admin routes at a real
// lobby instead.
const gameServer = process.env.GAME_SERVER ?? 'http://localhost:8080';
const gameServerWs = gameServer.replace(/^http/, 'ws');
const lobbyServer = process.env.LOBBY_SERVER ?? gameServer;

export default defineConfig({
    base: './',
    build: {
        rollupOptions: {
            input: {
                index: 'index.html',
                spectate: 'spectate.html',
                replay: 'replay.html',
            },
            output: {
                manualChunks: {
                    phaser: ['phaser']
                }
            }
        },
    },
    server: {
        // 5173 (Vite default) so it doesn't clash with the game server on
        // :8080. WS_URL auto-resolves to ws://<host>/ws, which Vite then
        // proxies down to the Go server — same URL shape as prod Caddy.
        port: 5173,
        proxy: {
            // /ws covers /ws, /ws/spectate, /ws/replay because Vite's
            // default match is a path prefix. The WS upgrade carries
            // query strings (replay_id, ticket) verbatim. Everything HTTP
            // — identity, leaderboard, replay, /register page, admin —
            // is on the lobby service.
            '/ws': { target: gameServerWs, ws: true, changeOrigin: true },
            '/api': { target: lobbyServer, changeOrigin: true },
            '/register': { target: lobbyServer, changeOrigin: true },
            '/leaderboard': { target: lobbyServer, changeOrigin: true },
            '/spectate': { target: lobbyServer, changeOrigin: true },
            '/admin': { target: lobbyServer, changeOrigin: true },
        },
    }
});
