import os
import time
from typing import Dict, List, Literal, Optional, Tuple

from fastapi import FastAPI, HTTPException
from fastapi.middleware.cors import CORSMiddleware
from langchain_astradb import AstraDBVectorStore
from langchain_core.messages import AIMessage, HumanMessage, SystemMessage
from langchain_core.output_parsers import StrOutputParser
from langchain_core.prompts import ChatPromptTemplate, MessagesPlaceholder
from langchain_openai import ChatOpenAI, OpenAIEmbeddings
from langchain_text_splitters import RecursiveCharacterTextSplitter
from pydantic import BaseModel, Field
from dotenv import load_dotenv

load_dotenv()

DEFAULT_CLINIC_ID = "global"
SYSTEM_PROMPT = (
    "You are MedSpa AI Concierge, a warm, trustworthy assistant for a medical spa. "
    "Use the clinic intel below plus prior conversation history to craft short, "
    "actionable replies that stay HIPAA compliant. Always guide clients toward booking, "
    "clarify goals, and share relevant prep or deposit expectations when helpful."
)


def env(name: str, default: Optional[str] = None, required: bool = False) -> Optional[str]:
    value = os.getenv(name)
    if value is None or value == "":
        if required and default in (None, ""):
            raise RuntimeError(f"{name} must be set")
        return default
    return value


OPENAI_API_KEY = env("OPENAI_API_KEY", required=True)
OPENAI_BASE_URL = env("OPENAI_API_BASE")
OPENAI_PROJECT = env("OPENAI_PROJECT")
OPENAI_ORG = env("OPENAI_ORG")
CHAT_MODEL = env("OPENAI_MODEL", "gpt-4o-mini")
EMBED_MODEL = env("OPENAI_EMBED_MODEL", "text-embedding-3-small")
RAG_TOP_K = int(env("RAG_TOP_K", "4"))

ASTRA_ENDPOINT = env("ASTRA_DB_API_ENDPOINT", required=True)
ASTRA_TOKEN = env("ASTRA_DB_APPLICATION_TOKEN", required=True)
ASTRA_KEYSPACE = env("ASTRA_DB_KEYSPACE", required=True)
ASTRA_COLLECTION = env("ASTRA_DB_COLLECTION", "medspa_knowledge")

default_headers = {}
if OPENAI_PROJECT:
    default_headers["OpenAI-Project"] = OPENAI_PROJECT
if OPENAI_ORG:
    default_headers["OpenAI-Organization"] = OPENAI_ORG
if not default_headers:
    default_headers = None

embedding_kwargs = {
    "api_key": OPENAI_API_KEY,
    "base_url": OPENAI_BASE_URL,
    "model": EMBED_MODEL,
}
if default_headers:
    embedding_kwargs["default_headers"] = default_headers

embeddings = OpenAIEmbeddings(**embedding_kwargs)

vector_store = AstraDBVectorStore(
    collection_name=ASTRA_COLLECTION,
    api_endpoint=ASTRA_ENDPOINT,
    token=ASTRA_TOKEN,
    namespace=ASTRA_KEYSPACE,
    embedding=embeddings,
)

text_splitter = RecursiveCharacterTextSplitter(chunk_size=600, chunk_overlap=80)
chat_kwargs = {
    "api_key": OPENAI_API_KEY,
    "base_url": OPENAI_BASE_URL,
    "model": CHAT_MODEL,
    "temperature": float(env("OPENAI_TEMPERATURE", "0.2")),
    "timeout": 40,
}
if default_headers:
    chat_kwargs["default_headers"] = default_headers
chat_llm = ChatOpenAI(**chat_kwargs)

prompt = ChatPromptTemplate.from_messages(
    [
        ("system", SYSTEM_PROMPT),
        ("system", "Clinic intel:\n{context}"),
        MessagesPlaceholder(variable_name="history"),
        ("human", "{latest_input}"),
    ]
)
conversation_chain = prompt | chat_llm | StrOutputParser()

app = FastAPI(title="MedSpa LangChain Orchestrator", version="0.1.0")
app.add_middleware(
    CORSMiddleware,
    allow_origins=["*"],
    allow_credentials=True,
    allow_methods=["*"],
    allow_headers=["*"],
)


