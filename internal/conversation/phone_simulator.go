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
    /* Typing indicator */
    .typing-indicator{
      display: flex;
      gap: 4px;
      padding: 12px 16px;
      background: var(--bubble-in);
      border-radius: 18px 18px 18px 4px;
      width: fit-content;
    }
    .typing-dot{
      width: 8px;
      height: 8px;
      background: var(--muted);
      border-radius: 50%;
      animation: typing-bounce 1.4s infinite ease-in-out both;
    }
    .typing-dot:nth-child(1){ animation-delay: 0s; }
    .typing-dot:nth-child(2){ animation-delay: 0.2s; }
    .typing-dot:nth-child(3){ animation-delay: 0.4s; }
    @keyframes typing-bounce{
      0%, 80%, 100%{ transform: scale(0.6); opacity: 0.4; }
      40%{ transform: scale(1); opacity: 1; }
    }
    .row.typing{ margin-top: 12px; }
    /* Incoming call overlay */
    .call-overlay{
      position: absolute;
      top: 0;
      left: 0;
      right: 0;
      bottom: 0;
      background: linear-gradient(180deg, #1c1c1e 0%, #2c2c2e 100%);
      z-index: 200;
      display: none;
      flex-direction: column;
      align-items: center;
      justify-content: flex-start;
      padding-top: 80px;
      border-radius: 36px;
    }
    .call-overlay.active{
      display: flex;
    }
    .call-avatar{
      width: 120px;
      height: 120px;
      border-radius: 50%;
      background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
      display: flex;
      align-items: center;
      justify-content: center;
      font-size: 48px;
      color: white;
      margin-bottom: 24px;
      box-shadow: 0 8px 32px rgba(102, 126, 234, 0.4);
    }
    .call-name{
      font-size: 32px;
      font-weight: 300;
      color: white;
      margin-bottom: 8px;
    }
    .call-status{
      font-size: 18px;
      color: rgba(255,255,255,0.7);
      margin-bottom: 40px;
    }
    .call-rings{
      display: flex;
      gap: 8px;
      margin-bottom: 60px;
    }
    .call-ring{
      width: 12px;
      height: 12px;
      border-radius: 50%;
      background: rgba(255,255,255,0.3);
      transition: background 0.3s;
    }
    .call-ring.active{
      background: #34c759;
      box-shadow: 0 0 12px rgba(52, 199, 89, 0.6);
    }
    .call-buttons{
      display: flex;
      gap: 60px;
      margin-top: auto;
      margin-bottom: 80px;
    }
    .call-btn{
      width: 72px;
      height: 72px;
      border-radius: 50%;
      border: none;
      display: flex;
      align-items: center;
      justify-content: center;
      font-size: 28px;
      cursor: pointer;
      transition: transform 0.2s;
    }
    .call-btn:hover{
      transform: scale(1.1);
    }
    .call-btn.decline{
      background: #ff3b30;
      color: white;
    }
    .call-btn.accept{
      background: #34c759;
      color: white;
    }
    /* Browser view styles */
    .browser-view{
      position: absolute;
      top: 0;
      left: 0;
      right: 0;
      bottom: 0;
      background: #fff;
      z-index: 100;
      display: none;
      flex-direction: column;
      border-radius: 36px;
      overflow: hidden;
    }
    .browser-view.active{
      display: flex;
    }
    .browser-bar{
      background: #f5f5f5;
      padding: 52px 12px 10px 12px;
      border-bottom: 1px solid #ddd;
      display: flex;
      align-items: center;
      gap: 8px;
    }
    .browser-url{
      flex: 1;
      background: #fff;
      border: 1px solid #ddd;
      border-radius: 8px;
      padding: 8px 12px;
      font-size: 12px;
      color: #333;
      white-space: nowrap;
      overflow: hidden;
      text-overflow: ellipsis;
    }
    .browser-url .secure{
      color: #34c759;
      margin-right: 4px;
    }
    .browser-close{
      background: #e5e5e5;
      border: none;
      border-radius: 50%;
      width: 28px;
      height: 28px;
      cursor: pointer;
      font-size: 16px;
      color: #666;
      display: flex;
      align-items: center;
      justify-content: center;
    }
    .browser-frame{
      flex: 1;
      border: none;
      background: #fff;
    }
  </style>
</head>
<body>
  <div class="phone">
    <div class="screen">
      <div class="island" aria-hidden="true"></div>
      <!-- Incoming call overlay -->
      <div id="callOverlay" class="call-overlay">
        <div class="call-avatar">🏥</div>
        <div class="call-name" id="callName">Cleveland Primecare</div>
        <div class="call-status" id="callStatus">incoming call...</div>
        <div class="call-rings">
          <div class="call-ring" id="ring1"></div>
          <div class="call-ring" id="ring2"></div>
          <div class="call-ring" id="ring3"></div>
          <div class="call-ring" id="ring4"></div>
        </div>
        <div class="call-buttons">
          <button class="call-btn decline" onclick="window.declineCall()" title="Decline">✕</button>
          <button class="call-btn accept" onclick="window.acceptCall()" title="Accept">📞</button>
        </div>
      </div>
      <!-- Browser view overlay (for showing checkout pages) -->
      <div id="browserView" class="browser-view">
        <div class="browser-bar">
          <div class="browser-url">
            <span class="secure">🔒</span>
            <span id="browserUrlText">square.com</span>
          </div>
          <button class="browser-close" onclick="window.closeBrowser()" title="Close">✕</button>
        </div>
        <iframe id="browserFrame" class="browser-frame" sandbox="allow-scripts allow-same-origin allow-forms allow-popups"></iframe>
      </div>
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
      // Show typing indicator if last message was from user (AI is responding)
      if (messages.length > 0) {
        const lastMsg = messages[messages.length - 1];
        if ((lastMsg.role || "").toLowerCase() === "user" && window._showTyping) {
          html += '<div class="row typing assistant"><div><div class="typing-indicator"><div class="typing-dot"></div><div class="typing-dot"></div><div class="typing-dot"></div></div></div></div>';
        }
      }
      const prevScroll = root.scrollTop;
      const atBottom = (root.scrollHeight - root.clientHeight - prevScroll) < 40;
      root.innerHTML = html;
      if (atBottom) {
        root.scrollTop = root.scrollHeight;
      }
    }

    // Typing indicator control
    window._showTyping = false;
    window.showTyping = function() {
      window._showTyping = true;
      pollOnce();
    };
    window.hideTyping = function() {
      window._showTyping = false;
    };

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

    // Incoming call functions (called from Playwright)
    let callInterval = null;
    let ringCount = 0;

    window.showIncomingCall = function(clinicName) {
      const overlay = document.getElementById("callOverlay");
      const nameEl = document.getElementById("callName");
      const statusEl = document.getElementById("callStatus");

      nameEl.textContent = clinicName || "Cleveland Primecare";
      statusEl.textContent = "incoming call...";
      ringCount = 0;

      // Reset ring indicators
      for (let i = 1; i <= 4; i++) {
        document.getElementById("ring" + i).classList.remove("active");
      }

      overlay.classList.add("active");

      // Play ring sound and animate rings
      callInterval = setInterval(function() {
        ringCount++;
        if (ringCount <= 4) {
          document.getElementById("ring" + ringCount).classList.add("active");
          // Play ring tone
          try {
            const ctx = new (window.AudioContext || window.webkitAudioContext)();
            const osc = ctx.createOscillator();
            const gain = ctx.createGain();
            osc.connect(gain);
            gain.connect(ctx.destination);
            osc.frequency.setValueAtTime(440, ctx.currentTime);
            osc.frequency.setValueAtTime(480, ctx.currentTime + 0.15);
            gain.gain.setValueAtTime(0.15, ctx.currentTime);
            gain.gain.exponentialRampToValueAtTime(0.01, ctx.currentTime + 0.4);
            osc.start(ctx.currentTime);
            osc.stop(ctx.currentTime + 0.4);
          } catch(e) {}
        }
      }, 1200);
    };

    window.endCall = function(reason) {
      const overlay = document.getElementById("callOverlay");
      const statusEl = document.getElementById("callStatus");

      if (callInterval) {
        clearInterval(callInterval);
        callInterval = null;
      }

      statusEl.textContent = reason || "call ended";

      setTimeout(function() {
        overlay.classList.remove("active");
      }, 1000);
    };

    window.declineCall = function() {
      window.endCall("call declined");
    };

    window.acceptCall = function() {
      window.endCall("connected");
    };

    // Browser view functions (called from Playwright)
    window.openBrowser = function(url) {
      const browserView = document.getElementById("browserView");
      const browserFrame = document.getElementById("browserFrame");
      const browserUrlText = document.getElementById("browserUrlText");

      // Extract domain for display
      try {
        const parsed = new URL(url);
        browserUrlText.textContent = parsed.hostname + parsed.pathname.substring(0, 30) + (parsed.pathname.length > 30 ? "..." : "");
      } catch {
        browserUrlText.textContent = url.substring(0, 40);
      }

      browserFrame.src = url;
      browserView.classList.add("active");
    };

    window.closeBrowser = function() {
      const browserView = document.getElementById("browserView");
      const browserFrame = document.getElementById("browserFrame");
      browserFrame.src = "about:blank";
      browserView.classList.remove("active");
    };

    // Intercept link clicks to open in embedded browser
    document.addEventListener("click", function(e) {
      const link = e.target.closest("a");
      if (link && link.href && (link.href.includes("square") || link.href.includes("checkout") || link.href.includes("pay"))) {
        e.preventDefault();
        window.openBrowser(link.href);
      }
    });
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
