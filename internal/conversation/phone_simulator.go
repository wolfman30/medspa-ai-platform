package conversation

import (
	"net/http"
)

const phoneSimulatorHTML = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1, viewport-fit=cover" />
  <title>MedSpa AI - Phone Simulator</title>
  <style>
    :root{
      --bg: #0b0f14;
      --phone-bg: #0b0f14;
      --header-bg: rgba(16, 24, 40, 0.92);
      --muted: rgba(255,255,255,0.68);
      --text: rgba(255,255,255,0.92);
      --bubble-in: #2a2f36;
      --bubble-out: #0a84ff;
      --bubble-out-text: #ffffff;
      --bubble-in-text: rgba(255,255,255,0.92);
      --link: #7dd3ff;
      --danger: #ff453a;
      --ok: #34c759;
      --shadow: 0 24px 80px rgba(0,0,0,0.55);
      --radius: 22px;
      --font: ui-sans-serif, -apple-system, system-ui, Segoe UI, Roboto, Helvetica, Arial, "Apple Color Emoji", "Segoe UI Emoji";
    }
    html, body { height: 100%; }
    body{
      margin:0;
      font-family: var(--font);
      background: radial-gradient(1000px 600px at 50% 10%, #152033 0%, var(--bg) 60%);
      color: var(--text);
      display:flex;
      align-items:center;
      justify-content:center;
      padding: 24px;
      box-sizing:border-box;
    }
    .phone{
      width: 430px;
      height: 932px;
      max-width: 96vw;
      max-height: 92vh;
      border-radius: 44px;
      background: linear-gradient(180deg, rgba(255,255,255,0.06), rgba(255,255,255,0.03));
      padding: 14px;
      box-shadow: var(--shadow);
      position: relative;
    }
    .screen{
      width: 100%;
      height: 100%;
      border-radius: 36px;
      overflow: hidden;
      background: var(--phone-bg);
      display:flex;
      flex-direction: column;
      position: relative;
    }
    .island{
      position:absolute;
      top: 10px;
      left: 50%;
      transform: translateX(-50%);
      width: 134px;
      height: 36px;
      border-radius: 18px;
      background: rgba(0,0,0,0.82);
      z-index: 5;
      box-shadow: 0 8px 28px rgba(0,0,0,0.35);
    }
    .header{
      position: sticky;
      top: 0;
      z-index: 4;
      padding: 18px 16px 12px 16px;
      background: var(--header-bg);
      backdrop-filter: blur(14px);
      border-bottom: 1px solid rgba(255,255,255,0.08);
    }
    .title{
      font-size: 16px;
      font-weight: 650;
      letter-spacing: 0.2px;
    }
    .meta{
      margin-top: 6px;
      font-size: 12px;
      color: var(--muted);
      display:flex;
      flex-wrap: wrap;
      gap: 8px 14px;
    }
    .meta code{
      font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, "Liberation Mono", "Courier New", monospace;
      font-size: 11px;
      color: rgba(255,255,255,0.85);
      background: rgba(255,255,255,0.08);
      padding: 2px 6px;
      border-radius: 8px;
    }
    .status{
      margin-top: 10px;
      font-size: 12px;
      display:flex;
      align-items:center;
      justify-content: space-between;
      gap: 12px;
      color: var(--muted);
    }
    .status .pill{
      display:inline-flex;
      align-items:center;
      gap: 8px;
      padding: 6px 10px;
      border-radius: 999px;
      background: rgba(255,255,255,0.06);
      border: 1px solid rgba(255,255,255,0.08);
    }
    .dot{
      width: 8px;
      height: 8px;
      border-radius: 999px;
      background: rgba(255,255,255,0.35);
    }
    .dot.ok{ background: var(--ok); }
    .dot.bad{ background: var(--danger); }
    .messages{
      flex: 1 1 auto;
      overflow-y: auto;
      padding: 14px 14px 10px 14px;
      scroll-behavior: smooth;
    }
    .day{
      text-align:center;
      margin: 10px 0 14px;
      font-size: 12px;
      color: rgba(255,255,255,0.45);
    }
    .row{
      display:flex;
      width: 100%;
      margin: 8px 0;
    }
    .row.user{ justify-content: flex-end; }
    .row.assistant{ justify-content: flex-start; }
    .bubble{
      max-width: 78%;
      padding: 10px 12px;
      border-radius: 18px;
      line-height: 1.25;
      font-size: 14px;
      white-space: pre-wrap;
      word-break: break-word;
      border: 1px solid rgba(255,255,255,0.08);
    }
    .bubble.user{
      background: var(--bubble-out);
      color: var(--bubble-out-text);
      border-color: rgba(255,255,255,0.16);
      border-bottom-right-radius: 6px;
    }
    .bubble.assistant{
      background: var(--bubble-in);
      color: var(--bubble-in-text);
      border-bottom-left-radius: 6px;
    }
    .bubble.highlight{
      box-shadow: 0 0 0 2px rgba(255, 214, 10, 0.35) inset;
      border-color: rgba(255, 214, 10, 0.55);
    }
    .bubble a{
      color: var(--link);
      text-decoration: underline;
      text-underline-offset: 2px;
    }
    .time{
      margin-top: 6px;
      font-size: 11px;
      color: rgba(255,255,255,0.55);
      text-align: right;
    }
    .row.assistant .time{
      text-align: left;
    }
    .composer{
      padding: 10px 12px 14px 12px;
      border-top: 1px solid rgba(255,255,255,0.08);
      background: rgba(0,0,0,0.35);
      backdrop-filter: blur(10px);
    }
    .composer .hint{
      font-size: 12px;
      color: rgba(255,255,255,0.55);
      display:flex;
      gap: 10px;
      flex-wrap: wrap;
    }
    .composer input{
      width: 100%;
      margin-top: 8px;
      padding: 10px 12px;
      border-radius: 14px;
      border: 1px solid rgba(255,255,255,0.10);
      background: rgba(255,255,255,0.05);
      color: rgba(255,255,255,0.90);
      outline: none;
      font-size: 14px;
    }
    .composer input::placeholder{
      color: rgba(255,255,255,0.40);
    }
    .hidden{ display:none; }
  </style>
</head>
<body>
  <div class="phone">
    <div class="screen">
      <div class="island" aria-hidden="true"></div>
      <div class="header">
        <div class="title">MedSpa AI - SMS</div>
        <div class="meta">
          <div>Org: <code id="orgID">--</code></div>
          <div>Customer: <code id="customerPhone">--</code></div>
          <div>Clinic: <code id="clinicPhone">--</code></div>
        </div>
        <div class="status">
          <div class="pill"><span id="dot" class="dot"></span><span id="statusText">Waiting for config...</span></div>
          <div id="lastUpdated">--</div>
        </div>
      </div>
      <div id="messages" class="messages"></div>
      <div class="composer">
        <div class="hint">
          <span>Polling: <code id="pollMs">800</code>ms</span>
          <span>Endpoint: <code id="endpoint">--</code></span>
        </div>
        <input id="configInput" placeholder="Optional: orgID,customerPhone,clinicPhone (comma-separated) then press Enter" />
      </div>
    </div>
  </div>

  <script>
    const POLL_MS = Number(new URLSearchParams(location.search).get("poll_ms") || "800");
    document.getElementById("pollMs").textContent = String(POLL_MS);

    function qs(key) {
      return new URLSearchParams(location.search).get(key) || "";
    }

    function setText(id, value) {
      const el = document.getElementById(id);
      if (el) el.textContent = value || "--";
    }

    function formatTime(iso) {
      try {
        const d = new Date(iso);
        if (Number.isNaN(d.getTime())) return "";
        return d.toLocaleTimeString([], { hour: "numeric", minute: "2-digit" });
      } catch {
        return "";
      }
    }

    function escapeHTML(str) {
      return (str || "").replace(/[&<>"']/g, (c) => ({
        "&": "&amp;",
        "<": "&lt;",
        ">": "&gt;",
        '"': "&quot;",
        "'": "&#39;"
      }[c]));
    }

    function linkify(text) {
      const safe = escapeHTML(text || "");
      const urlRe = /(https?:\/\/[^\s<]+)/g;
      return safe.replace(urlRe, (m) => '<a href="' + m + '" target="_blank" rel="noopener noreferrer">' + m + "</a>");
    }

    function dayLabel(iso) {
      try {
        const d = new Date(iso);
        if (Number.isNaN(d.getTime())) return "";
        return d.toLocaleDateString([], { weekday: "short", month: "short", day: "numeric" });
      } catch {
        return "";
      }
    }

    function shouldHighlight(msg) {
      const kind = (msg.kind || "").toLowerCase();
      if (kind === "deposit_link" || kind === "payment_confirmation") return true;
      const body = (msg.body || "").toLowerCase();
      if (body.includes("deposit") || body.includes("priority booking")) return true;
      if (body.startsWith("payment received") || body.includes("payment of $")) return true;
      if (body.includes("secure") && body.includes("http")) return true;
      return false;
    }

    function render(messages) {
      const root = document.getElementById("messages");
      if (!Array.isArray(messages)) messages = [];

      let html = "";
      let lastDay = "";
      for (const m of messages) {
        const role = (m.role || "assistant").toLowerCase() === "user" ? "user" : "assistant";
        const ts = m.timestamp || "";
        const d = dayLabel(ts);
        if (d && d !== lastDay) {
          html += '<div class="day">' + escapeHTML(d) + "</div>";
          lastDay = d;
        }
        const bubbleClass = "bubble " + role + (shouldHighlight(m) ? " highlight" : "");
        const body = linkify(m.body || "");
        const t = formatTime(ts);
        html += '<div class="row ' + role + '"><div><div class="' + bubbleClass + '">' + body + '</div><div class="time">' + escapeHTML(t) + "</div></div></div>";
      }
      if (!html) {
        html = '<div class="day">No messages yet</div>';
      }
      const prevScroll = root.scrollTop;
      const atBottom = (root.scrollHeight - root.clientHeight - prevScroll) < 40;
      root.innerHTML = html;
      if (atBottom) {
        root.scrollTop = root.scrollHeight;
      }
    }

    function setStatus(ok, text) {
      const dot = document.getElementById("dot");
      dot.classList.remove("ok", "bad");
      dot.classList.add(ok ? "ok" : "bad");
      document.getElementById("statusText").textContent = text;
    }

    function buildEndpoint(orgID, phone) {
      if (!orgID || !phone) return "";
      return "/admin/clinics/" + encodeURIComponent(orgID) + "/sms/" + encodeURIComponent(phone) + "?limit=500";
    }

    let state = {
      orgID: qs("orgID") || qs("org_id"),
      customerPhone: qs("phone") || qs("customer") || qs("customer_phone"),
      clinicPhone: qs("clinic") || qs("clinic_phone"),
      lastHash: "",
    };

    const input = document.getElementById("configInput");
    input.addEventListener("keydown", (e) => {
      if (e.key !== "Enter") return;
      const raw = (input.value || "").trim();
      if (!raw) return;
      const parts = raw.split(",").map(s => s.trim());
      if (parts.length >= 1) state.orgID = parts[0] || state.orgID;
      if (parts.length >= 2) state.customerPhone = parts[1] || state.customerPhone;
      if (parts.length >= 3) state.clinicPhone = parts[2] || state.clinicPhone;
      input.value = "";
      state.lastHash = "";
    });

    async function pollOnce() {
      setText("orgID", state.orgID);
      setText("customerPhone", state.customerPhone);
      setText("clinicPhone", state.clinicPhone);

      const endpoint = buildEndpoint(state.orgID, state.customerPhone);
      setText("endpoint", endpoint || "missing orgID/phone");
      if (!endpoint) {
        setStatus(false, "Missing orgID and/or phone");
        render([]);
        return;
      }

      try {
        const resp = await fetch(endpoint, { cache: "no-store" });
        if (!resp.ok) {
          setStatus(false, "HTTP " + String(resp.status));
          return;
        }
        const data = await resp.json();
        const msgs = (data && data.messages) || [];
        const hash = JSON.stringify(msgs.map(m => [m.id, m.role, m.body, m.timestamp, m.kind]));
        if (hash !== state.lastHash) {
          state.lastHash = hash;
          render(msgs);
        }
        setStatus(true, "Live");
        document.getElementById("lastUpdated").textContent = new Date().toLocaleTimeString([], { hour: "numeric", minute: "2-digit", second: "2-digit" });
      } catch (err) {
        setStatus(false, err && err.message ? err.message : "Fetch failed");
      }
    }

    setStatus(false, "Starting...");
    pollOnce();
    setInterval(pollOnce, POLL_MS);
  </script>
</body>
</html>`

// PhoneSimulator renders an iPhone-style chat UI that polls the SMS transcript endpoint.
// Route: GET /admin/e2e/phone-simulator?orgID=...&phone=...&clinic=...
func (h *Handler) PhoneSimulator(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(phoneSimulatorHTML))
}
