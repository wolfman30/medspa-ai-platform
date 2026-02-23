/**
 * Nova Sonic Sidecar — main entry point.
 * Express HTTP + WebSocket server.
 */

import express from "express";
import { createServer } from "node:http";
import { WebSocketServer } from "ws";
import { randomUUID } from "node:crypto";
import { CallSession } from "./bridge.js";

const PORT = parseInt(process.env.PORT || "3002", 10);

const app = express();

app.get("/health", (_req, res) => {
  res.json({ status: "ok", service: "nova-sonic-sidecar", uptime: process.uptime() });
});

const server = createServer(app);

const wss = new WebSocketServer({ server, path: "/ws/nova-sonic" });

wss.on("connection", (ws) => {
  const callId = randomUUID();
  console.log(JSON.stringify({ ts: new Date().toISOString(), level: "info", msg: "New WebSocket connection", callId }));
  new CallSession(ws, callId);
});

server.listen(PORT, () => {
  console.log(JSON.stringify({
    ts: new Date().toISOString(),
    level: "info",
    msg: `Nova Sonic sidecar listening on port ${PORT}`,
  }));
});

// Graceful shutdown
function shutdown(signal: string) {
  console.log(JSON.stringify({ ts: new Date().toISOString(), level: "info", msg: `Received ${signal}, shutting down` }));

  wss.clients.forEach((ws) => ws.close());

  server.close(() => {
    console.log(JSON.stringify({ ts: new Date().toISOString(), level: "info", msg: "Server closed" }));
    process.exit(0);
  });

  // Force exit after 10s
  setTimeout(() => process.exit(1), 10_000).unref();
}

process.on("SIGTERM", () => shutdown("SIGTERM"));
process.on("SIGINT", () => shutdown("SIGINT"));
