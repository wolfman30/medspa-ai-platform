/**
 * Nova Sonic bidirectional streaming client.
 * Manages a single Bedrock InvokeModelWithBidirectionalStream session.
 */

import {
  BedrockRuntimeClient,
  InvokeModelWithBidirectionalStreamCommand,
  type InvokeModelWithBidirectionalStreamCommandOutput,
} from "@aws-sdk/client-bedrock-runtime";
import { NodeHttp2Handler } from "@smithy/node-http-handler";
import {
  fromEnv,
  fromContainerMetadata,
  fromInstanceMetadata,
  createCredentialChain,
} from "@aws-sdk/credential-providers";
import { randomUUID } from "node:crypto";
import { type ToolSpec } from "./tools.js";

// ── Constants ──────────────────────────────────────────────────────────

const MODEL_ID = "amazon.nova-sonic-v1:0";
const SESSION_MAX_MS = 8 * 60 * 1000; // 8 minutes
const RENEWAL_BUFFER_MS = 30 * 1000; // renew 30s before expiry

// ── Public interfaces ──────────────────────────────────────────────────

export interface NovaSonicConfig {
  systemPrompt: string;
  tools: ToolSpec[];
  voice: string;
  inputSampleRate?: number;
  outputSampleRate?: number;
}

export interface NovaSonicCallbacks {
  onAudio: (base64Audio: string) => void;
  onToolCall: (toolCallId: string, toolName: string, input: Record<string, unknown>) => void;
  onTranscript: (role: "user" | "assistant", text: string) => void;
  onError: (message: string) => void;
  onSessionRenewed: () => void;
}

// ── Internal types ─────────────────────────────────────────────────────

/** Wrapper around a single Bedrock stream event to send. */
interface QueuedEvent {
  event: Record<string, unknown>;
}

/** Shape of a parsed Bedrock response event (top-level wrapper). */
interface BedrockResponseEvent {
  event?: Record<string, unknown>;
}

/** contentStart event payload. */
interface ContentStartPayload {
  contentName?: string;
  contentId?: string;
  type?: "AUDIO" | "TEXT" | "TOOL";
  role?: string;
  toolUseConfiguration?: { toolName?: string };
}

/** contentEnd event payload. */
interface ContentEndPayload {
  contentName?: string;
  contentId?: string;
  type?: "AUDIO" | "TEXT" | "TOOL";
  toolUseConfiguration?: { toolUseId?: string };
  toolResultInputConfiguration?: { toolUseId?: string };
}

/** textOutput / toolUse event payload. */
interface TextPayload {
  contentName?: string;
  contentId?: string;
  content?: string;
}

/** audioOutput event payload. */
interface AudioOutputPayload {
  content?: string;
}

// ── Client class ───────────────────────────────────────────────────────

export class NovaSonicClient {
  private callId: string;
  private config: NovaSonicConfig;
  private callbacks: NovaSonicCallbacks;
  private client: BedrockRuntimeClient;

  private promptName = randomUUID();
  private audioContentId = randomUUID();

  // Event queues
  private queue: QueuedEvent[] = [];
  private audioQueue: QueuedEvent[] = [];
  private isActive = false;
  private sessionStartTime = 0;
  private renewalTimer: ReturnType<typeof setTimeout> | null = null;
  private audioChunkCount = 0;

  // Async iterator signaling — any of these resolves unblocks the generator
  private queueResolve: (() => void) | null = null;
  private closeResolve: (() => void) | null = null;
  private audioStateResolve: (() => void) | null = null;

  // Output audio state — hold input while assistant speaks
  private audioOutputActive = false;
  private lastEmittedTranscript = "";
  private recentAssistantTexts = new Set<string>();

  // Accumulation buffers for multi-chunk content
  private textContentBuffers = new Map<string, { role: string; text: string }>();
  private toolUseBuffers = new Map<string, { toolName: string; input: string }>();

  constructor(callId: string, config: NovaSonicConfig, callbacks: NovaSonicCallbacks) {
    this.callId = callId;
    this.config = config;
    this.callbacks = callbacks;

    const handler = new NodeHttp2Handler({
      requestTimeout: 300_000,
      sessionTimeout: 300_000,
      disableConcurrentStreams: false,
      maxConcurrentStreams: 20,
    });

    this.client = new BedrockRuntimeClient({
      region: process.env.AWS_REGION || "us-east-1",
      credentials: createCredentialChain(fromEnv(), fromContainerMetadata(), fromInstanceMetadata()),
      requestHandler: handler,
    });
  }

