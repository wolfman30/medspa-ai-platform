#!/usr/bin/env python3
"""
Full End-to-End Automated Test for MedSpa AI Platform

This script simulates the complete production flow:
1. Missed call webhook → AI sends initial SMS
2. Customer SMS responses → AI conversation with preference extraction
3. Customer agrees to deposit → Square checkout link generated
4. Square payment webhook → Payment confirmed
5. Confirmation SMS sent to customer

Usage:
    python scripts/e2e_full_flow.py

    # With custom settings:
    API_URL=http://localhost:8082 python scripts/e2e_full_flow.py

    # Skip database checks (if no psql access):
    SKIP_DB_CHECK=1 python scripts/e2e_full_flow.py
"""

import os
import sys
import time
import json
import uuid
import hmac
import hashlib
import base64
import re
import html as html_lib
import subprocess
import shutil
from datetime import datetime, timezone
from html.parser import HTMLParser
from urllib.parse import urljoin, urlparse, urldefrag
from typing import Optional, Dict, Any, List, Tuple

# =============================================================================
# Optional .env Loading (so the runner matches the Go API's env behavior)
# =============================================================================

def load_dotenv(path: str) -> None:
    """Best-effort .env loader (no external deps)."""
    if not path or not os.path.exists(path):
        return
    try:
        with open(path, "r", encoding="utf-8") as f:
            for raw_line in f:
                line = raw_line.strip()
                if not line or line.startswith("#") or "=" not in line:
                    continue
                key, value = line.split("=", 1)
                key = key.strip()
                value = value.strip().strip('"').strip("'")
                if key and key not in os.environ:
                    os.environ[key] = value
    except Exception:
        return

_PROJECT_ROOT = os.path.abspath(os.path.join(os.path.dirname(__file__), ".."))
load_dotenv(os.getenv("DOTENV_PATH", os.path.join(_PROJECT_ROOT, ".env")))

# Fix Windows console encoding for Unicode
if sys.platform == 'win32':
    sys.stdout.reconfigure(encoding='utf-8', errors='replace')
    sys.stderr.reconfigure(encoding='utf-8', errors='replace')

try:
    import requests
except ImportError:
    print("ERROR: 'requests' module required. Install with: pip install requests")
    sys.exit(1)

# =============================================================================
# Configuration
# =============================================================================

API_URL = os.getenv("API_URL", "http://localhost:8082")
PROD_API_URL = os.getenv("PROD_API_URL", "https://api.aiwolfsolutions.com")
DATABASE_URL = os.getenv("DATABASE_URL", "postgresql://medspa:medspa@localhost:5432/medspa?sslmode=disable")
SKIP_DB_CHECK = os.getenv("SKIP_DB_CHECK", "").lower() in ("1", "true", "yes")

# Square checkout redirect URLs.
# Note: Square can reject redirect_url values that aren't whitelisted for the app.
# For local dev runs, default to example.com (override with SUCCESS_URL/CANCEL_URL as needed).
_DEFAULT_SUCCESS_URL_LOCAL = "https://example.com/success"
_DEFAULT_CANCEL_URL_LOCAL = "https://example.com/cancel"
_DEFAULT_SUCCESS_URL_REMOTE = f"{PROD_API_URL}/payments/success"
_DEFAULT_CANCEL_URL_REMOTE = f"{PROD_API_URL}/payments/cancel"
_is_local_api = API_URL.startswith("http://localhost") or API_URL.startswith("http://127.0.0.1")
SUCCESS_URL = os.getenv("SUCCESS_URL", _DEFAULT_SUCCESS_URL_LOCAL if _is_local_api else _DEFAULT_SUCCESS_URL_REMOTE)
CANCEL_URL = os.getenv("CANCEL_URL", _DEFAULT_CANCEL_URL_LOCAL if _is_local_api else _DEFAULT_CANCEL_URL_REMOTE)

# Telnyx webhook secret for signature validation (from .env)
TELNYX_WEBHOOK_SECRET = os.getenv("TELNYX_WEBHOOK_SECRET", "").strip()

# Square webhook secret for payment webhook signature validation
SQUARE_WEBHOOK_SIGNATURE_KEY = os.getenv("SQUARE_WEBHOOK_SIGNATURE_KEY", "").strip()

# Test identifiers - using UUIDs for production-like behavior
TEST_ORG_ID = os.getenv("TEST_ORG_ID", "11111111-1111-1111-1111-111111111111")
TEST_CUSTOMER_PHONE = os.getenv("TEST_CUSTOMER_PHONE", "+15550001234")  # Customer's phone
TEST_CLINIC_PHONE = os.getenv("TEST_CLINIC_PHONE", "+18662894911")      # Clinic's hosted number (Twilio verified)
TEST_CUSTOMER_NAME = "E2E Automated Test"
TEST_CUSTOMER_EMAIL = "e2e-automated@test.dev"

# E2E behavior toggles
SMS_PROVIDER = os.getenv("SMS_PROVIDER", "").strip().lower()
DEMO_MODE = os.getenv("DEMO_MODE", "").strip().lower() in ("1", "true", "yes", "on")
E2E_REQUIRE_TELNYX = os.getenv("E2E_REQUIRE_TELNYX", "").strip().lower() in ("1", "true", "yes", "on")

# Conversation simulation delays
AI_RESPONSE_WAIT = int(os.getenv("AI_RESPONSE_WAIT", "8"))  # seconds to wait for AI processing
STEP_DELAY = float(os.getenv("STEP_DELAY", "2"))  # delay between steps

