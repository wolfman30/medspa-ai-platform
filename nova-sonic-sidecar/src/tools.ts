/** Tool definitions for Nova Sonic in Bedrock format. */

export interface ToolSpec {
  toolSpec: {
    name: string;
    description: string;
    inputSchema: {
      json: Record<string, unknown>;
    };
  };
}

export const DEFAULT_TOOLS: ToolSpec[] = [
  {
    toolSpec: {
      name: "check_availability",
      description:
        "Check available appointment times for a service at the clinic",
      inputSchema: {
        json: {
          type: "object",
          properties: {
            service: {
              type: "string",
              description: "Service name (e.g., Botox, Lip Filler)",
            },
            preferred_days: { type: "string" },
            preferred_times: { type: "string" },
            provider_preference: { type: "string" },
          },
          required: ["service"],
        },
      },
    },
  },
  {
    toolSpec: {
      name: "get_clinic_info",
      description:
        "Get clinic information: services, pricing, policies, providers",
      inputSchema: {
        json: {
          type: "object",
          properties: {
            query: {
              type: "string",
              description: "What information to look up",
            },
          },
          required: ["query"],
        },
      },
    },
  },
  {
    toolSpec: {
      name: "send_sms",
      description:
        "Send an SMS to the caller with booking link, time slots, or other info",
      inputSchema: {
        json: {
          type: "object",
          properties: {
            message: {
              type: "string",
              description: "SMS content to send",
            },
          },
          required: ["message"],
        },
      },
    },
  },
  {
    toolSpec: {
      name: "save_qualification",
      description:
        "Save patient qualification data (name, patient type, preferences)",
      inputSchema: {
        json: {
          type: "object",
          properties: {
            name: { type: "string" },
            patient_type: {
              type: "string",
              enum: ["new", "returning"],
            },
            service: { type: "string" },
            preferred_days: { type: "string" },
            preferred_times: { type: "string" },
            provider_preference: { type: "string" },
          },
        },
      },
    },
  },
];