  // ── Logging ────────────────────────────────────────────────────────

  private log(level: string, msg: string, extra?: Record<string, unknown>): void {
    const ts = new Date().toISOString();
    const payload = JSON.stringify({ ts, level, callId: this.callId, msg, ...extra });
    if (level === "error") console.error(payload);
    else console.log(payload);
  }

  // ── Public API ─────────────────────────────────────────────────────

  /** Start the bidirectional stream and begin processing responses. */
  async start(): Promise<void> {
    this.isActive = true;
    this.sessionStartTime = Date.now();

    this.enqueueSessionStart();
    this.enqueuePromptStart();
    this.enqueueSystemPrompt();
    // CRITICAL: Audio stream MUST be open and continuously active for Nova Sonic
    // to generate audio output. The model requires real-time audio sampling cadence.
    // Order: system prompt → audio stream start → greeting text (delayed)
    this.enqueueAudioContentStart();
    // Send initial silent frames to start the audio stream
    this.enqueueSilentAudioFrames(20);
    // Start continuous silent audio keepalive in background
    this.startSilentKeepalive();

    const response = await this.client.send(
      new InvokeModelWithBidirectionalStreamCommand({
        modelId: MODEL_ID,
        body: this.createAsyncIterable(),
      })
    );

    this.log("info", "Bedrock stream established");

    // NOTE: Auto-greeting is handled by Telnyx TTS (speak command) BEFORE
    // media streaming starts. Nova Sonic crossmodal (text→audio) doesn't work
    // reliably, so we don't send a greeting trigger here. Nova Sonic only
    // handles the conversation after the caller responds to the Telnyx greeting.
    this.scheduleRenewal();
    this.processResponseStream(response);
  }

  /** Send audio data (base64 PCM) from the caller to Bedrock. */
  sendAudio(base64Data: string): void {
    if (!this.isActive) return;
    this.audioChunkCount++;
    if (this.audioChunkCount <= 3 || this.audioChunkCount % 50 === 0) {
      this.log("info", "Audio chunk received from Go", {
        chunk: this.audioChunkCount,
        audioOutputActive: this.audioOutputActive,
        audioQueueLen: this.audioQueue.length,
        dataLen: base64Data.length,
      });
    }
    this.addEvent(
      {
        event: {
          audioInput: {
            promptName: this.promptName,
            contentName: this.audioContentId,
            content: base64Data,
          },
        },
      },
      true
    );
  }

  /** Send a tool result back to Nova Sonic. */
  sendToolResult(toolCallId: string, result: string): void {
    if (!this.isActive) return;
    this.log("info", "Sending tool result", { toolCallId });
    this.addEvent({
      event: {
        toolResult: {
          promptName: this.promptName,
          contentName: randomUUID(),
          content: result,
        },
      },
    });
  }

  /** Gracefully close the stream. */
  async close(): Promise<void> {
    if (!this.isActive) return;
    this.log("info", "Closing Nova Sonic session");

    if (this.renewalTimer) {
      clearTimeout(this.renewalTimer);
      this.renewalTimer = null;
    }

    this.addEvent({ event: { contentEnd: { promptName: this.promptName, contentName: this.audioContentId } } });
    this.addEvent({ event: { promptEnd: { promptName: this.promptName } } });
    this.addEvent({ event: { sessionEnd: {} } });

    // Give events time to flush, then force close
    await new Promise<void>((resolve) => setTimeout(resolve, 500));
    this.isActive = false;
    this.closeResolve?.();
  }

  // ── Queue management ───────────────────────────────────────────────

  /** Enqueue an event for sending; isAudio events go to the audio queue. */
  private addEvent(event: QueuedEvent, isAudio = false): void {
    if (!this.isActive) return;
    if (isAudio) {
      this.audioQueue.push(event);
    } else {
      this.queue.push(event);
    }
    this.queueResolve?.();
    this.queueResolve = null;
  }