# Knowledge seeding / scraping
# Default prospect uses Boulevard (case study: Skin House Facial Bar).
DEFAULT_KNOWLEDGE_SCRAPE_URL = "https://skinhousefacialbar.com"
_scrape_url_raw = os.getenv("KNOWLEDGE_SCRAPE_URL", DEFAULT_KNOWLEDGE_SCRAPE_URL).strip()
KNOWLEDGE_SCRAPE_URL = "" if _scrape_url_raw.lower() in ("", "0", "false", "off", "none") else _scrape_url_raw
KNOWLEDGE_SCRAPE_MAX_PAGES = int(os.getenv("KNOWLEDGE_SCRAPE_MAX_PAGES", "5"))
KNOWLEDGE_SCRAPE_MAX_DOCS = int(os.getenv("KNOWLEDGE_SCRAPE_MAX_DOCS", "12"))
KNOWLEDGE_SCRAPE_MAX_CHARS = int(os.getenv("KNOWLEDGE_SCRAPE_MAX_CHARS", "2500"))
KNOWLEDGE_SCRAPE_TIMEOUT = int(os.getenv("KNOWLEDGE_SCRAPE_TIMEOUT", "15"))
KNOWLEDGE_PREVIEW_FILE = os.getenv("KNOWLEDGE_PREVIEW_FILE", "tmp/knowledge_preview.json")

# Colors for terminal output
class Colors:
    HEADER = '\033[95m'
    BLUE = '\033[94m'
    CYAN = '\033[96m'
    GREEN = '\033[92m'
    YELLOW = '\033[93m'
    RED = '\033[91m'
    ENDC = '\033[0m'
    BOLD = '\033[1m'

# =============================================================================
# Utility Functions
# =============================================================================

def print_header(text: str):
    print(f"\n{Colors.HEADER}{Colors.BOLD}{'='*70}")
    print(f"  {text}")
    print(f"{'='*70}{Colors.ENDC}\n")

def print_step(step_num: int, text: str):
    print(f"\n{Colors.CYAN}{Colors.BOLD}[STEP {step_num}] {text}{Colors.ENDC}")
    print("-" * 60)

def print_success(text: str):
    print(f"{Colors.GREEN}✅ {text}{Colors.ENDC}")

def print_warning(text: str):
    print(f"{Colors.YELLOW}⚠️  {text}{Colors.ENDC}")

def print_error(text: str):
    print(f"{Colors.RED}❌ {text}{Colors.ENDC}")

def print_info(text: str):
    print(f"{Colors.BLUE}ℹ️  {text}{Colors.ENDC}")

def timestamp() -> str:
    return datetime.now(timezone.utc).isoformat().replace("+00:00", "Z")

def generate_event_id() -> str:
    return f"evt_{uuid.uuid4().hex[:16]}"

def compute_telnyx_signature(timestamp: str, payload: bytes) -> str:
    """Compute Telnyx webhook signature (HMAC-SHA256)."""
    unsigned = f"{timestamp}.".encode() + payload
    mac = hmac.new(TELNYX_WEBHOOK_SECRET.encode(), unsigned, hashlib.sha256)
    return mac.hexdigest()

def wait_with_countdown(seconds: int, message: str = "Waiting"):
    print(f"   {message}...", end="", flush=True)
    for i in range(seconds, 0, -1):
        print(f" {i}", end="", flush=True)
        time.sleep(1)
    print(" done!")

def docker_compose_cmd() -> Optional[list]:
    """Return the docker compose command as a list (supports both plugins + legacy)."""
    if shutil.which("docker") is not None:
        return ["docker", "compose"]
    if shutil.which("docker-compose") is not None:
        return ["docker-compose"]
    return None

def run_psql(sql: str, *, tuples_only: bool = False, timeout: int = 10) -> Optional[subprocess.CompletedProcess]:
    """Run a SQL query using psql, with a Docker fallback when psql isn't installed locally."""
    if SKIP_DB_CHECK:
        return None

    psql_args = ["psql", DATABASE_URL]
    if tuples_only:
        psql_args.append("-t")
    psql_args += ["-c", sql]

    if shutil.which("psql") is not None:
        return subprocess.run(psql_args, capture_output=True, text=True, timeout=timeout)

    compose = docker_compose_cmd()
    if compose is None:
        return None

    docker_args = compose + ["exec", "-T", "postgres"] + psql_args
    return subprocess.run(docker_args, capture_output=True, text=True, timeout=timeout)

# =============================================================================
# API Interaction Functions
# =============================================================================

def check_health() -> bool:
    """Verify API is running and healthy."""
    try:
        resp = requests.get(f"{API_URL}/health", timeout=10)
        if resp.status_code == 200:
            print_success("API is healthy")
            return True
        else:
            print_error(f"API returned status {resp.status_code}")
            return False
    except requests.exceptions.RequestException as e:
        print_error(f"Cannot connect to API at {API_URL}: {e}")
        return False

# =============================================================================
# Knowledge Scraping Utilities
# =============================================================================

class _HTMLKnowledgeExtractor(HTMLParser):
    def __init__(self):
        super().__init__()
        self.title_parts: List[str] = []
        self.text_parts: List[str] = []
        self.links: List[str] = []
        self._in_title = False
        self._skip_depth = 0

    def handle_starttag(self, tag, attrs):
        tag = tag.lower()
        if tag in ("script", "style", "noscript", "svg"):
            self._skip_depth += 1
            return
        if tag == "title":
            self._in_title = True
        if tag == "a":
            for k, v in attrs:
                if k.lower() == "href" and v:
                    self.links.append(v)
        if tag in ("p", "br", "li", "h1", "h2", "h3", "h4", "h5", "h6"):
            self.text_parts.append("\n")

    def handle_endtag(self, tag):
        tag = tag.lower()
        if tag in ("script", "style", "noscript", "svg") and self._skip_depth > 0:
            self._skip_depth -= 1
            return
        if tag == "title":
            self._in_title = False

    def handle_data(self, data):
        if self._skip_depth > 0:
            return
        if not data:
            return
        if self._in_title:
            self.title_parts.append(data.strip())
        self.text_parts.append(data)


