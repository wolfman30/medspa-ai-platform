/**
 * Nova Sonic bidirectional streaming client.
 * Manages a single Bedrock InvokeModelWithBidirectionalStream session.
 */

import {
  BedrockRuntimeClient,
  InvokeModelWithBidirectionalStreamCommand,
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

const MODEL_ID = "amazon.nova-sonic-v1:0";
const SESSION_MAX_MS = 8 * 60 * 1000; // 8 minutes
const RENEWAL_BUFFER_MS = 30 * 1000; // renew 30s before expiry

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

interface QueuedEvent {
  event: Record<string, unknown>;
}

export class NovaSonicClient {
  private callId: string;
  private config: NovaSonicConfig;
  private callbacks: NovaSonicCallbacks;
  private client: BedrockRuntimeClient;

  private promptName = randomUUID();
  private audioContentId = randomUUID();

  private queue: QueuedEvent[] = [];
  private audioQueue: QueuedEvent[] = [];
  private isActive = false;
  private sessionStartTime = 0;
  private renewalTimer: ReturnType<typeof setTimeout> | null = null;

  // Signaling for async iterator
  private queueResolve: (() => void) | null = null;
  private closeResolve: (() => void) | null = null;

  // Track output audio state to hold input while assistant speaks
  private audioOutputActive = false;
  private audioStateResolve: (() => void) | null = null;

  // Track text content for transcripts
  private textContentBuffers = new Map<string, { role: string; text: string }>();
  // Track tool use content
  private toolUseBuffers = new Map<string, { toolName: string; input: string }>();
  private pendingToolContentType = new Map<string, "toolUse" | "text" | "audio">();

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

  private log(level: string, msg: string, extra?: Record<string, unknown>) {
    const ts = new Date().toISOString();
    const payload = JSON.stringify({ ts, level, callId: this.callId, msg, ...extra });
    if (level === "error") console.error(payload);
    else console.log(payload);
  }

  /** Start the bidirectional stream. */
  async start(): Promise<void> {
    this.isActive = true;
    this.sessionStartTime = Date.now();

    this.enqueueSessionStart();
    this.enqueuePromptStart();
    this.enqueueSystemPrompt();
    this.enqueueAudioContentStart();

    const asyncIterable = this.createAsyncIterable();

    const response = await this.client.send(
      new InvokeModelWithBidirectionalStreamCommand({
        modelId: MODEL_ID,
        body: asyncIterable,
      })
    );

    this.log("info", "Bedrock stream established");
    this.scheduleRenewal();
    this.processResponseStream(response);
  }

  /** Send audio data (base64 PCM). */
  sendAudio(base64Data: string): void {
    if (!this.isActive) return;
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

    const toolResultContentId = randomUUID();

    this.addEvent({
      event: {
        contentStart: {
          promptName: this.promptName,
          contentName: toolResultContentId,
          type: "TOOL_RESULT",
          interactive: false,
          role: "TOOL",
          toolResultInputConfiguration: {
            toolUseId: toolCallId,
            type: "TEXT",
            textInputConfiguration: { mediaType: "text/plain" },
          },
        },
      },
    });

    this.addEvent({
      event: {
        textInput: {
          promptName: this.promptName,
          contentName: toolResultContentId,
          content: result,
        },
      },
    });

    this.addEvent({
      event: {
        contentEnd: {
          promptName: this.promptName,
          contentName: toolResultContentId,
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

    // Send contentEnd, promptEnd, sessionEnd
    this.addEvent({
      event: {
        contentEnd: {
          promptName: this.promptName,
          contentName: this.audioContentId,
        },
      },
    });

    this.addEvent({
      event: { promptEnd: { promptName: this.promptName } },
    });

    this.addEvent({
      event: { sessionEnd: {} },
    });

    // Give events time to flush, then force close
    await new Promise<void>((resolve) => setTimeout(resolve, 500));
    this.isActive = false;
    this.closeResolve?.();
  }

  // ---- Queue management ----

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

  private createAsyncIterable(): AsyncIterable<{ chunk: { bytes: Uint8Array } }> {
    const self = this;
    return {
      [Symbol.asyncIterator]() {
        return {
          async next() {
            while (self.isActive) {
              // Non-audio events first
              if (self.queue.length > 0) {
                const evt = self.queue.shift()!;
                return {
                  value: { chunk: { bytes: new TextEncoder().encode(JSON.stringify(evt)) } },
                  done: false,
                };
              }

              // Audio events only when output audio is not active
              if (self.audioQueue.length > 0 && !self.audioOutputActive) {
                const evt = self.audioQueue.shift()!;
                return {
                  value: { chunk: { bytes: new TextEncoder().encode(JSON.stringify(evt)) } },
                  done: false,
                };
              }

              // Wait for signal
              await new Promise<void>((resolve) => {
                self.queueResolve = resolve;
                self.audioStateResolve = resolve;
                self.closeResolve = resolve;
              });
            }
            return { value: undefined, done: true as const };
          },
          async return() {
            self.isActive = false;
            return { value: undefined, done: true as const };
          },
        };
      },
    };
  }

  // ---- Session events ----

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

    if (this.config.tools.length > 0) {
      cfg.toolUseOutputConfiguration = { mediaType: "application/json" };
      cfg.toolConfiguration = { tools: this.config.tools };
    }

    this.addEvent({ event: { promptStart: cfg } });
  }

  private enqueueSystemPrompt(): void {
    const contentName = randomUUID();
    this.addEvent({
      event: {
        contentStart: {
          promptName: this.promptName,
          contentName,
          type: "TEXT",
          interactive: false,
          role: "SYSTEM",
          textInputConfiguration: { mediaType: "text/plain" },
        },
      },
    });
    this.addEvent({
      event: {
        textInput: {
          promptName: this.promptName,
          contentName,
          content: this.config.systemPrompt,
        },
      },
    });
    this.addEvent({
      event: {
        contentEnd: { promptName: this.promptName, contentName },
      },
    });
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

  // ---- Response processing ----

  private async processResponseStream(response: any): Promise<void> {
    try {
      for await (const event of response.body) {
        if (!this.isActive) break;
        if (!event.chunk?.bytes) continue;

        const text = new TextDecoder().decode(event.chunk.bytes);
        let json: any;
        try {
          json = JSON.parse(text);
        } catch {
          this.log("error", "Failed to parse response chunk", { text });
          continue;
        }

        const eventType = Object.keys(json.event || {})[0] || "unknown";

        switch (eventType) {
          case "contentStart":
            this.handleContentStart(json.event.contentStart);
            break;
          case "textOutput":
            this.handleTextOutput(json.event.textOutput);
            break;
          case "audioOutput":
            this.handleAudioOutput(json.event.audioOutput);
            break;
          case "contentEnd":
            this.handleContentEnd(json.event.contentEnd);
            break;
          case "toolUse":
            this.handleToolUse(json.event.toolUse);
            break;
          case "completionStart":
          case "completionEnd":
          case "usageEvent":
            break;
          default:
            this.log("warn", "Unknown event type", { eventType });
        }
      }

      this.log("info", "Response stream ended");
    } catch (err: any) {
      this.log("error", "Stream error", { error: err.message });
      if (this.isActive) {
        this.callbacks.onError(`Stream error: ${err.message}`);
      }
    }
  }

  private handleContentStart(data: any): void {
    const contentName = data?.contentName;
    const type = data?.type;

    if (type === "AUDIO") {
      this.audioOutputActive = true;
    } else if (type === "TEXT") {
      this.textContentBuffers.set(contentName, { role: data?.role?.toLowerCase() || "assistant", text: "" });
      this.pendingToolContentType.set(contentName, "text");
    } else if (type === "TOOL") {
      const toolName = data?.toolUseConfiguration?.toolName || "";
      this.toolUseBuffers.set(contentName, { toolName, input: "" });
      this.pendingToolContentType.set(contentName, "toolUse");
    }
  }

  private handleTextOutput(data: any): void {
    const contentName = data?.contentName;
    const content = data?.content || "";

    const textBuf = this.textContentBuffers.get(contentName);
    if (textBuf) {
      textBuf.text += content;
      return;
    }

    const toolBuf = this.toolUseBuffers.get(contentName);
    if (toolBuf) {
      toolBuf.input += content;
    }
  }

  private handleAudioOutput(data: any): void {
    const content = data?.content;
    if (content) {
      this.callbacks.onAudio(content);
    }
  }

  private handleToolUse(data: any): void {
    const contentName = data?.contentName;
    const content = data?.content || "";
    const toolBuf = this.toolUseBuffers.get(contentName);
    if (toolBuf) {
      toolBuf.input += content;
    }
  }

  private handleContentEnd(data: any): void {
    const contentName = data?.contentName;
    const type = data?.type;

    if (type === "AUDIO") {
      this.audioOutputActive = false;
      this.audioStateResolve?.();
      this.audioStateResolve = null;
    } else if (type === "TEXT") {
      const textBuf = this.textContentBuffers.get(contentName);
      if (textBuf && textBuf.text) {
        const role = textBuf.role === "user" ? "user" : "assistant";
        this.callbacks.onTranscript(role, textBuf.text);
      }
      this.textContentBuffers.delete(contentName);
      this.pendingToolContentType.delete(contentName);
    } else if (type === "TOOL") {
      const toolBuf = this.toolUseBuffers.get(contentName);
      if (toolBuf) {
        let input: Record<string, unknown> = {};
        try {
          input = JSON.parse(toolBuf.input);
        } catch {
          input = { raw: toolBuf.input };
        }
        const toolCallId = data?.toolUseConfiguration?.toolUseId || data?.toolResultInputConfiguration?.toolUseId || contentName;
        this.callbacks.onToolCall(toolCallId, toolBuf.toolName, input);
      }
      this.toolUseBuffers.delete(contentName);
      this.pendingToolContentType.delete(contentName);
    }
  }

  // ---- Session renewal ----

  private scheduleRenewal(): void {
    const renewIn = SESSION_MAX_MS - RENEWAL_BUFFER_MS;
    this.renewalTimer = setTimeout(() => {
      this.renewSession().catch((err) => {
        this.log("error", "Session renewal failed", { error: err.message });
        this.callbacks.onError("Session renewal failed");
      });
    }, renewIn);
  }

  private async renewSession(): Promise<void> {
    this.log("info", "Renewing Nova Sonic session");

    // Close current audio content, prompt, and session
    this.addEvent({
      event: { contentEnd: { promptName: this.promptName, contentName: this.audioContentId } },
    });
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
    this.textContentBuffers.clear();
    this.toolUseBuffers.clear();
    this.pendingToolContentType.clear();

    // Start fresh
    await this.start();
    this.callbacks.onSessionRenewed();
  }
}