  /**
   * Async iterable that yields queued events to the Bedrock SDK.
   * Control events are sent first; audio is held while the assistant is speaking.
   */
  private createAsyncIterable(): AsyncIterable<{ chunk: { bytes: Uint8Array } }> {
    const self = this;

    async function* generator() {
      const encoder = new TextEncoder();

      while (self.isActive) {
        // Priority 1: control/non-audio events
        if (self.queue.length > 0) {
          const evt = self.queue.shift()!;
          const jsonStr = JSON.stringify(evt);
          const eventType = Object.keys(evt.event)[0] ?? "unknown";
          if (eventType === "promptStart") {
            self.log("info", `Sending event to Bedrock: ${eventType}`, { size: jsonStr.length, payload: jsonStr.substring(0, 2000) });
          } else {
            self.log("info", `Sending event to Bedrock: ${eventType}`, { size: jsonStr.length });
          }
          yield { chunk: { bytes: encoder.encode(jsonStr) } };
          continue;
        }

        // Priority 2: audio input (only when assistant isn't speaking)
        if (self.audioQueue.length > 0 && !self.audioOutputActive) {
          const evt = self.audioQueue.shift()!;
          yield { chunk: { bytes: encoder.encode(JSON.stringify(evt)) } };
          continue;
        }

        // Wait until something changes (new event, audio state change, or close)
        await new Promise<void>((resolve) => {
          self.queueResolve = resolve;
          self.audioStateResolve = resolve;
          self.closeResolve = resolve;
        });
      }
    }

    return generator();
  }

  // ── Session setup events ───────────────────────────────────────────

  private enqueueSessionStart(): void {
    this.addEvent({
      event: {
        sessionStart: {
          inferenceConfiguration: {
            maxTokens: 1024,
            topP: 0.9,
            temperature: 0.7,
          },
        },
      },
    });
  }

  private enqueuePromptStart(): void {
    const cfg: Record<string, unknown> = {
      promptName: this.promptName,
      textOutputConfiguration: { mediaType: "text/plain" },
      audioOutputConfiguration: {
        audioType: "SPEECH",
        encoding: "base64",
        mediaType: "audio/lpcm",
        sampleRateHertz: this.config.outputSampleRate ?? 8000,
        sampleSizeBits: 16,
        channelCount: 1,
        voiceId: this.config.voice || "tiffany",
      },
    };

    // NOTE: Tools are disabled for now. When toolConfiguration is present
    // (any format — string or object schema, with or without toolChoice),
    // Nova Sonic switches to text-only mode and never emits audioOutput events.
    // Audio works perfectly without tools. We handle tool-like functionality
    // (availability, SMS, qualification) via system prompt + post-call extraction.
    // TODO: Re-enable when AWS fixes Nova Sonic tool + audio coexistence.
    if (this.config.tools.length > 0) {
      this.log("info", "Tools available but DISABLED (Nova Sonic audio incompatibility)", {
        count: this.config.tools.length,
        names: this.config.tools.map((t) => t.toolSpec?.name ?? "unknown"),
      });
    }

    this.addEvent({ event: { promptStart: cfg } });
  }

  /**
   * Sanitize tool specs for Nova Sonic. Per AWS docs, inputSchema.json
   * MUST be a JSON string (e.g. '{"type":"object",...}'), not a nested object.
   * See: docs.aws.amazon.com/nova/latest/userguide/input-events.html
   */
  private sanitizeToolSpecs(tools: ToolSpec[]): { toolSpec: { name: string; description: string; inputSchema: { json: string } } }[] {
    return tools.map((t) => ({
      toolSpec: {
        name: t.toolSpec.name,
        description: t.toolSpec.description,
        inputSchema: {
          json:
            typeof t.toolSpec.inputSchema.json === "string"
              ? (t.toolSpec.inputSchema.json as unknown as string)
              : JSON.stringify(t.toolSpec.inputSchema.json),
        },
      },
    }));
  }

  private enqueueSystemPrompt(): void {
    const contentName = randomUUID();
    this.addEvent({ event: { contentStart: { promptName: this.promptName, contentName, type: "TEXT", interactive: false, role: "SYSTEM", textInputConfiguration: { mediaType: "text/plain" } } } });
    this.addEvent({ event: { textInput: { promptName: this.promptName, contentName, content: this.config.systemPrompt } } });
    this.addEvent({ event: { contentEnd: { promptName: this.promptName, contentName } } });
  }