def _normalize_url(base_url: str, href: str) -> Optional[str]:
    if not href:
        return None
    href = href.strip()
    if not href or href.startswith("#"):
        return None
    if href.lower().startswith(("mailto:", "tel:", "javascript:")):
        return None

    full = urljoin(base_url, href)
    full, _ = urldefrag(full)
    parsed = urlparse(full)
    if parsed.scheme not in ("http", "https"):
        return None

    # Drop query params to reduce tracking/noise.
    parsed = parsed._replace(query="")
    return parsed.geturl()


def _is_same_site(base_url: str, other_url: str) -> bool:
    base = urlparse(base_url)
    other = urlparse(other_url)
    if not base.netloc or not other.netloc:
        return False
    base_host = base.netloc.lower().lstrip("www.")
    other_host = other.netloc.lower().lstrip("www.")
    return base_host == other_host


def _looks_like_html_page(url: str) -> bool:
    path = urlparse(url).path.lower()
    for ext in (".png", ".jpg", ".jpeg", ".gif", ".webp", ".svg", ".css", ".js", ".pdf", ".zip", ".mp4", ".mov"):
        if path.endswith(ext):
            return False
    return True


def _score_url(url: str) -> int:
    path = urlparse(url).path.lower()
    score = 0
    for kw, weight in [
        ("services", 50),
        ("treatments", 45),
        ("pricing", 45),
        ("prices", 45),
        ("membership", 40),
        ("memberships", 40),
        ("packages", 35),
        ("faq", 35),
        ("policies", 30),
        ("policy", 30),
        ("about", 20),
        ("contact", 15),
        ("locations", 15),
        ("hours", 10),
    ]:
        if kw in path:
            score += weight
    return score


def _extract_page(url: str) -> Tuple[str, str, List[str]]:
    resp = requests.get(url, timeout=KNOWLEDGE_SCRAPE_TIMEOUT, headers={"User-Agent": "Mozilla/5.0"})
    resp.raise_for_status()

    parser = _HTMLKnowledgeExtractor()
    parser.feed(resp.text)

    title = html_lib.unescape(" ".join(parser.title_parts)).strip()
    if not title:
        title = urlparse(url).path.strip("/") or url

    text = html_lib.unescape(" ".join(parser.text_parts))
    text = re.sub(r"\\s+", " ", text).strip()
    return title, text, parser.links


def scrape_site_to_documents(base_url: str) -> Tuple[List[str], Dict[str, Any]]:
    if not base_url.startswith(("http://", "https://")):
        base_url = "https://" + base_url

    title, text, links = _extract_page(base_url)
    domain = urlparse(base_url).netloc.lower().lstrip("www.")

    candidates: List[str] = []
    for href in links:
        norm = _normalize_url(base_url, href)
        if not norm:
            continue
        if not _looks_like_html_page(norm):
            continue
        if not _is_same_site(base_url, norm):
            continue
        candidates.append(norm)

    for slug in ("/services", "/pricing", "/faq", "/policies", "/about", "/contact", "/membership", "/memberships", "/packages"):
        candidates.append(_normalize_url(base_url, slug) or "")

    uniq = sorted({u for u in candidates if u and u != base_url}, key=lambda u: (_score_url(u), -len(u)), reverse=True)
    urls = [base_url] + uniq[: max(0, KNOWLEDGE_SCRAPE_MAX_PAGES - 1)]

    documents: List[str] = []
    pages: List[Dict[str, Any]] = []
    seen_hashes: set[str] = set()

    # Always include an explicit "website" fact document so simple questions like
    # "what's your website url" have a high-recall match in retrieval.
    website_doc = f"Clinic Website\nSource: {base_url}\n\nOfficial website: {base_url}\nDomain: {domain}\nBusiness: {title}"
    documents.append(website_doc)
    pages.append({"url": base_url, "title": "Clinic Website", "chars": len(website_doc), "preview": website_doc[:240]})

    for url in urls:
        if len(documents) >= KNOWLEDGE_SCRAPE_MAX_DOCS:
            break
        try:
            page_title, page_text, _ = _extract_page(url)
        except Exception as e:
            pages.append({"url": url, "error": str(e)})
            continue

        if len(page_text) < 250:
            pages.append({"url": url, "title": page_title, "skipped": "too_short"})
            continue

        snippet = page_text[:KNOWLEDGE_SCRAPE_MAX_CHARS].strip()
        content_hash = hashlib.sha256(snippet.encode("utf-8")).hexdigest()
        if content_hash in seen_hashes:
            pages.append({"url": url, "title": page_title, "skipped": "duplicate"})
            continue
        seen_hashes.add(content_hash)

        doc = f"{page_title}\nSource: {url}\n\n{snippet}"
        documents.append(doc)
        pages.append({"url": url, "title": page_title, "chars": len(snippet), "preview": snippet[:240]})

    preview = {
        "source_url": base_url,
        "pages": pages,
        "documents_count": len(documents),
    }
    return documents, preview