class KnowledgePayload(BaseModel):
    documents: List[str] = Field(..., min_items=1, max_items=50)


class ConversationMessage(BaseModel):
    role: Literal["system", "user", "assistant"]
    content: str


class ConversationRequest(BaseModel):
    conversation_id: str
    clinic_id: Optional[str] = None
    org_id: Optional[str] = None
    lead_id: Optional[str] = None
    channel: Optional[str] = None
    metadata: Optional[Dict[str, str]] = None
    latest_input: Optional[str] = None
    history: List[ConversationMessage] = Field(..., min_items=1)


class ConversationResponse(BaseModel):
    message: str
    contexts: List[str]
    latency_ms: int


@app.get("/healthz")
def healthz() -> Dict[str, str]:
    return {"status": "ok"}


@app.post("/v1/knowledge/{clinic_id}", status_code=201)
def ingest_knowledge(clinic_id: str, payload: KnowledgePayload) -> Dict[str, int]:
    slug = clinic_id.strip().lower() or DEFAULT_CLINIC_ID
    chunks = []
    for text in payload.documents:
        cleaned = text.strip()
        if not cleaned:
            continue
        documents = text_splitter.create_documents([cleaned])
        for doc in documents:
            doc.metadata["clinic_id"] = slug
            chunks.append(doc)
    if not chunks:
        raise HTTPException(status_code=400, detail="documents produced no content")

    vector_store.add_documents(chunks)
    return {"documents": len(chunks)}


@app.post("/v1/conversations/respond", response_model=ConversationResponse)
def respond(req: ConversationRequest) -> ConversationResponse:
    latest = req.latest_input or extract_latest_user(req.history)
    if not latest:
        raise HTTPException(status_code=400, detail="latest_input or user history required")

    clinic_id = (req.clinic_id or DEFAULT_CLINIC_ID).strip().lower() or DEFAULT_CLINIC_ID
    history_msgs = to_langchain_messages(req.history)
    context_text, snippets = retrieve_context(clinic_id, latest)

    start = time.perf_counter()
    message = conversation_chain.invoke(
        {
            "context": context_text or "No clinic-specific intel available.",
            "history": history_msgs,
            "latest_input": latest,
        }
    )
    latency_ms = int((time.perf_counter() - start) * 1000)

    return ConversationResponse(
        message=message.strip(),
        contexts=snippets,
        latency_ms=latency_ms,
    )


def retrieve_context(clinic_id: str, query: str) -> Tuple[str, List[str]]:
    docs = search_docs(clinic_id, query)
    if len(docs) < RAG_TOP_K and clinic_id != DEFAULT_CLINIC_ID:
        docs += search_docs(DEFAULT_CLINIC_ID, query)

    snippets: List[str] = []
    seen = set()
    for doc in docs:
        text = doc.page_content.strip()
        if not text or text in seen:
            continue
        seen.add(text)
        snippets.append(text)
        if len(snippets) >= RAG_TOP_K:
            break

    context_lines = [f"{idx + 1}. {snippet}" for idx, snippet in enumerate(snippets)]
    context_text = "\n".join(context_lines)
    return context_text, snippets


def search_docs(clinic_id: str, query: str):
    retriever = vector_store.as_retriever(
        search_kwargs={"k": RAG_TOP_K, "filter": {"clinic_id": clinic_id}}
    )
    return retriever.get_relevant_documents(query)


def to_langchain_messages(history: List[ConversationMessage]):
    messages = []
    for item in history:
        content = item.content.strip()
        if not content:
            continue
        if item.role == "system":
            messages.append(SystemMessage(content=content))
        elif item.role == "assistant":
            messages.append(AIMessage(content=content))
        else:
            messages.append(HumanMessage(content=content))
    return messages


def extract_latest_user(history: List[ConversationMessage]) -> Optional[str]:
    for item in reversed(history):
        if item.role == "user" and item.content.strip():
            return item.content.strip()
    return None


if __name__ == "__main__":
    import uvicorn

    uvicorn.run(
        "app:app",
        host="0.0.0.0",
        port=int(env("PORT", "8081")),
        reload=env("RELOAD", "false").lower() == "true",
    )