  /** Send a synthetic user turn so Nova Sonic knows the greeting was already handled. */
  private enqueueGreetingTrigger(): void {
    const contentName = randomUUID();
    this.addEvent({ event: { contentStart: { promptName: this.promptName, contentName, type: "TEXT", interactive: true, role: "USER", textInputConfiguration: { mediaType: "text/plain" } } } });
    this.addEvent({ event: { textInput: { promptName: this.promptName, contentName, content: "[system: the automated greeting has been played to the caller. Wait silently for the caller to speak. Do not respond until the caller says something.]" } } });
    this.addEvent({ event: { contentEnd: { promptName: this.promptName, contentName } } });
  }

  private enqueueAudioContentStart(): void {
    this.addEvent({
      event: {
        contentStart: {
          promptName: this.promptName,
          contentName: this.audioContentId,
          type: "AUDIO",
          interactive: true,
          role: "USER",
          audioInputConfiguration: {
            audioType: "SPEECH",
            encoding: "base64",
            mediaType: "audio/lpcm",
            sampleRateHertz: this.config.inputSampleRate ?? 8000,
            sampleSizeBits: 16,
            channelCount: 1,
          },
        },
      },
    });
  }

  /** Send silent audio frames to establish the audio stream before text input.
   *  Nova Sonic requires an active audio stream for crossmodal (text→audio) output.
   *  These go to the CONTROL queue (isAudio=false) so they're sent before the greeting text. */
  private enqueueSilentAudioFrames(count: number): void {
    const silentFrame = Buffer.alloc(1024).toString("base64"); // 512 samples of silence at 16-bit
    for (let i = 0; i < count; i++) {
      this.addEvent(
        {
          event: {
            audioInput: {
              promptName: this.promptName,
              contentName: this.audioContentId,
              content: silentFrame,
            },
          },
        },
        false  // control queue, NOT audio queue — must be sent before greeting text
      );
    }
  }

  /** Continuously send silent audio frames until real audio arrives from the caller.
   *  Nova Sonic requires continuous audio sampling cadence (~32ms per frame). */
  private startSilentKeepalive(): void {
    const silentFrame = Buffer.alloc(1024).toString("base64");
    const interval = setInterval(() => {
      if (!this.isActive) {
        clearInterval(interval);
        return;
      }
      // Stop sending silent frames once real audio is flowing (chunk > 10)
      if (this.audioChunkCount > 10) {
        clearInterval(interval);
        return;
      }
      this.addEvent(
        {
          event: {
            audioInput: {
              promptName: this.promptName,
              contentName: this.audioContentId,
              content: silentFrame,
            },
          },
        },
        true // OK to use audio queue now — real audio isn't flowing yet
      );
    }, 100); // every 100ms
  }

  // ── Response stream processing ─────────────────────────────────────

  /** Read and dispatch all events from the Bedrock response stream. */
  private async processResponseStream(response: InvokeModelWithBidirectionalStreamCommandOutput): Promise<void> {
    let eventCount = 0;
    try {
      for await (const event of response.body as AsyncIterable<{ chunk?: { bytes?: Uint8Array } }>) {
        if (!this.isActive) break;
        if (!event.chunk?.bytes) continue;
        eventCount++;

        const parsed = this.parseResponseEvent(event.chunk.bytes);
        if (!parsed) continue;

        const eventType = Object.keys(parsed.event ?? {})[0] ?? "unknown";
        if (eventType !== "audioOutput") {
          this.log("info", `Bedrock event #${eventCount}: ${eventType}`, {
            audioOutputActive: this.audioOutputActive,
            audioQueueLen: this.audioQueue.length,
          });
        }

        this.dispatchResponseEvent(eventType, parsed.event ?? {});
      }

      this.log("info", "Response stream ended", { totalEvents: eventCount });
    } catch (err: unknown) {
      const message = err instanceof Error ? err.message : String(err);
      this.log("error", "Stream error", { error: message });
      if (this.isActive) {
        this.callbacks.onError(`Stream error: ${message}`);
      }
    }
  }

  /** Decode and parse a raw Bedrock response chunk. Returns null on failure. */
  private parseResponseEvent(bytes: Uint8Array): BedrockResponseEvent | null {
    const text = new TextDecoder().decode(bytes);
    try {
      return JSON.parse(text) as BedrockResponseEvent;
    } catch {
      this.log("error", "Failed to parse response chunk", { text: text.substring(0, 200) });
      return null;
    }
  }