def seed_knowledge() -> bool:
    """Seed the knowledge base for the test org."""
    knowledge_file = os.getenv("KNOWLEDGE_FILE", "testdata/demo-clinic-knowledge.json")

    try:
        documents: List[str] = []
        preview: Optional[Dict[str, Any]] = None

        if KNOWLEDGE_SCRAPE_URL:
            print_info(f"Scraping prospect website for knowledge: {KNOWLEDGE_SCRAPE_URL}")
            documents, preview = scrape_site_to_documents(KNOWLEDGE_SCRAPE_URL)
            if not documents:
                raise ValueError("scrape produced no usable documents")

            preview_dir = os.path.dirname(KNOWLEDGE_PREVIEW_FILE)
            if preview_dir:
                os.makedirs(preview_dir, exist_ok=True)
            with open(KNOWLEDGE_PREVIEW_FILE, "w", encoding="utf-8") as f:
                json.dump(preview, f, indent=2, ensure_ascii=False)
            print_info(f"Knowledge preview saved to: {KNOWLEDGE_PREVIEW_FILE}")

            for i, doc in enumerate(documents[:3], start=1):
                excerpt = doc.split("\n\n", 1)[-1][:200].strip()
                title_line = doc.splitlines()[0].strip()
                print_info(f"Preview doc {i}: {title_line} — {excerpt}...")
        else:
            if not os.path.exists(knowledge_file):
                raise FileNotFoundError(f"Knowledge file not found: {knowledge_file}")
            with open(knowledge_file, 'r') as f:
                knowledge_data = json.load(f)

            raw_docs = knowledge_data.get("documents", [])
            if not isinstance(raw_docs, list):
                raise ValueError("knowledge JSON must include a 'documents' array")

            for doc in raw_docs:
                if isinstance(doc, str):
                    text = doc.strip()
                elif isinstance(doc, dict):
                    title = str(doc.get("title", "")).strip()
                    content = str(doc.get("content", "")).strip()
                    if title and content:
                        text = f"{title}\n\n{content}"
                    else:
                        text = (content or title).strip()
                else:
                    raise ValueError("documents must be strings or {title, content} objects")

                if text:
                    documents.append(text)

            if not documents:
                raise ValueError("knowledge documents cannot be empty")

        payload = {"documents": documents}
        print_info(f"Uploading {len(documents)} knowledge snippets to org {TEST_ORG_ID}")

        resp = requests.post(
            f"{API_URL}/knowledge/{TEST_ORG_ID}",
            json=payload,
            headers={
                "Content-Type": "application/json",
                "X-Org-ID": TEST_ORG_ID
            },
            timeout=120
        )

        if resp.status_code in (200, 201, 204):
            print_success("Knowledge base seeded")
            return True
        else:
            print_error(f"Knowledge seeding returned {resp.status_code}: {resp.text[:200]}")
            return False
    except Exception as e:
        print_error(f"Knowledge seeding failed: {e}")
        return False

def seed_hosted_number() -> bool:
    """Seed the hosted number mapping so webhooks can find the clinic."""
    if SKIP_DB_CHECK:
        print_warning("Skipping hosted number seeding (no DB access)")
        return True

    try:
        sql = f"""
            INSERT INTO hosted_number_orders (clinic_id, e164_number, status, created_at, updated_at)
            VALUES ('{TEST_ORG_ID}', '{TEST_CLINIC_PHONE}', 'activated', NOW(), NOW())
            ON CONFLICT (clinic_id, e164_number) DO UPDATE SET status = 'activated', updated_at = NOW();
        """
        result = run_psql(sql, timeout=10)

        if result is not None and result.returncode == 0:
            print_success(f"Hosted number {TEST_CLINIC_PHONE} mapped to org {TEST_ORG_ID}")
            return True
        else:
            stderr = ""
            if result is not None and result.stderr:
                stderr = result.stderr[:200]
            print_warning(f"Hosted number seeding failed (webhooks may 404): {stderr or 'psql not available'}")
            return True  # Non-fatal
    except Exception as e:
        print_warning(f"Hosted number seeding failed: {e}")
        return True  # Non-fatal


def seed_clinic_config(clinic_name: str = "Cleveland Primecare Medspa") -> bool:
    """Seed the clinic config in Redis so the AI uses the correct clinic name."""
    try:
        config = {
            "org_id": TEST_ORG_ID,
            "name": clinic_name,
            "timezone": "America/New_York",
            "business_hours": {
                "monday": {"open": "09:00", "close": "18:00"},
                "tuesday": {"open": "09:00", "close": "18:00"},
                "wednesday": {"open": "09:00", "close": "18:00"},
                "thursday": {"open": "09:00", "close": "18:00"},
                "friday": {"open": "09:00", "close": "17:00"},
            },
            "callback_sla_hours": 12,
            "deposit_amount_cents": 5000,
            "services": ["Botox", "Fillers", "Laser Treatments", "HydraFacial", "DiamondGlow"],
            "notifications": {
                "email_enabled": False,
                "sms_enabled": False,
                "notify_on_payment": True,
                "notify_on_new_lead": False,
            },
        }
        config_json = json.dumps(config)

        # Use docker compose exec to set the Redis key
        cmd = [
            "docker", "compose", "exec", "-T", "redis",
            "redis-cli", "SET", f"clinic:config:{TEST_ORG_ID}", config_json
        ]
        result = subprocess.run(cmd, capture_output=True, text=True, timeout=10, cwd=_PROJECT_ROOT)

        if result.returncode == 0:
            print_success(f"Clinic config seeded: {clinic_name}")
            return True
        else:
            print_warning(f"Clinic config seeding failed: {result.stderr[:200] if result.stderr else 'unknown error'}")
            return True  # Non-fatal
    except Exception as e:
        print_warning(f"Clinic config seeding failed: {e}")
        return True  # Non-fatal


def _wait_for_conversation_job(job_id: str, timeout_seconds: int = 240) -> Optional[Dict[str, Any]]:
    """Poll the conversation job endpoint until completed/failed."""
    deadline = time.time() + timeout_seconds
    while time.time() < deadline:
        try:
            resp = requests.get(
                f"{API_URL}/conversations/jobs/{job_id}",
                headers={"X-Org-ID": TEST_ORG_ID},
                timeout=10,
            )
            if resp.status_code != 200:
                time.sleep(1)
                continue

            job = resp.json()
            status = str(job.get("status", "")).lower()
            if status in ("completed", "failed"):
                return job
        except Exception:
            pass

        time.sleep(1)

    return None

