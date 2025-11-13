# LangChain Orchestrator

This FastAPI microservice wraps our LangChain toolchain, Astra DB vector store, and OpenAI models so the Go workers can focus on job orchestration.

## Features

- `/v1/knowledge/{clinic_id}` ingests clinic docs, chunks them, and persists embeddings to Astra DB.
- `/v1/conversations/respond` accepts the full chat history + metadata, runs a retrieval-augmented prompt, and returns an assistant reply in <10s.
- `/healthz` lightweight readiness probe for ECS/LB.

## Environment

| Variable | Description |
|----------|-------------|
| `OPENAI_API_KEY` | Required OpenAI key (shared with GOP worker). |
| `OPENAI_API_BASE` | Optional Azure / proxy base URL. |
| `OPENAI_MODEL` | Chat model (`gpt-4o-mini` by default). |
| `OPENAI_EMBED_MODEL` | Embedding model (`text-embedding-3-small`). |
| `RAG_TOP_K` | Number of snippets per response (default `4`). |
| `ASTRA_DB_API_ENDPOINT` | Astra DB REST endpoint. |
| `ASTRA_DB_APPLICATION_TOKEN` | Astra application token. |
| `ASTRA_DB_KEYSPACE` | Keyspace / namespace name. |
| `ASTRA_DB_COLLECTION` | Collection/table for knowledge chunks. |
| `PORT` | Bind port (default `8081`). |

Create a `.env` next to `app.py` or export variables before running.

## Local Dev

```bash
cd orchestrator
python -m venv .venv
source .venv/bin/activate  # or .venv\Scripts\activate on Windows
pip install -r requirements.txt
cp ../.env.example .env  # update with real keys + Astra creds
uvicorn app:app --reload --port 8081
```

Send a smoke request:

```bash
curl -X POST http://localhost:8081/v1/knowledge/demo \
  -H "Content-Type: application/json" \
  -d '{"documents":["We charge a $50 refundable deposit for injectables."]}'

curl -X POST http://localhost:8081/v1/conversations/respond \
  -H "Content-Type: application/json" \
  -d '{
        "conversation_id":"demo-1",
        "clinic_id":"demo",
        "history":[
          {"role":"system","content":"MedSpa context"},
          {"role":"user","content":"Hi, do you have Botox this week?"}
        ],
        "latest_input":"Hi, do you have Botox this week?"
      }'
```

## Deployment Notes

- Package as its own container (ECR repo `medspa-langchain-orchestrator`).
- Requires Astra DB (Vector) credentials + network egress to OpenAI.
- Scale horizontally; service is stateless aside from Astra DB + OpenAI calls.