  /** Route a parsed response event to the appropriate handler. */
  private dispatchResponseEvent(eventType: string, eventData: Record<string, unknown>): void {
    switch (eventType) {
      case "contentStart":
        this.handleContentStart(eventData.contentStart as ContentStartPayload);
        break;
      case "textOutput":
        this.handleTextOutput(eventData.textOutput as TextPayload);
        break;
      case "audioOutput":
        this.handleAudioOutput(eventData.audioOutput as AudioOutputPayload);
        break;
      case "contentEnd":
        this.handleContentEnd(eventData.contentEnd as ContentEndPayload);
        break;
      case "toolUse":
        this.handleToolUse(eventData.toolUse as TextPayload);
        break;
      case "completionStart":
      case "completionEnd":
      case "usageEvent":
        break;
      default:
        this.log("warn", "Unknown event type", { eventType });
    }
  }

  // ── Event handlers ─────────────────────────────────────────────────

  private handleContentStart(data: ContentStartPayload): void {
    const contentName = data.contentName || data.contentId;
    this.log("info", "handleContentStart DETAIL", {
      contentName,
      contentId: data.contentId,
      type: data.type,
      role: data.role,
      rawKeys: Object.keys(data),
    });
    if (!contentName) return;

    if (data.type === "AUDIO") {
      this.audioOutputActive = true;
    } else if (data.type === "TEXT") {
      this.textContentBuffers.set(contentName, {
        role: data.role?.toLowerCase() || "assistant",
        text: "",
      });
    } else if (data.type === "TOOL") {
      const toolName = data.toolUseConfiguration?.toolName ?? "";
      this.toolUseBuffers.set(contentName, { toolName, input: "" });
    }
  }

  private handleTextOutput(data: TextPayload): void {
    const contentName = data.contentName || data.contentId;
    if (!contentName) return;
    const content = data.content ?? "";

    this.log("info", "handleTextOutput DETAIL", {
      contentName,
      contentLen: content.length,
      contentPreview: content.substring(0, 200),
      hasTextBuf: this.textContentBuffers.has(contentName),
      hasToolBuf: this.toolUseBuffers.has(contentName),
      allTextBufKeys: Array.from(this.textContentBuffers.keys()),
    });

    const textBuf = this.textContentBuffers.get(contentName);
    if (textBuf) {
      textBuf.text += content;
      return;
    }

    const toolBuf = this.toolUseBuffers.get(contentName);
    if (toolBuf) {
      toolBuf.input += content;
    } else {
      this.log("warn", "textOutput has NO matching buffer — creating ad-hoc TEXT buffer", { contentName });
      this.textContentBuffers.set(contentName, { role: "assistant", text: content });
    }
  }

  private handleAudioOutput(data: AudioOutputPayload): void {
    if (data.content) {
      this.callbacks.onAudio(data.content);
    }
  }

  private handleToolUse(data: TextPayload): void {
    const contentName = data.contentName || data.contentId;
    if (!contentName) return;
    const toolBuf = this.toolUseBuffers.get(contentName);
    if (toolBuf) {
      toolBuf.input += data.content ?? "";
    }
  }

  private handleContentEnd(data: ContentEndPayload): void {
    const contentName = data.contentName || data.contentId;
    this.log("info", "handleContentEnd DETAIL", {
      contentName,
      type: data.type,
      rawKeys: Object.keys(data),
      rawData: JSON.stringify(data).substring(0, 500),
      hasTextBuf: contentName ? this.textContentBuffers.has(contentName) : false,
      hasToolBuf: contentName ? this.toolUseBuffers.has(contentName) : false,
    });
    if (!contentName) return;

    if (data.type === "AUDIO") {
      this.audioOutputActive = false;
      this.audioStateResolve?.();
      this.audioStateResolve = null;
      // Check if there's a text buffer for this contentName (fallback for mismatched types)
      if (this.textContentBuffers.has(contentName)) {
        this.log("warn", "contentEnd type=AUDIO but text buffer exists — emitting transcript anyway", { contentName });
        this.emitTextTranscript(contentName);
      }
    } else if (data.type === "TEXT") {
      this.emitTextTranscript(contentName);
    } else if (data.type === "TOOL") {
      this.emitToolCall(contentName, data);
    } else {
      // Unknown or missing type — check if we have a pending text buffer
      if (this.textContentBuffers.has(contentName)) {
        this.log("warn", `contentEnd type=${data.type} but text buffer exists — emitting transcript`, { contentName });
        this.emitTextTranscript(contentName);
      } else if (this.toolUseBuffers.has(contentName)) {
        this.log("warn", `contentEnd type=${data.type} but tool buffer exists — emitting tool call`, { contentName });
        this.emitToolCall(contentName, data);
      }
    }
  }