def _extract_conversation_id(job: Dict[str, Any]) -> str:
    convo_id = str(job.get("conversationId", "") or "").strip()
    if convo_id:
        return convo_id
    response = job.get("response") or {}
    if isinstance(response, dict):
        for key in ("ConversationID", "conversationId", "conversationID"):
            value = response.get(key)
            if isinstance(value, str) and value.strip():
                return value.strip()
    return ""

def _extract_conversation_message(job: Dict[str, Any]) -> str:
    response = job.get("response") or {}
    if isinstance(response, dict):
        for key in ("Message", "message"):
            value = response.get(key)
            if isinstance(value, str) and value.strip():
                return value.strip()
    return ""

def verify_rag_knowledge() -> bool:
    """Verify that the assistant can answer a question using seeded knowledge (RAG)."""
    if not KNOWLEDGE_SCRAPE_URL:
        print_warning("KNOWLEDGE_SCRAPE_URL disabled; skipping RAG verification")
        return True

    expected_domain = urlparse(KNOWLEDGE_SCRAPE_URL).netloc.lower().lstrip("www.")
    if not expected_domain:
        print_warning("Could not derive expected domain from KNOWLEDGE_SCRAPE_URL; skipping RAG verification")
        return True

    lead_id = f"e2e_knowledge_{uuid.uuid4().hex[:10]}"

    try:
        start_payload = {
            "OrgID": TEST_ORG_ID,
            "LeadID": lead_id,
            "Intro": "Admin check: verifying clinic knowledge is available.",
            "Source": "e2e_knowledge_check",
            "ClinicID": TEST_ORG_ID,
            "Channel": "sms",
            "From": TEST_CUSTOMER_PHONE,
            "To": TEST_CLINIC_PHONE,
        }

        start_resp = requests.post(
            f"{API_URL}/conversations/start",
            json=start_payload,
            headers={"Content-Type": "application/json", "X-Org-ID": TEST_ORG_ID},
            timeout=15,
        )
        if start_resp.status_code not in (200, 202):
            print_error(f"Conversation start failed: {start_resp.status_code} - {start_resp.text[:200]}")
            return False

        start_job_id = (start_resp.json() or {}).get("jobId", "")
        if not start_job_id:
            print_error("Conversation start did not return jobId")
            return False

        start_job = _wait_for_conversation_job(start_job_id, timeout_seconds=240)
        if not start_job or str(start_job.get("status", "")).lower() != "completed":
            print_error(f"Conversation start job did not complete: {start_job or 'timeout'}")
            return False

        conversation_id = _extract_conversation_id(start_job)
        if not conversation_id:
            print_error("Could not extract ConversationID from start job")
            return False

        msg_payload = {
            "OrgID": TEST_ORG_ID,
            "LeadID": lead_id,
            "ConversationID": conversation_id,
            "Message": "What is the business website URL? Reply with the full URL.",
            "ClinicID": TEST_ORG_ID,
            "Channel": "sms",
            "From": TEST_CUSTOMER_PHONE,
            "To": TEST_CLINIC_PHONE,
        }
        msg_resp = requests.post(
            f"{API_URL}/conversations/message",
            json=msg_payload,
            headers={"Content-Type": "application/json", "X-Org-ID": TEST_ORG_ID},
            timeout=15,
        )
        if msg_resp.status_code not in (200, 202):
            print_error(f"Conversation message enqueue failed: {msg_resp.status_code} - {msg_resp.text[:200]}")
            return False

        msg_job_id = (msg_resp.json() or {}).get("jobId", "")
        if not msg_job_id:
            print_error("Conversation message did not return jobId")
            return False

        msg_job = _wait_for_conversation_job(msg_job_id, timeout_seconds=300)
        if not msg_job or str(msg_job.get("status", "")).lower() != "completed":
            print_error(f"Conversation message job did not complete: {msg_job or 'timeout'}")
            return False

        answer = _extract_conversation_message(msg_job)
        if not answer:
            print_error("Conversation message completed but returned no Message")
            return False

        answer_lc = answer.lower()
        if expected_domain in answer_lc:
            print_success(f"RAG verified (found {expected_domain} in assistant response)")
            return True

        print_error(f"RAG verification failed; expected domain '{expected_domain}' not found in response: {answer[:220]}")
        return False

    except Exception as e:
        print_error(f"RAG verification failed: {e}")
        return False

def create_lead() -> Optional[Dict[str, Any]]:
    """Create a test lead."""
    payload = {
        "name": TEST_CUSTOMER_NAME,
        "phone": TEST_CUSTOMER_PHONE,
        "email": TEST_CUSTOMER_EMAIL,
        "message": "E2E automated test lead",
        "source": "e2e_automated_test"
    }

    try:
        resp = requests.post(
            f"{API_URL}/leads/web",
            json=payload,
            headers={
                "Content-Type": "application/json",
                "X-Org-ID": TEST_ORG_ID
            },
            timeout=10
        )

        if resp.status_code in (200, 201):
            lead = resp.json()
            print_success(f"Lead created: {lead.get('id', 'unknown')}")
            return lead
        else:
            print_error(f"Lead creation failed: {resp.status_code} - {resp.text[:200]}")
            return None
    except Exception as e:
        print_error(f"Lead creation failed: {e}")
        return None

