/**
 * Bridge: manages WebSocket connections from the Go server,
 * mapping each connection to a Nova Sonic session.
 */

import { type WebSocket } from "ws";
import { NovaSonicClient, type NovaSonicConfig } from "./nova-sonic-client.js";
import { ElevenLabsSession, type ElevenLabsConfig } from "./elevenlabs-tts.js";
import { DEFAULT_TOOLS, type ToolSpec } from "./tools.js";

// ── Inbound messages (Go → Sidecar) ───────────────────────────────────

interface InitMessage {
  type: "init";
  config: {
    systemPrompt: string;
    tools?: ToolSpec[];
    voice?: string;
    orgId?: string;
    callerPhone?: string;
    clinicName?: string;
    greeting?: string;
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

// ── Outbound messages (Sidecar → Go) ──────────────────────────────────

type OutboundMessage =
  | { type: "audio"; data: string }
  | { type: "tool_call"; toolCallId: string; toolName: string; input: Record<string, unknown> }
  | { type: "transcript"; role: "user" | "assistant"; text: string }
  | { type: "error"; message: string }
  | { type: "session_renewed" };

// ── GoToolDefinition (flat format from Go) ─────────────────────────────

interface GoToolDefinition {
  name: string;
  description: string;
  inputSchema: string | Record<string, unknown>;
}

// ── CallSession ────────────────────────────────────────────────────────

/** Manages a single call's lifecycle over one WebSocket connection. */
export class CallSession {
  private ws: WebSocket;
  private callId: string;
  private client: NovaSonicClient | null = null;
  private elevenLabsSession: ElevenLabsSession | null = null;
  private elevenLabsApiKey: string;
  private pendingAssistantText = "";
  private ttsQueue: string[] = [];
  private ttsProcessing = false;
  private greetingSent = false;

  constructor(ws: WebSocket, callId: string) {
    this.ws = ws;
    this.callId = callId;
    this.elevenLabsApiKey = process.env.ELEVENLABS_API_KEY || "";

    ws.on("message", (raw) => {
      try {
        const msg: InboundMessage = JSON.parse(raw.toString());
        this.handleMessage(msg);
      } catch (err: unknown) {
        const message = err instanceof Error ? err.message : String(err);
        this.send({ type: "error", message: `Invalid message: ${message}` });
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

  /** Route an inbound message to the appropriate handler. */
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

  /** Initialize the Nova Sonic client from an init message. */
  private async handleInit(msg: InitMessage): Promise<void> {
    if (this.client) {
      this.send({ type: "error", message: "Session already initialized" });
      return;
    }

    const tools = this.normalizeTools(msg.config.tools);

    const config: NovaSonicConfig = {
      systemPrompt: msg.config.systemPrompt,
      tools,
      voice: msg.config.voice || "tiffany",
      inputSampleRate: 8000,
      outputSampleRate: 8000,
    };

    this.client = new NovaSonicClient(this.callId, config, {
      // Nova Sonic native audio ignored — ElevenLabs handles all TTS
      onAudio: (_data) => {},
      onToolCall: (toolCallId, toolName, input) =>
        this.send({ type: "tool_call", toolCallId, toolName, input }),
      onTranscript: (role, text) => {
        // Strip mood/stage direction tags like [warm], [empathetic], etc.
        let cleanText = text.replace(/\[[\w\s]+\]\s*/g, "").trim();
        if (!cleanText) return;

        this.send({ type: "transcript", role, text: cleanText });

        // Route assistant text to ElevenLabs for TTS
        if (role === "assistant") {
          // Skip duplicate greetings — we already sent one via ElevenLabs
          if (this.greetingSent && this.isGreeting(cleanText)) {
            this.log("info", `Skipping duplicate greeting: ${cleanText.substring(0, 60)}`);
            return;
          }
          this.enqueueTTS(cleanText);
        }
      },
      onError: (message) => this.send({ type: "error", message }),
      onSessionRenewed: () => this.send({ type: "session_renewed" }),
    });

    try {
      await this.client.start();
      this.log("info", "Nova Sonic session started");

      // Send auto-greeting via ElevenLabs immediately (bypasses Nova Sonic VAD)
      const greeting = msg.config.greeting ||
        `Thank you for calling ${msg.config.clinicName || "our clinic"}. How can I help you today?`;
      this.log("info", `Sending auto-greeting via ElevenLabs: ${greeting}`);
      this.enqueueTTS(greeting);
      this.greetingSent = true;
      // Also send as transcript so Go side knows about it
      this.send({ type: "transcript", role: "assistant", text: greeting });
    } catch (err: unknown) {
      const message = err instanceof Error ? err.message : String(err);
      this.log("error", `Failed to start Nova Sonic: ${message}`);
      this.send({ type: "error", message: `Init failed: ${message}` });
      this.client = null;
    }
  }

  /**
   * Normalize tools from Go format ({name, description, inputSchema})
   * to Bedrock format ({toolSpec: {name, description, inputSchema: {json}}}).
   */
  private normalizeTools(rawTools?: ToolSpec[] | GoToolDefinition[]): ToolSpec[] {
    if (!rawTools || rawTools.length === 0) return DEFAULT_TOOLS;

    return rawTools.map((t) => {
      // Already in Bedrock format
      if ("toolSpec" in t) return t as ToolSpec;

      // Go ToolDefinition format — wrap it
      const goTool = t as GoToolDefinition;
      return {
        toolSpec: {
          name: goTool.name,
          description: goTool.description,
          inputSchema: {
            json:
              typeof goTool.inputSchema === "string"
                ? JSON.parse(goTool.inputSchema)
                : goTool.inputSchema,
          },
        },
      } as ToolSpec;
    });
  }

  /** Send a message to Go over the WebSocket. */
  private send(msg: OutboundMessage): void {
    if (this.ws.readyState === this.ws.OPEN) {
      this.ws.send(JSON.stringify(msg));
    }
  }

  /** Queue text for ElevenLabs TTS and process sequentially. */
  private enqueueTTS(text: string): void {
    this.ttsQueue.push(text);
    if (!this.ttsProcessing) {
      this.processTTSQueue();
    }
  }

  /** Process queued TTS requests one at a time. */
  private async processTTSQueue(): Promise<void> {
    if (this.ttsProcessing) return;
    this.ttsProcessing = true;

    while (this.ttsQueue.length > 0) {
      const text = this.ttsQueue.shift()!;
      try {
        await this.speakViaElevenLabs(text);
      } catch (err) {
        const message = err instanceof Error ? err.message : String(err);
        this.log("error", `ElevenLabs TTS failed: ${message}`);
      }
    }

    this.ttsProcessing = false;
  }

  /** Speak text via ElevenLabs streaming TTS, sending audio to Go/Telnyx. */
  private speakViaElevenLabs(text: string): Promise<void> {
    return new Promise<void>((resolve, reject) => {
      if (!this.elevenLabsApiKey) {
        this.log("error", "ELEVENLABS_API_KEY not set");
        reject(new Error("ELEVENLABS_API_KEY not set"));
        return;
      }

      const session = new ElevenLabsSession(this.callId, {
        apiKey: this.elevenLabsApiKey,
      }, {
        onAudio: (base64Audio) => {
          // Forward PCM audio to Go → Telnyx
          this.send({ type: "audio", data: base64Audio });
        },
        onError: (message) => {
          this.log("error", `ElevenLabs error: ${message}`);
          this.send({ type: "error", message });
        },
        onDone: () => {
          session.close();
          resolve();
        },
      });

      session.connect()
        .then(() => {
          session.sendText(text);
          session.flush();
        })
        .catch((err) => {
          session.close();
          reject(err);
        });
    });
  }

  /** Close the Nova Sonic client and WebSocket. */
  private async cleanup(): Promise<void> {
    if (this.client) {
      try {
        await this.client.close();
      } catch {
        // ignore cleanup errors
      }
      this.client = null;
    }
    if (this.ws.readyState === this.ws.OPEN) {
      this.ws.close();
    }
    this.log("info", "Call session cleaned up");
  }

  /** Check if text is a greeting (to suppress duplicates from Nova Sonic). */
  private isGreeting(text: string): boolean {
    const lower = text.toLowerCase();
    return (
      (lower.includes("thank") && lower.includes("calling")) ||
      (lower.includes("hi there") && lower.includes("how can i help")) ||
      (lower.includes("welcome to") && lower.includes("how can"))
    );
  }

  private log(level: string, msg: string): void {
    const ts = new Date().toISOString();
    const payload = JSON.stringify({ ts, level, callId: this.callId, msg });
    if (level === "error") console.error(payload);
    else console.log(payload);
  }
}
