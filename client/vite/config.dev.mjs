import { defineConfig } from 'vite';

// Game server expected at :8080 in dev. Override with GAME_SERVER if you
// need to point Vite at a different host/port.
const gameServer = process.env.GAME_SERVER ?? 'http://localhost:8080';
const gameServerWs = gameServer.replace(/^http/, 'ws');

// Lobby service expected at :3001 in dev (serves /leaderboard, /spectate,
// /register page, /api/leaderboard, /api/replay/*). Falls back to the
// game server so a dev who's only running the game binary with
// DEV_MOUNT_IDENTITY=1 still gets /register + /api/login. Override with
// LOBBY_SERVER if running the lobby on a non-default port.
const lobbyServer = process.env.LOBBY_SERVER ?? 'http://localhost:3001';

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
            // query strings (replay_id, ticket) verbatim.
            '/ws': { target: gameServerWs, ws: true, changeOrigin: true },
            '/api': { target: gameServer, changeOrigin: true },
            '/register': { target: gameServer, changeOrigin: true },
            // Leaderboard page is lobby-only; the game server doesn't
            // expose /leaderboard even with DEV_MOUNT_IDENTITY=1, so this
            // has to go to the lobby binary.
            '/leaderboard': { target: lobbyServer, changeOrigin: true },
        },
    }
});