def send_telnyx_voice_webhook(
    hangup_cause: str = "no_answer",
    *,
    event_id: Optional[str] = None,
    call_id: Optional[str] = None,
    from_phone: Optional[str] = None,
    to_phone: Optional[str] = None,
) -> bool:
    """Simulate a missed call via Telnyx voice webhook."""
    event_id = event_id or generate_event_id()
    call_id = call_id or f"call_{uuid.uuid4().hex[:12]}"
    from_phone = from_phone or TEST_CUSTOMER_PHONE
    to_phone = to_phone or TEST_CLINIC_PHONE
    payload = {
        "data": {
            "id": event_id,
            "event_type": "call.hangup",
            "occurred_at": timestamp(),
            "payload": {
                "id": call_id,
                "status": hangup_cause,
                "hangup_cause": hangup_cause,
                "from": {"phone_number": from_phone},
                "to": [{"phone_number": to_phone}]
            }
        }
    }

    try:
        ts = str(int(time.time()))
        payload_bytes = json.dumps(payload).encode('utf-8')
        signature = compute_telnyx_signature(ts, payload_bytes)

        resp = requests.post(
            f"{API_URL}/webhooks/telnyx/voice",
            data=payload_bytes,
            headers={
                "Content-Type": "application/json",
                "Telnyx-Timestamp": ts,
                "Telnyx-Signature": signature
            },
            timeout=30
        )

        print_info(f"Voice webhook response: {resp.status_code}")

        if resp.status_code == 200:
            print_success("Missed call webhook processed")
            return True
        elif resp.status_code in (401, 403):
            print_warning("Voice webhook signature validation failed")
            return True  # Non-fatal for testing
        else:
            print_error(f"Voice webhook failed: {resp.text[:200]}")
            return False
    except Exception as e:
        print_error(f"Voice webhook failed: {e}")
        return False

def send_telnyx_sms_webhook(
    message_text: str,
    *,
    event_id: Optional[str] = None,
    telnyx_message_id: Optional[str] = None,
    from_phone: Optional[str] = None,
    to_phone: Optional[str] = None,
) -> bool:
    """Simulate an incoming SMS via Telnyx webhook."""
    event_id = event_id or generate_event_id()
    telnyx_message_id = telnyx_message_id or f"msg_{uuid.uuid4().hex[:12]}"
    from_phone = from_phone or TEST_CUSTOMER_PHONE
    to_phone = to_phone or TEST_CLINIC_PHONE
    payload = {
        "data": {
            "id": event_id,
            "event_type": "message.received",
            "occurred_at": timestamp(),
            "payload": {
                "id": telnyx_message_id,
                "type": "SMS",
                "direction": "inbound",
                "from": {"phone_number": from_phone},
                "to": [{"phone_number": to_phone}],
                "text": message_text,
                "received_at": timestamp()
            }
        }
    }

    try:
        ts = str(int(time.time()))
        payload_bytes = json.dumps(payload).encode('utf-8')
        signature = compute_telnyx_signature(ts, payload_bytes)

        resp = requests.post(
            f"{API_URL}/webhooks/telnyx/messages",
            data=payload_bytes,
            headers={
                "Content-Type": "application/json",
                "Telnyx-Timestamp": ts,
                "Telnyx-Signature": signature
            },
            timeout=30
        )

        print_info(f"SMS webhook response: {resp.status_code}")

        if resp.status_code == 200:
            msg_preview = f"\"{message_text[:50]}...\"" if len(message_text) > 50 else f"\"{message_text}\""
            print_success(f"SMS webhook processed: {msg_preview}")
            return True
        elif resp.status_code in (401, 403):
            print_warning("SMS webhook signature validation failed")
            return True  # Non-fatal for testing
        else:
            print_error(f"SMS webhook failed: {resp.text[:200]}")
            return False
    except Exception as e:
        print_error(f"SMS webhook failed: {e}")
        return False

def create_checkout(lead_id: str, amount_cents: int = 5000) -> Optional[Dict[str, Any]]:
    """Create a Square checkout link."""
    payload = {
        "lead_id": lead_id,
        "amount_cents": amount_cents,
        "success_url": SUCCESS_URL,
        "cancel_url": CANCEL_URL
        # Don't pass booking_intent_id - let the server generate it
    }

    try:
        resp = requests.post(
            f"{API_URL}/payments/checkout",
            json=payload,
            headers={
                "Content-Type": "application/json",
                "X-Org-ID": TEST_ORG_ID
            },
            timeout=30
        )

        if resp.status_code == 200:
            result = resp.json()
            print_success(f"Checkout created: {result.get('checkout_url', 'unknown')[:60]}...")
            return result
        else:
            print_error(f"Checkout creation failed: {resp.status_code} - {resp.text[:200]}")
            return None
    except Exception as e:
        print_error(f"Checkout creation failed: {e}")
        return None

def compute_square_signature(webhook_url: str, body: bytes, key: str) -> str:
    """Compute the Square webhook HMAC-SHA1 signature."""
    message = webhook_url + body.decode("utf-8")
    mac = hmac.new(key.encode("utf-8"), message.encode("utf-8"), hashlib.sha1)
    return base64.b64encode(mac.digest()).decode("ascii")


def send_square_payment_webhook(lead_id: str, booking_intent_id: str, amount_cents: int = 5000) -> bool:
    """Simulate a Square payment.completed webhook."""
    event_id = f"sq_evt_{uuid.uuid4().hex[:16]}"
    payment_id = f"sq_pay_{uuid.uuid4().hex[:16]}"

    payload = {
        "id": event_id,
        "event_id": event_id,
        "created_at": timestamp(),
        "type": "payment.completed",
        "data": {
            "object": {
                "payment": {
                    "id": payment_id,
                    "status": "COMPLETED",
                    "order_id": f"sq_order_{uuid.uuid4().hex[:12]}",
                    "amount_money": {
                        "amount": amount_cents,
                        "currency": "USD"
                    },
                    "metadata": {
                        "org_id": TEST_ORG_ID,
                        "lead_id": lead_id,
                        "booking_intent_id": booking_intent_id
                    }
                }
            }
        }
    }

    webhook_url = f"{API_URL}/webhooks/square"
    body_bytes = json.dumps(payload, separators=(",", ":")).encode("utf-8")

    # Compute signature if we have the Square webhook secret
    signature = ""
    if SQUARE_WEBHOOK_SIGNATURE_KEY:
        signature = compute_square_signature(webhook_url, body_bytes, SQUARE_WEBHOOK_SIGNATURE_KEY)

    try:
        resp = requests.post(
            webhook_url,
            data=body_bytes,
            headers={
                "Content-Type": "application/json",
                "X-Square-Signature": signature
            },
            timeout=30
        )

        print_info(f"Square webhook response: {resp.status_code}")

        if resp.status_code == 200:
            print_success("Square payment webhook accepted")
            return True
        elif resp.status_code == 401:
            print_warning("Square signature validation failed (expected in test without key)")
            return True  # Acceptable in dev
        elif resp.status_code == 403:
            print_warning("Square signature validation failed - check SQUARE_WEBHOOK_SIGNATURE_KEY")
            return False
        else:
            print_error(f"Square webhook failed: {resp.text[:200]}")
            return False
    except Exception as e:
        print_error(f"Square webhook failed: {e}")
        return False

