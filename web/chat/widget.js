(function() {
  'use strict';

  var script = document.currentScript;
  var orgID = script && script.getAttribute('data-org') || '';
  var primaryColor = script && script.getAttribute('data-color') || '#6366f1';
  var greeting = script && script.getAttribute('data-greeting') || 'Hi! How can we help you today?';
  var baseURL = script && script.src ? script.src.replace(/\/chat\/widget\.js.*$/, '') : '';

  if (!orgID) {
    console.error('MedSpa Chat: data-org attribute is required');
    return;
  }

  var SESSION_KEY = 'medspa_chat_session_' + orgID;
  var STATE_KEY = 'medspa_chat_state_' + orgID;

  function getSessionID() {
    var sid = localStorage.getItem(SESSION_KEY);
    if (!sid) {
      sid = 'ws_' + Math.random().toString(36).substr(2, 16) + Date.now().toString(36);
      localStorage.setItem(SESSION_KEY, sid);
    }
    return sid;
  }

  var sessionID = getSessionID();
  var ws = null;
  var isOpen = localStorage.getItem(STATE_KEY) === 'open';
  var messages = [];
  var isTyping = false;
  var reconnectAttempts = 0;
  var maxReconnect = 5;
  var reconnectTimer = null;

  // Create shadow DOM container
  var host = document.createElement('div');
  host.id = 'medspa-chat-widget';
  var shadow = host.attachShadow({ mode: 'closed' });

  var style = document.createElement('style');
  style.textContent = '\
    * { box-sizing: border-box; margin: 0; padding: 0; }\
    .chat-bubble {\
      position: fixed; bottom: 20px; right: 20px; width: 60px; height: 60px;\
      border-radius: 50%; background: ' + primaryColor + '; cursor: pointer;\
      display: flex; align-items: center; justify-content: center;\
      box-shadow: 0 4px 12px rgba(0,0,0,0.15); z-index: 99999;\
      transition: transform 0.2s;\
    }\
    .chat-bubble:hover { transform: scale(1.1); }\
    .chat-bubble svg { width: 28px; height: 28px; fill: white; }\
    .chat-window {\
      position: fixed; bottom: 90px; right: 20px; width: 380px; max-width: calc(100vw - 40px);\
      height: 520px; max-height: calc(100vh - 120px);\
      background: #fff; border-radius: 16px; overflow: hidden;\
      box-shadow: 0 8px 30px rgba(0,0,0,0.12); z-index: 99999;\
      display: flex; flex-direction: column;\
      transition: opacity 0.2s, transform 0.2s;\
    }\
    .chat-window.hidden { display: none; }\
    .chat-header {\
      background: ' + primaryColor + '; color: white; padding: 16px 20px;\
      font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif;\
      font-size: 16px; font-weight: 600;\
      display: flex; justify-content: space-between; align-items: center;\
    }\
    .chat-close {\
      background: none; border: none; color: white; font-size: 20px;\
      cursor: pointer; padding: 4px 8px; line-height: 1;\
    }\
    .chat-messages {\
      flex: 1; overflow-y: auto; padding: 16px; display: flex;\
      flex-direction: column; gap: 8px;\
      font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif;\
      font-size: 14px; line-height: 1.5;\
    }\
    .msg {\
      max-width: 80%; padding: 10px 14px; border-radius: 16px;\
      word-wrap: break-word; white-space: pre-wrap;\
    }\
    .msg.user {\
      align-self: flex-end; background: ' + primaryColor + '; color: white;\
      border-bottom-right-radius: 4px;\
    }\
    .msg.assistant {\
      align-self: flex-start; background: #f0f0f0; color: #333;\
      border-bottom-left-radius: 4px;\
    }\
    .typing {\
      align-self: flex-start; background: #f0f0f0; color: #999;\
      padding: 10px 14px; border-radius: 16px; border-bottom-left-radius: 4px;\
      font-style: italic;\
    }\
    .typing-dots span {\
      display: inline-block; width: 6px; height: 6px; margin: 0 2px;\
      background: #999; border-radius: 50%; animation: blink 1.4s infinite;\
    }\
    .typing-dots span:nth-child(2) { animation-delay: 0.2s; }\
    .typing-dots span:nth-child(3) { animation-delay: 0.4s; }\
    @keyframes blink { 0%,80%,100% { opacity: 0.3; } 40% { opacity: 1; } }\
    .chat-input-area {\
      display: flex; padding: 12px; border-top: 1px solid #e5e5e5;\
      background: #fafafa;\
    }\
    .chat-input {\
      flex: 1; border: 1px solid #ddd; border-radius: 24px; padding: 10px 16px;\
      font-size: 14px; outline: none; resize: none;\
      font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif;\
    }\
    .chat-input:focus { border-color: ' + primaryColor + '; }\
    .chat-send {\
      background: ' + primaryColor + '; border: none; color: white; width: 40px;\
      height: 40px; border-radius: 50%; cursor: pointer; margin-left: 8px;\
      display: flex; align-items: center; justify-content: center; flex-shrink: 0;\
    }\
    .chat-send:hover { opacity: 0.9; }\
    .chat-send:disabled { opacity: 0.5; cursor: not-allowed; }\
    .chat-send svg { width: 18px; height: 18px; fill: white; }\
    @media (max-width: 480px) {\
      .chat-window { bottom: 0; right: 0; width: 100%; max-width: 100%; height: 100%; max-height: 100%; border-radius: 0; }\
      .chat-bubble { bottom: 16px; right: 16px; }\
    }\
  ';

  var container = document.createElement('div');
  shadow.appendChild(style);
  shadow.appendChild(container);

  function render() {
    var html = '';

    // Chat bubble
    html += '<div class="chat-bubble" id="bubble">' +
      '<svg viewBox="0 0 24 24"><path d="M20 2H4c-1.1 0-2 .9-2 2v18l4-4h14c1.1 0 2-.9 2-2V4c0-1.1-.9-2-2-2z"/></svg>' +
      '</div>';

    // Chat window
    html += '<div class="chat-window ' + (isOpen ? '' : 'hidden') + '" id="window">';
    html += '<div class="chat-header"><span>Chat with us</span><button class="chat-close" id="close">&times;</button></div>';
    html += '<div class="chat-messages" id="messages">';

    for (var i = 0; i < messages.length; i++) {
      html += '<div class="msg ' + messages[i].role + '">' + escapeHtml(messages[i].text) + '</div>';
    }
    if (isTyping) {
      html += '<div class="typing"><span class="typing-dots"><span></span><span></span><span></span></span></div>';
    }

    html += '</div>';
    html += '<div class="chat-input-area">';
    html += '<input type="text" class="chat-input" id="input" placeholder="Type a message..." autocomplete="off" />';
    html += '<button class="chat-send" id="send">' +
      '<svg viewBox="0 0 24 24"><path d="M2.01 21L23 12 2.01 3 2 10l15 2-15 2z"/></svg>' +
      '</button>';
    html += '</div></div>';

    container.innerHTML = html;

    // Bind events
    var bubble = shadow.getElementById('bubble');
    var closeBtn = shadow.getElementById('close');
    var input = shadow.getElementById('input');
    var sendBtn = shadow.getElementById('send');
    var msgContainer = shadow.getElementById('messages');

    bubble.addEventListener('click', function() {
      isOpen = !isOpen;
      localStorage.setItem(STATE_KEY, isOpen ? 'open' : 'closed');
      render();
      if (isOpen) { connectWS(); scrollToBottom(); }
    });

    if (closeBtn) {
      closeBtn.addEventListener('click', function() {
        isOpen = false;
        localStorage.setItem(STATE_KEY, 'closed');
        render();
      });
    }

    if (input) {
      input.addEventListener('keydown', function(e) {
        if (e.key === 'Enter' && !e.shiftKey) {
          e.preventDefault();
          sendMessage();
        }
      });
      if (isOpen) {
        setTimeout(function() { input.focus(); }, 50);
      }
    }

    if (sendBtn) {
      sendBtn.addEventListener('click', sendMessage);
    }

    if (msgContainer) {
      scrollToBottom();
    }
  }

  function scrollToBottom() {
    var el = shadow.getElementById('messages');
    if (el) {
      setTimeout(function() { el.scrollTop = el.scrollHeight; }, 10);
    }
  }

  function escapeHtml(str) {
    var div = document.createElement('div');
    div.appendChild(document.createTextNode(str));
    return div.innerHTML;
  }

  function sendMessage() {
    var input = shadow.getElementById('input');
    if (!input) return;
    var text = input.value.trim();
    if (!text) return;

    messages.push({ role: 'user', text: text });
    input.value = '';
    isTyping = true;
    render();

    if (ws && ws.readyState === WebSocket.OPEN) {
      ws.send(JSON.stringify({ type: 'message', org_id: orgID, session_id: sessionID, text: text }));
    } else {
      // HTTP fallback
      fetch(baseURL + '/chat/message', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ org_id: orgID, session_id: sessionID, text: text })
      }).catch(function(err) {
        console.error('MedSpa Chat: send failed', err);
        isTyping = false;
        render();
      });
    }
  }

  function connectWS() {
    if (ws && (ws.readyState === WebSocket.OPEN || ws.readyState === WebSocket.CONNECTING)) return;

    var wsURL = baseURL.replace(/^http/, 'ws') + '/chat/ws?org=' + encodeURIComponent(orgID) + '&session=' + encodeURIComponent(sessionID);

    try {
      ws = new WebSocket(wsURL);
    } catch (e) {
      console.warn('MedSpa Chat: WebSocket not available, using HTTP fallback');
      loadHistory();
      return;
    }

    ws.onopen = function() {
      reconnectAttempts = 0;
    };

    ws.onmessage = function(evt) {
      var data;
      try { data = JSON.parse(evt.data); } catch (e) { return; }

      switch (data.type) {
        case 'session':
          if (data.session_id) {
            sessionID = data.session_id;
            localStorage.setItem(SESSION_KEY, sessionID);
          }
          break;
        case 'history':
          if (data.messages && data.messages.length > 0) {
            messages = [];
            for (var i = 0; i < data.messages.length; i++) {
              messages.push({ role: data.messages[i].role, text: data.messages[i].text });
            }
            render();
          }
          break;
        case 'message':
          isTyping = false;
          messages.push({ role: data.role || 'assistant', text: data.text });
          render();
          break;
        case 'typing':
          isTyping = true;
          render();
          break;
        case 'pong':
          break;
        case 'error':
          isTyping = false;
          messages.push({ role: 'assistant', text: data.text || 'Something went wrong.' });
          render();
          break;
      }
    };

    ws.onclose = function() {
      ws = null;
      if (isOpen && reconnectAttempts < maxReconnect) {
        reconnectAttempts++;
        var delay = Math.min(1000 * Math.pow(2, reconnectAttempts), 30000);
        reconnectTimer = setTimeout(connectWS, delay);
      }
    };

    ws.onerror = function() {
      // onclose will fire after this
    };

    // Keepalive
    setInterval(function() {
      if (ws && ws.readyState === WebSocket.OPEN) {
        ws.send(JSON.stringify({ type: 'ping' }));
      }
    }, 30000);
  }

  function loadHistory() {
    fetch(baseURL + '/chat/history?org=' + encodeURIComponent(orgID) + '&session=' + encodeURIComponent(sessionID))
      .then(function(r) { return r.json(); })
      .then(function(data) {
        if (data.messages && data.messages.length > 0) {
          messages = [];
          for (var i = 0; i < data.messages.length; i++) {
            messages.push({ role: data.messages[i].role, text: data.messages[i].text });
          }
          render();
        }
      })
      .catch(function() {});
  }

  // Show greeting on first visit
  if (messages.length === 0 && greeting) {
    messages.push({ role: 'assistant', text: greeting });
  }

  // Init
  document.body.appendChild(host);
  render();
  if (isOpen) { connectWS(); }
})();
