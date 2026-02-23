/**
 * Bridge: manages WebSocket connections from Go server,
 * mapping each connection to a Nova Sonic session.
 */

import { type WebSocket } from "ws";
import { NovaSonicClient, type NovaSonicConfig } from "./nova-sonic-client.js";
import { DEFAULT_TOOLS, type ToolSpec } from "./tools.js";

/** Messages from Go → Sidecar */
interface InitMessage {
  type: "init";
  config: {
    systemPrompt: string;
    tools?: ToolSpec[];
    voice?: string;
    orgId?: string;
    callerPhone?: string;
  };
}

interface AudioMessage {
  type: "audio";
  data: string; // base64 PCM
}

interface ToolResultMessage {
  type: "tool_result";
  toolCallId: string;
  result: string;
}

interface CloseMessage {
  type: "close";
}

type InboundMessage = InitMessage | AudioMessage | ToolResultMessage | CloseMessage;

/** Messages from Sidecar → Go */
type OutboundMessage =
  | { type: "audio"; data: string }
  | { type: "tool_call"; toolCallId: string; toolName: string; input: Record<string, unknown> }
  | { type: "transcript"; role: "user" | "assistant"; text: string }
  | { type: "error"; message: string }
  | { type: "session_renewed" };

export class CallSession {
  private ws: WebSocket;
  private callId: string;
  private client: NovaSonicClient | null = null;

  constructor(ws: WebSocket, callId: string) {
    this.ws = ws;
    this.callId = callId;

    ws.on("message", (raw) => {
      try {
        const msg: InboundMessage = JSON.parse(raw.toString());
        this.handleMessage(msg);
      } catch (err: any) {
        this.send({ type: "error", message: `Invalid message: ${err.message}` });
      }
    });

    ws.on("close", () => {
      this.log("info", "Go WebSocket closed");
      this.cleanup();
    });

    ws.on("error", (err) => {
      this.log("error", `WebSocket error: ${err.message}`);
      this.cleanup();
    });

    this.log("info", "Call session created");
  }

  private handleMessage(msg: InboundMessage): void {
    switch (msg.type) {
      case "init":
        this.handleInit(msg);
        break;
      case "audio":
        this.client?.sendAudio(msg.data);
        break;
      case "tool_result":
        this.client?.sendToolResult(msg.toolCallId, msg.result);
        break;
      case "close":
        this.cleanup();
        break;
    }
  }

  private async handleInit(msg: InitMessage): Promise<void> {
    if (this.client) {
      this.send({ type: "error", message: "Session already initialized" });
      return;
    }

    // Normalize tools from Go format ({name, description, inputSchema}) to
    // Bedrock format ({toolSpec: {name, description, inputSchema: {json}}})
    let tools: ToolSpec[] = DEFAULT_TOOLS;
    if (msg.config.tools && msg.config.tools.length > 0) {
      tools = msg.config.tools.map((t: any) => {
        // Already in Bedrock format?
        if (t.toolSpec) return t as ToolSpec;
        // Go ToolDefinition format — wrap it
        return {
          toolSpec: {
            name: t.name,
            description: t.description,
            inputSchema: { json: typeof t.inputSchema === "string" ? JSON.parse(t.inputSchema) : t.inputSchema },
          },
        } as ToolSpec;
      });
    }

    const config: NovaSonicConfig = {
      systemPrompt: msg.config.systemPrompt,
      tools,
      voice: msg.config.voice || "tiffany",
      inputSampleRate: 8000,
      outputSampleRate: 8000,
    };

    this.client = new NovaSonicClient(this.callId, config, {
      onAudio: (data) => this.send({ type: "audio", data }),
      onToolCall: (toolCallId, toolName, input) =>
        this.send({ type: "tool_call", toolCallId, toolName, input }),
      onTranscript: (role, text) => this.send({ type: "transcript", role, text }),
      onError: (message) => this.send({ type: "error", message }),
      onSessionRenewed: () => this.send({ type: "session_renewed" }),
    });

    try {
      await this.client.start();
      this.log("info", "Nova Sonic session started");
    } catch (err: any) {
      this.log("error", `Failed to start Nova Sonic: ${err.message}`);
      this.send({ type: "error", message: `Init failed: ${err.message}` });
      this.client = null;
    }
  }

  private send(msg: OutboundMessage): void {
    if (this.ws.readyState === this.ws.OPEN) {
      this.ws.send(JSON.stringify(msg));
    }
  }

  private async cleanup(): Promise<void> {
    if (this.client) {
      try {
        await this.client.close();
      } catch {
        // ignore
      }
      this.client = null;
    }
    if (this.ws.readyState === this.ws.OPEN) {
      this.ws.close();
    }
    this.log("info", "Call session cleaned up");
  }

  private log(level: string, msg: string): void {
    const ts = new Date().toISOString();
    const payload = JSON.stringify({ ts, level, callId: this.callId, msg });
    if (level === "error") console.error(payload);
    else console.log(payload);
  }
}