  /** Emit a transcript callback for a completed text content block. */
  private emitTextTranscript(contentName: string): void {
    const textBuf = this.textContentBuffers.get(contentName);
    this.log("info", "emitTextTranscript FIRING", {
      contentName,
      hasBuffer: !!textBuf,
      text: textBuf?.text?.substring(0, 200) ?? "(none)",
      role: textBuf?.role ?? "(none)",
    });
    if (textBuf?.text) {
      const role = textBuf.role === "user" ? "user" as const : "assistant" as const;
      // Deduplicate — Nova Sonic sends each response as TEXT + AUDIO with same text
      const normalized = textBuf.text.trim().replace(/\s+/g, " ");
      if (normalized && role === "assistant" && this.recentAssistantTexts.has(normalized)) {
        this.log("info", "Skipping duplicate assistant text", { text: normalized.substring(0, 80) });
      } else if (normalized) {
        this.callbacks.onTranscript(role, textBuf.text);
        if (role === "assistant") {
          this.lastEmittedTranscript = normalized;
          this.recentAssistantTexts.add(normalized);
          // Keep set bounded — clear after 20 entries
          if (this.recentAssistantTexts.size > 20) {
            const first = this.recentAssistantTexts.values().next().value;
            if (first) this.recentAssistantTexts.delete(first);
          }
        }
      }
    }
    this.textContentBuffers.delete(contentName);
  }

  /** Emit a tool call callback for a completed tool content block. */
  private emitToolCall(contentName: string, data: ContentEndPayload): void {
    const toolBuf = this.toolUseBuffers.get(contentName);
    if (toolBuf) {
      let input: Record<string, unknown> = {};
      try {
        input = JSON.parse(toolBuf.input);
      } catch {
        input = { raw: toolBuf.input };
      }
      const toolCallId =
        data.toolUseConfiguration?.toolUseId ??
        data.toolResultInputConfiguration?.toolUseId ??
        contentName;
      this.log("info", "Tool call received", { toolCallId, toolName: toolBuf.toolName });
      this.callbacks.onToolCall(toolCallId, toolBuf.toolName, input);
    }
    this.toolUseBuffers.delete(contentName);
  }

  // ── Session renewal ────────────────────────────────────────────────

  /** Schedule automatic session renewal before the 8-minute limit. */
  private scheduleRenewal(): void {
    const renewIn = SESSION_MAX_MS - RENEWAL_BUFFER_MS;
    this.renewalTimer = setTimeout(() => {
      this.renewSession().catch((err: unknown) => {
        const message = err instanceof Error ? err.message : String(err);
        this.log("error", "Session renewal failed", { error: message });
        this.callbacks.onError("Session renewal failed");
      });
    }, renewIn);
  }

  /** Tear down the current session and start a fresh one. */
  private async renewSession(): Promise<void> {
    this.log("info", "Renewing Nova Sonic session");

    this.addEvent({ event: { contentEnd: { promptName: this.promptName, contentName: this.audioContentId } } });
    this.addEvent({ event: { promptEnd: { promptName: this.promptName } } });
    this.addEvent({ event: { sessionEnd: {} } });

    await new Promise<void>((r) => setTimeout(r, 300));
    this.isActive = false;
    this.closeResolve?.();

    // Reset state
    this.promptName = randomUUID();
    this.audioContentId = randomUUID();
    this.queue = [];
    this.audioQueue = [];
    this.audioOutputActive = false;
    this.audioChunkCount = 0;
    this.textContentBuffers.clear();
    this.toolUseBuffers.clear();

    await this.start();
    this.callbacks.onSessionRenewed();
  }
}