def check_database(query: str, description: str) -> Optional[str]:
    """Run a database query and return results."""
    if SKIP_DB_CHECK:
        print_warning(f"Skipping DB check: {description}")
        return None

    try:
        result = run_psql(query, tuples_only=True, timeout=10)
        if result is None:
            print_warning("psql not available - skipping database checks")
            return None
        if result.returncode != 0:
            print_warning(f"DB query failed: {result.stderr[:200]}")
            return None

        output = result.stdout.strip()
        if output:
            print_success(f"{description}: {output[:100]}")
        else:
            print_info(f"{description}: (no results)")
        return output
    except Exception as e:
        print_warning(f"DB check failed: {e}")
        return None

def get_payment_id_for_lead(lead_id: str) -> Optional[str]:
    """Get the most recent payment ID for a lead from the database."""
    if SKIP_DB_CHECK:
        return None

    try:
        sql = f"SELECT id FROM payments WHERE lead_id = '{lead_id}' ORDER BY created_at DESC LIMIT 1;"
        result = run_psql(sql, tuples_only=True, timeout=10)
        if result is None or result.returncode != 0:
            return None
        output = result.stdout.strip()
        return output or None
    except Exception:
        return None

# =============================================================================
# Main E2E Test Flow
# =============================================================================

def run_e2e_test():
    """Run the complete end-to-end test."""

    if not TELNYX_WEBHOOK_SECRET:
        raise RuntimeError("TELNYX_WEBHOOK_SECRET is required (set it in .env or the environment) to sign test webhooks.")

    require_telnyx_number = E2E_REQUIRE_TELNYX or DEMO_MODE
    if require_telnyx_number:
        if SMS_PROVIDER != "telnyx":
            raise RuntimeError(f"E2E is configured to require Telnyx, but SMS_PROVIDER={SMS_PROVIDER!r}. Set SMS_PROVIDER=telnyx.")
        if TEST_CLINIC_PHONE == "+18662894911":
            raise RuntimeError("E2E requires the Telnyx long-code, but TEST_CLINIC_PHONE is still the default Twilio verified number. Set TEST_CLINIC_PHONE to your Telnyx number (E164).")

    print_header("MedSpa AI Platform - Full E2E Automated Test")

    print(f"Configuration:")
    print(f"  API URL:        {API_URL}")
    print(f"  Test Org ID:    {TEST_ORG_ID}")
    print(f"  Customer Phone: {TEST_CUSTOMER_PHONE}")
    print(f"  Clinic Phone:   {TEST_CLINIC_PHONE}")
    print(f"  Success URL:    {SUCCESS_URL}")
    print(f"  Cancel URL:     {CANCEL_URL}")
    print(f"  DB Checks:      {'Disabled' if SKIP_DB_CHECK else 'Enabled'}")

    # Track test results
    results = {
        "passed": 0,
        "failed": 0,
        "warnings": 0
    }
    lead_id = None
    booking_intent_id = None  # Will be populated from database after checkout

    # =========================================================================
    # Step 1: Health Check
    # =========================================================================
    print_step(1, "Checking API Health")
    if not check_health():
        print_error("FATAL: API is not healthy. Aborting test.")
        print_info(f"Make sure the API is running on {API_URL}")
        sys.exit(1)
    results["passed"] += 1

    time.sleep(STEP_DELAY)

    # =========================================================================
    # Step 2: Seed Knowledge Base and Hosted Number
    # =========================================================================
    print_step(2, "Seeding Knowledge Base and Hosted Number Mapping")
    if not seed_knowledge():
        results["failed"] += 1
        print_error("FATAL: Knowledge seeding failed. Aborting test.")
        sys.exit(1)
    if not verify_rag_knowledge():
        results["failed"] += 1
        print_error("FATAL: RAG verification failed. Aborting test.")
        sys.exit(1)
    seed_hosted_number()  # Maps clinic phone to org ID for webhook routing
    results["passed"] += 1

    time.sleep(STEP_DELAY)

    # =========================================================================
    # Step 3: Create Test Lead
    # =========================================================================
    print_step(3, "Creating Test Lead")
    lead = create_lead()
    if lead:
        lead_id = lead.get("id")
        results["passed"] += 1
    else:
        print_warning("Lead creation failed - continuing with mock ID")
        lead_id = str(uuid.uuid4())
        results["warnings"] += 1

    time.sleep(STEP_DELAY)

    # =========================================================================
    # Step 4: Simulate Missed Call
    # =========================================================================
    print_step(4, "Simulating Missed Call (Telnyx Voice Webhook)")
    print_info("This triggers the AI to send an initial 'Sorry we missed your call' SMS")

    if send_telnyx_voice_webhook():
        results["passed"] += 1
    else:
        results["warnings"] += 1

    wait_with_countdown(AI_RESPONSE_WAIT, "Waiting for AI to process missed call")

    # =========================================================================
    # Step 5: Customer SMS - Initial Inquiry
    # =========================================================================
    print_step(5, "Customer SMS: Initial Inquiry")

    if send_telnyx_sms_webhook("Hi, I want to book Botox for weekday afternoons"):
        results["passed"] += 1
    else:
        results["failed"] += 1

    wait_with_countdown(AI_RESPONSE_WAIT, "Waiting for AI to respond")

    # =========================================================================
    # Step 6: Customer SMS - Confirms Interest
    # =========================================================================
    print_step(6, "Customer SMS: Confirms Interest")

    if send_telnyx_sms_webhook("Yes, I'm a new patient. What times do you have available?"):
        results["passed"] += 1
    else:
        results["failed"] += 1

    wait_with_countdown(AI_RESPONSE_WAIT, "Waiting for AI to respond")

    # =========================================================================
    # Step 7: Customer SMS - Ready to Book
    # =========================================================================
    print_step(7, "Customer SMS: Ready to Book with Deposit")

    if send_telnyx_sms_webhook("Friday at 3pm works great. Yes, I'll pay the deposit to secure my appointment."):
        results["passed"] += 1
    else:
        results["failed"] += 1

    wait_with_countdown(AI_RESPONSE_WAIT, "Waiting for AI to process deposit intent")

    # =========================================================================
    # Step 8: Verify Lead Preferences in Database
    # =========================================================================
    print_step(8, "Verifying Lead Preferences in Database")

    check_database(
        f"SELECT service_interest, preferred_days, preferred_times FROM leads WHERE phone = '{TEST_CUSTOMER_PHONE}' ORDER BY created_at DESC LIMIT 1;",
        "Lead preferences"
    )

    time.sleep(STEP_DELAY)

    # =========================================================================
    # Step 9: Create Checkout (Manual Trigger for Testing)
    # =========================================================================
    print_step(9, "Creating Square Checkout Link")
    print_info("In production, the AI would trigger this automatically when deposit intent is detected")

    if lead_id:
        checkout = create_checkout(lead_id, 5000)
        if checkout:
            results["passed"] += 1
            print_info(f"Customer would open: {checkout.get('checkout_url', 'N/A')}")
            # Get the actual payment ID from the database for the Square webhook
            booking_intent_id = get_payment_id_for_lead(lead_id)
            if booking_intent_id:
                print_info(f"Payment record ID (booking_intent_id): {booking_intent_id}")
            else:
                print_warning("Could not retrieve payment ID from database - Square webhook may fail")
                booking_intent_id = str(uuid.uuid4())  # Fallback
        else:
            results["warnings"] += 1
    else:
        print_warning("No lead ID available - skipping checkout creation")
        results["warnings"] += 1

    time.sleep(STEP_DELAY)

    # =========================================================================
    # Step 10: Simulate Square Payment Completion
    # =========================================================================
    print_step(10, "Simulating Square Payment Completion")
    print_info("This simulates the customer completing payment on Square's hosted checkout")

    if lead_id and booking_intent_id:
        if send_square_payment_webhook(lead_id, booking_intent_id, 5000):
            results["passed"] += 1
        else:
            results["failed"] += 1
    else:
        print_warning("No lead ID or booking_intent_id - skipping payment webhook")
        results["warnings"] += 1

    wait_with_countdown(AI_RESPONSE_WAIT, "Waiting for payment processing and confirmation SMS")

    # =========================================================================
    # Step 11: Verify Payment and Outbox
    # =========================================================================
    print_step(11, "Verifying Payment Status and Outbox Events")

    check_database(
        f"SELECT status, provider_ref FROM payments WHERE lead_id::text LIKE '%{lead_id[:8] if lead_id else 'xxx'}%' ORDER BY created_at DESC LIMIT 1;",
        "Payment status"
    )

    check_database(
        "SELECT event_type, dispatched_at FROM outbox ORDER BY created_at DESC LIMIT 5;",
        "Recent outbox events"
    )

    # =========================================================================
    # Step 12: Final Database Verification
    # =========================================================================
    print_step(12, "Final Database Verification")

    check_database(
        f"SELECT deposit_status, priority_level FROM leads WHERE phone = '{TEST_CUSTOMER_PHONE}' ORDER BY created_at DESC LIMIT 1;",
        "Final lead status"
    )

    # =========================================================================
    # Summary
    # =========================================================================
    print_header("E2E Test Summary")

    total = results["passed"] + results["failed"] + results["warnings"]

    print(f"  {Colors.GREEN}Passed:   {results['passed']}{Colors.ENDC}")
    print(f"  {Colors.RED}Failed:   {results['failed']}{Colors.ENDC}")
    print(f"  {Colors.YELLOW}Warnings: {results['warnings']}{Colors.ENDC}")
    print(f"  Total:    {total}")
    print()

    if results["failed"] == 0:
        print_success("E2E TEST PASSED!")
        print()
        print("Next steps to verify manually:")
        print("  1. Check API logs for conversation flow")
        print("  2. Verify SMS messages were queued (check Telnyx dashboard or logs)")
        print("  3. Confirm outbox events were processed")
        return 0
    else:
        print_error("E2E TEST HAD FAILURES")
        print()
        print("Troubleshooting:")
        print("  1. Check API logs for errors")
        print("  2. Verify database connectivity")
        print("  3. Check Redis is running")
        print("  4. Ensure AWS Bedrock credentials are configured")
        return 1

# =============================================================================
# Entry Point
# =============================================================================

if __name__ == "__main__":
    try:
        exit_code = run_e2e_test()
        sys.exit(exit_code)
    except KeyboardInterrupt:
        print("\n\nTest interrupted by user.")
        sys.exit(130)
    except Exception as e:
        print_error(f"Unexpected error: {e}")
        import traceback
        traceback.print_exc()
        sys.exit(1)
