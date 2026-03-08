/**
 * ElevenLabs streaming TTS client via WebSocket.
 * Accepts text, streams back PCM L16 audio at 8kHz for Telnyx.
 */

import { WebSocket } from "ws";

const DEFAULT_VOICE_ID = "l4Coq6695JDX9xtLqXDE"; // Lauren B
const DEFAULT_MODEL_ID = "eleven_flash_v2_5";

export interface ElevenLabsConfig {
  apiKey: string;
  voiceId?: string;
  modelId?: string;
  stability?: number;
  similarityBoost?: number;
  speed?: number;
  outputSampleRate?: number;
}

export interface ElevenLabsCallbacks {
  /** Called with base64-encoded PCM L16 audio chunks */
  onAudio: (base64Audio: string) => void;
  onError: (message: string) => void;
  onDone: () => void;
}

interface ElevenLabsResponse {
  audio?: string; // base64 PCM
  isFinal?: boolean;
  error?: { message: string; code?: string };
}

/**
 * A single ElevenLabs streaming TTS session.
 * Create one per utterance. Send text chunks, then flush.
 */
export class ElevenLabsSession {
  private ws: WebSocket | null = null;
  private callbacks: ElevenLabsCallbacks;
  private closed = false;
  private config: Required<ElevenLabsConfig>;
  private callId: string;

  constructor(callId: string, config: ElevenLabsConfig, callbacks: ElevenLabsCallbacks) {
    this.callId = callId;
    this.callbacks = callbacks;
    this.config = {
      apiKey: config.apiKey,
      voiceId: config.voiceId || DEFAULT_VOICE_ID,
      modelId: config.modelId || DEFAULT_MODEL_ID,
      stability: config.stability ?? 0.5,
      similarityBoost: config.similarityBoost ?? 0.75,
      speed: config.speed ?? 1.0,
      outputSampleRate: config.outputSampleRate ?? 8000,
    };
  }

  /** Open the WebSocket connection and send BOS. */
  connect(): Promise<void> {
    return new Promise((resolve, reject) => {
      const url =
        `wss://api.elevenlabs.io/v1/text-to-speech/${this.config.voiceId}/stream-input` +
        `?model_id=${this.config.modelId}&output_format=pcm_${this.config.outputSampleRate}`;

      this.ws = new WebSocket(url, { headers: { "xi-api-key": this.config.apiKey } });

      this.ws.on("open", () => {
        // Send BOS (beginning of stream)
        const bos = {
          text: " ",
          xi_api_key: this.config.apiKey,
          voice_settings: {
            stability: this.config.stability,
            similarity_boost: this.config.similarityBoost,
            speed: this.config.speed,
          },
          generation_config: {
            chunk_length_schedule: [120, 160, 250, 290],
          },
        };
        this.ws!.send(JSON.stringify(bos));
        this.log("info", "ElevenLabs session connected");
        resolve();
      });

      this.ws.on("message", (data) => {
        try {
          const resp: ElevenLabsResponse = JSON.parse(data.toString());

          if (resp.error) {
            this.log("error", `ElevenLabs API error: ${resp.error.message}`);
            this.callbacks.onError(`ElevenLabs: ${resp.error.message}`);
            return;
          }

          if (resp.audio && resp.audio !== "") {
            // ElevenLabs returns raw PCM bytes as base64.
            // Forward directly — it's already L16 at our target sample rate.
            this.callbacks.onAudio(resp.audio);
          }

          if (resp.isFinal) {
            this.log("info", "ElevenLabs stream complete");
            this.callbacks.onDone();
          }
        } catch (err) {
          this.log("error", `ElevenLabs parse error: ${err}`);
        }
      });

      this.ws.on("error", (err) => {
        this.log("error", `ElevenLabs WebSocket error: ${err.message}`);
        if (!this.closed) {
          this.callbacks.onError(`ElevenLabs connection error: ${err.message}`);
        }
        reject(err);
      });

      this.ws.on("close", () => {
        this.log("info", "ElevenLabs WebSocket closed");
        this.closed = true;
      });
    });
  }

  /** Send a text chunk for synthesis. */
  sendText(text: string): void {
    if (this.closed || !this.ws || this.ws.readyState !== WebSocket.OPEN) return;
    this.ws.send(JSON.stringify({ text }));
  }

  /** Signal end of text input — flushes remaining audio. */
  flush(): void {
    if (this.closed || !this.ws || this.ws.readyState !== WebSocket.OPEN) return;
    this.ws.send(JSON.stringify({ text: "" }));
  }

  /** Close the session immediately. */
  close(): void {
    if (this.closed) return;
    this.closed = true;
    if (this.ws) {
      this.ws.close();
      this.ws = null;
    }
  }

  private log(level: string, msg: string): void {
    const ts = new Date().toISOString();
    const payload = JSON.stringify({ ts, level, callId: this.callId, msg });
    if (level === "error") console.error(payload);
    else console.log(payload);
  }
}
