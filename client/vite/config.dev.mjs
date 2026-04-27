import { defineConfig } from 'vite';

// Game server expected at :8080 in dev. Override with GAME_SERVER if you
// need to point Vite at a different host/port.
const gameServer = process.env.GAME_SERVER ?? 'http://localhost:8080';
const gameServerWs = gameServer.replace(/^http/, 'ws');

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
        },
    }
});
