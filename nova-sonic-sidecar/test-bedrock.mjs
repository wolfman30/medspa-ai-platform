import {
  BedrockRuntimeClient,
  InvokeModelWithBidirectionalStreamCommand,
} from "@aws-sdk/client-bedrock-runtime";
import { NodeHttp2Handler } from "@smithy/node-http-handler";
import { randomUUID } from "node:crypto";

const handler = new NodeHttp2Handler({
  requestTimeout: 300_000,
  sessionTimeout: 300_000,
});

const client = new BedrockRuntimeClient({
  region: process.env.AWS_REGION || "us-east-1",
  requestHandler: handler,
});

const promptName = randomUUID();
const contentName = randomUUID();
const audioContentId = randomUUID();

const events = [
  { event: { sessionStart: { inferenceConfiguration: { maxTokens: 1024, topP: 0.9, temperature: 0.7 } } } },
  { event: { promptStart: { promptName, textOutputConfiguration: { mediaType: "text/plain" }, audioOutputConfiguration: { audioType: "SPEECH", encoding: "base64", mediaType: "audio/lpcm", sampleRateHertz: 8000, sampleSizeBits: 16, channelCount: 1, voiceId: "tiffany" } } } },
  { event: { contentStart: { promptName, contentName, type: "TEXT", interactive: true, role: "SYSTEM", textInputConfiguration: { mediaType: "text/plain" } } } },
  { event: { textInput: { promptName, contentName, content: "You are a friendly assistant." } } },
  { event: { contentEnd: { promptName, contentName } } },
  { event: { contentStart: { promptName, contentName: audioContentId, type: "AUDIO", interactive: true, role: "USER", audioInputConfiguration: { audioType: "SPEECH", encoding: "base64", mediaType: "audio/lpcm", sampleRateHertz: 8000, sampleSizeBits: 16, channelCount: 1 } } } },
];

let eventIndex = 0;

async function* generateStream() {
  for (const evt of events) {
    const json = JSON.stringify(evt);
    console.log(`Sending event ${eventIndex++}: ${Object.keys(evt.event)[0]} (${json.length} bytes)`);
    yield { chunk: { bytes: new TextEncoder().encode(json) } };
  }
  // Keep alive for a bit
  await new Promise(r => setTimeout(r, 5000));
}

try {
  console.log("Sending InvokeModelWithBidirectionalStreamCommand...");
  const response = await client.send(
    new InvokeModelWithBidirectionalStreamCommand({
      modelId: "amazon.nova-sonic-v1:0",
      body: generateStream(),
    })
  );
  console.log("Stream established!");
  
  let count = 0;
  for await (const event of response.body) {
    if (event.chunk?.bytes) {
      const text = new TextDecoder().decode(event.chunk.bytes);
      const parsed = JSON.parse(text);
      const type = Object.keys(parsed.event || {})[0] || "unknown";
      console.log(`Response event ${count++}: ${type}`);
      if (count > 5) break;
    }
  }
} catch (e) {
  console.error("Error:", e.message);
}

process.exit(0);
