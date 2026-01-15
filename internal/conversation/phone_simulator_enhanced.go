package conversation

import (
	"net/http"
)

const enhancedPhoneSimulatorHTML = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1, viewport-fit=cover" />
  <title>MedSpa AI - Phone Demo</title>
  <style>
    :root{
      --bg: #0b0f14;
      --phone-bg: #000;
      --header-bg: rgba(16, 24, 40, 0.92);
      --muted: rgba(255,255,255,0.68);
      --text: rgba(255,255,255,0.92);
      --bubble-in: #3b3b3d;
      --bubble-out: #007aff;
      --bubble-out-text: #ffffff;
      --bubble-in-text: rgba(255,255,255,0.95);
      --link: #7dd3ff;
      --danger: #ff453a;
      --ok: #30d158;
      --shadow: 0 24px 80px rgba(0,0,0,0.55);
      --font: -apple-system, BlinkMacSystemFont, 'SF Pro Display', 'SF Pro Text', system-ui, sans-serif;
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
      overflow: hidden;
    }

    /* Phone frame */
    .phone{
      width: 393px;
      height: 852px;
      max-width: 96vw;
      max-height: 94vh;
      border-radius: 55px;
      background: #1c1c1e;
      padding: 12px;
      box-shadow: var(--shadow), inset 0 0 0 3px #2c2c2e;
      position: relative;
    }
    .screen{
      width: 100%;
      height: 100%;
      border-radius: 47px;
      overflow: hidden;
      background: var(--phone-bg);
      display:flex;
      flex-direction: column;
      position: relative;
    }

    /* Dynamic island */
    .island{
      position:absolute;
      top: 11px;
      left: 50%;
      transform: translateX(-50%);
      width: 126px;
      height: 37px;
      border-radius: 24px;
      background: #000;
      z-index: 50;
    }

    /* Status bar */
    .status-bar{
      position: absolute;
      top: 0;
      left: 0;
      right: 0;
      height: 54px;
      padding: 14px 28px 0;
      display: flex;
      justify-content: space-between;
      align-items: flex-start;
      z-index: 40;
      font-size: 15px;
      font-weight: 600;
    }
    .status-bar .time{
      letter-spacing: -0.3px;
    }
    .status-bar .icons{
      display: flex;
      align-items: center;
      gap: 5px;
    }
    .status-bar .icons svg{
      height: 12px;
    }

    /* iOS Messages header */
    .msg-header{
      margin-top: 54px;
      padding: 8px 16px 12px;
      display: flex;
      align-items: center;
      gap: 12px;
      border-bottom: 0.5px solid rgba(255,255,255,0.1);
    }
    .msg-header .back{
      color: #007aff;
      font-size: 17px;
      display: flex;
      align-items: center;
      gap: 4px;
    }
    .msg-header .back svg{
      width: 12px;
      height: 20px;
    }
    .msg-header .avatar{
      width: 40px;
      height: 40px;
      border-radius: 50%;
      background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
      display: flex;
      align-items: center;
      justify-content: center;
      font-size: 18px;
      color: white;
      flex-shrink: 0;
    }
    .msg-header .contact{
      flex: 1;
      min-width: 0;
    }
    .msg-header .contact-name{
      font-size: 16px;
      font-weight: 600;
      white-space: nowrap;
      overflow: hidden;
      text-overflow: ellipsis;
    }
    .msg-header .contact-status{
      font-size: 12px;
      color: var(--muted);
    }

    /* Messages area */
    .messages{
      flex: 1 1 auto;
      overflow-y: auto;
      padding: 12px 16px 10px;
      scroll-behavior: smooth;
    }
    .day{
      text-align:center;
      margin: 16px 0;
      font-size: 12px;
      color: rgba(255,255,255,0.45);
      font-weight: 500;
    }
    .row{
      display:flex;
      width: 100%;
      margin: 3px 0;
    }
    .row.user{ justify-content: flex-end; }
    .row.assistant{ justify-content: flex-start; }
    .bubble{
      max-width: 75%;
      padding: 9px 14px;
      border-radius: 18px;
      line-height: 1.3;
      font-size: 16px;
      white-space: pre-wrap;
      word-break: break-word;
    }
    .bubble.user{
      background: var(--bubble-out);
      color: var(--bubble-out-text);
      border-bottom-right-radius: 4px;
    }
    .bubble.assistant{
      background: var(--bubble-in);
      color: var(--bubble-in-text);
      border-bottom-left-radius: 4px;
    }
    .bubble.highlight{
      box-shadow: 0 0 0 2px rgba(48, 209, 88, 0.4);
    }
    .bubble a{
      color: var(--link);
      text-decoration: underline;
      text-underline-offset: 2px;
    }
    .time-label{
      margin-top: 4px;
      font-size: 11px;
      color: rgba(255,255,255,0.4);
      text-align: right;
      padding-right: 4px;
    }
    .row.assistant .time-label{
      text-align: left;
      padding-left: 4px;
    }

    /* Composer bar */
    .composer{
      padding: 8px 12px 34px;
      background: rgba(28,28,30,0.95);
      border-top: 0.5px solid rgba(255,255,255,0.1);
      display: flex;
      align-items: center;
      gap: 8px;
    }
    .composer .input-wrap{
      flex: 1;
      background: rgba(255,255,255,0.08);
      border-radius: 20px;
      padding: 8px 16px;
      display: flex;
      align-items: center;
    }
    .composer .input-wrap input{
      background: transparent;
      border: none;
      outline: none;
      color: white;
      font-size: 16px;
      width: 100%;
    }
    .composer .input-wrap input::placeholder{
      color: rgba(255,255,255,0.4);
    }
    .composer .send-btn{
      width: 32px;
      height: 32px;
      border-radius: 50%;
      background: #007aff;
      border: none;
      display: flex;
      align-items: center;
      justify-content: center;
      cursor: pointer;
    }
    .composer .send-btn svg{
      width: 16px;
      height: 16px;
      fill: white;
    }

    /* Typing indicator */
    .typing-indicator{
      display: flex;
      gap: 4px;
      padding: 12px 14px;
      background: var(--bubble-in);
      border-radius: 18px 18px 18px 4px;
      width: fit-content;
    }
    .typing-dot{
      width: 8px;
      height: 8px;
      background: rgba(255,255,255,0.5);
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

    /* ============================================
       iOS NOTIFICATION BANNER
       ============================================ */
    .notification-banner{
      position: absolute;
      top: 60px;
      left: 8px;
      right: 8px;
      background: rgba(50,50,52,0.98);
      border-radius: 16px;
      padding: 12px 14px;
      display: flex;
      align-items: flex-start;
      gap: 10px;
      z-index: 100;
      transform: translateY(-120%);
      opacity: 0;
      transition: transform 0.35s cubic-bezier(0.4, 0, 0.2, 1), opacity 0.35s ease;
      box-shadow: 0 8px 32px rgba(0,0,0,0.4);
    }
    .notification-banner.show{
      transform: translateY(0);
      opacity: 1;
    }
    .notification-banner .app-icon{
      width: 38px;
      height: 38px;
      border-radius: 8px;
      background: linear-gradient(135deg, #30d158 0%, #34c759 100%);
      display: flex;
      align-items: center;
      justify-content: center;
      font-size: 20px;
      flex-shrink: 0;
    }
    .notification-banner .content{
      flex: 1;
      min-width: 0;
    }
    .notification-banner .header{
      display: flex;
      align-items: center;
      gap: 6px;
      margin-bottom: 2px;
    }
    .notification-banner .app-name{
      font-size: 13px;
      font-weight: 600;
      color: rgba(255,255,255,0.9);
    }
    .notification-banner .time{
      font-size: 12px;
      color: rgba(255,255,255,0.5);
    }
    .notification-banner .title{
      font-size: 14px;
      font-weight: 600;
      color: white;
      margin-bottom: 2px;
    }
    .notification-banner .preview{
      font-size: 14px;
      color: rgba(255,255,255,0.75);
      white-space: nowrap;
      overflow: hidden;
      text-overflow: ellipsis;
    }

    /* ============================================
       iOS INCOMING CALL SCREEN
       ============================================ */
    .call-overlay{
      position: absolute;
      top: 0;
      left: 0;
      right: 0;
      bottom: 0;
      background: linear-gradient(180deg, #1c1c1e 0%, #2c2c2e 50%, #1c1c1e 100%);
      z-index: 200;
      display: none;
      flex-direction: column;
      align-items: center;
      padding-top: 100px;
      border-radius: 47px;
    }
    .call-overlay.active{
      display: flex;
    }
    .call-label{
      font-size: 14px;
      color: rgba(255,255,255,0.6);
      margin-bottom: 8px;
      letter-spacing: 0.5px;
    }
    .call-name{
      font-size: 32px;
      font-weight: 300;
      color: white;
      margin-bottom: 4px;
      text-align: center;
      padding: 0 20px;
    }
    .call-status{
      font-size: 15px;
      color: rgba(255,255,255,0.6);
      margin-bottom: 60px;
    }
    .call-avatar{
      width: 100px;
      height: 100px;
      border-radius: 50%;
      background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
      display: flex;
      align-items: center;
      justify-content: center;
      font-size: 42px;
      color: white;
      margin-bottom: 20px;
      box-shadow: 0 8px 32px rgba(102, 126, 234, 0.3);
    }
    .call-actions{
      position: absolute;
      bottom: 60px;
      left: 0;
      right: 0;
      display: flex;
      justify-content: center;
      gap: 80px;
    }
    .call-btn{
      display: flex;
      flex-direction: column;
      align-items: center;
      gap: 8px;
    }
    .call-btn-circle{
      width: 64px;
      height: 64px;
      border-radius: 50%;
      border: none;
      display: flex;
      align-items: center;
      justify-content: center;
      cursor: pointer;
      transition: transform 0.2s;
    }
    .call-btn-circle:hover{
      transform: scale(1.08);
    }
    .call-btn-circle.decline{
      background: #ff3b30;
    }
    .call-btn-circle.accept{
      background: #30d158;
    }
    .call-btn-circle svg{
      width: 28px;
      height: 28px;
      fill: white;
    }
    .call-btn-label{
      font-size: 13px;
      color: rgba(255,255,255,0.8);
    }

    /* Call slide-to-answer (lock screen style) */
    .slide-container{
      position: absolute;
      bottom: 120px;
      left: 40px;
      right: 40px;
    }
    .slide-track{
      background: rgba(255,255,255,0.15);
      border-radius: 30px;
      height: 60px;
      display: flex;
      align-items: center;
      padding: 4px;
      position: relative;
    }
    .slide-thumb{
      width: 52px;
      height: 52px;
      border-radius: 50%;
      background: white;
      display: flex;
      align-items: center;
      justify-content: center;
      transition: transform 0.3s;
      z-index: 2;
    }
    .slide-thumb svg{
      width: 24px;
      height: 24px;
      fill: #30d158;
    }
    .slide-text{
      position: absolute;
      left: 0;
      right: 0;
      text-align: center;
      font-size: 18px;
      font-weight: 400;
      color: rgba(255,255,255,0.7);
      letter-spacing: 1px;
    }

    /* ============================================
       BROWSER VIEW (for checkout)
       ============================================ */
    .browser-view{
      position: absolute;
      top: 0;
      left: 0;
      right: 0;
      bottom: 0;
      background: #fff;
      z-index: 150;
      display: none;
      flex-direction: column;
      border-radius: 47px;
      overflow: hidden;
    }
    .browser-view.active{
      display: flex;
    }
    .browser-bar{
      background: #f9f9f9;
      padding: 58px 12px 10px 12px;
      border-bottom: 0.5px solid #ddd;
      display: flex;
      align-items: center;
      gap: 8px;
    }
    .browser-url{
      flex: 1;
      background: rgba(0,0,0,0.05);
      border-radius: 10px;
      padding: 10px 14px;
      font-size: 13px;
      color: #333;
      white-space: nowrap;
      overflow: hidden;
      text-overflow: ellipsis;
      display: flex;
      align-items: center;
      gap: 6px;
    }
    .browser-url .lock{
      color: #30d158;
    }
    .browser-close{
      background: none;
      border: none;
      font-size: 17px;
      color: #007aff;
      cursor: pointer;
      padding: 8px;
    }
    .browser-frame{
      flex: 1;
      border: none;
      background: #fff;
    }

    /* ============================================
       ILLUSTRATED HAND OVERLAY
       ============================================ */
    .hand-overlay{
      position: fixed;
      pointer-events: none;
      z-index: 10000;
      width: 200px;
      height: 200px;
      transition: transform 0.4s cubic-bezier(0.34, 1.56, 0.64, 1), opacity 0.3s;
      opacity: 0;
      transform: translate(-50%, -50%) scale(0.8);
    }
    .hand-overlay.visible{
      opacity: 1;
      transform: translate(-50%, -50%) scale(1);
    }
    .hand-overlay.tapping{
      transform: translate(-50%, -50%) scale(0.92);
    }
    .hand-svg{
      width: 100%;
      height: 100%;
      filter: drop-shadow(0 8px 20px rgba(0,0,0,0.3));
    }
    /* Tap ripple effect */
    .tap-ripple{
      position: fixed;
      width: 40px;
      height: 40px;
      border-radius: 50%;
      background: rgba(255,255,255,0.4);
      pointer-events: none;
      z-index: 9999;
      transform: translate(-50%, -50%) scale(0);
      opacity: 0;
    }
    .tap-ripple.animate{
      animation: ripple-expand 0.5s ease-out forwards;
    }
    @keyframes ripple-expand{
      0%{ transform: translate(-50%, -50%) scale(0); opacity: 0.8; }
      100%{ transform: translate(-50%, -50%) scale(2.5); opacity: 0; }
    }

    /* ============================================
       PAYMENT SUCCESS OVERLAY
       ============================================ */
    .payment-success{
      position: absolute;
      top: 0;
      left: 0;
      right: 0;
      bottom: 0;
      background: rgba(0,0,0,0.9);
      z-index: 160;
      display: none;
      flex-direction: column;
      align-items: center;
      justify-content: center;
      border-radius: 47px;
    }
    .payment-success.active{
      display: flex;
    }
    .success-check{
      width: 80px;
      height: 80px;
      border-radius: 50%;
      background: #30d158;
      display: flex;
      align-items: center;
      justify-content: center;
      margin-bottom: 24px;
      animation: check-pop 0.4s cubic-bezier(0.34, 1.56, 0.64, 1);
    }
    @keyframes check-pop{
      0%{ transform: scale(0); }
      100%{ transform: scale(1); }
    }
    .success-check svg{
      width: 40px;
      height: 40px;
      fill: white;
    }
    .success-title{
      font-size: 24px;
      font-weight: 600;
      color: white;
      margin-bottom: 8px;
    }
    .success-amount{
      font-size: 18px;
      color: rgba(255,255,255,0.7);
    }

    .hidden{ display:none !important; }
  </style>
</head>
<body>
  <div class="phone">
    <div class="screen">
      <div class="island"></div>

      <!-- Status bar -->
      <div class="status-bar">
        <div class="time" id="statusTime">9:41</div>
        <div class="icons">
          <svg viewBox="0 0 17 12" fill="white"><path d="M16.5 3.5v5a1 1 0 01-1 1h-1V2.5h1a1 1 0 011 1zM0 4a2 2 0 012-2h10a2 2 0 012 2v4a2 2 0 01-2 2H2a2 2 0 01-2-2V4z"/></svg>
          <svg viewBox="0 0 16 16" fill="white"><path d="M1 6a1 1 0 011-1h1v6H2a1 1 0 01-1-1V6zM4 4h1v8H4V4zM7 2h1v12H7V2zM10 4h1v8h-1V4zM13 6h1v4h-1V6z"/></svg>
          <svg viewBox="0 0 16 12" fill="white"><path d="M8 1C3.58 1 0 4.13 0 8c0 .34.03.67.08 1h3.03c-.07-.33-.11-.66-.11-1 0-2.76 2.24-5 5-5s5 2.24 5 5c0 .34-.04.67-.11 1h3.03c.05-.33.08-.66.08-1 0-3.87-3.58-7-8-7z"/></svg>
        </div>
      </div>

      <!-- Notification banner -->
      <div id="notificationBanner" class="notification-banner">
        <div class="app-icon">💬</div>
        <div class="content">
          <div class="header">
            <span class="app-name">Messages</span>
            <span class="time">now</span>
          </div>
          <div class="title" id="notifTitle">Wolf Aesthetics</div>
          <div class="preview" id="notifPreview">New message received</div>
        </div>
      </div>

      <!-- Incoming call overlay -->
      <div id="callOverlay" class="call-overlay">
        <div class="call-avatar" id="callAvatar">✨</div>
        <div class="call-label">incoming call</div>
        <div class="call-name" id="callName">Wolf Aesthetics</div>
        <div class="call-status" id="callStatus">mobile</div>
        <div class="call-actions">
          <div class="call-btn">
            <button class="call-btn-circle decline" onclick="window.declineCall()" title="Decline">
              <svg viewBox="0 0 24 24"><path d="M12 9c-1.6 0-3.15.25-4.6.72v3.1c0 .39-.23.74-.56.9-.98.49-1.87 1.12-2.66 1.85-.18.18-.43.28-.7.28-.28 0-.53-.11-.71-.29L.29 13.08c-.18-.17-.29-.42-.29-.7 0-.28.11-.53.29-.71C3.34 8.78 7.46 7 12 7s8.66 1.78 11.71 4.67c.18.18.29.43.29.71 0 .28-.11.53-.29.71l-2.48 2.48c-.18.18-.43.29-.71.29-.27 0-.52-.1-.7-.28-.79-.73-1.68-1.36-2.66-1.85-.33-.16-.56-.5-.56-.9v-3.1C15.15 9.25 13.6 9 12 9z"/></svg>
            </button>
            <span class="call-btn-label">Decline</span>
          </div>
          <div class="call-btn">
            <button class="call-btn-circle accept" onclick="window.acceptCall()" title="Accept">
              <svg viewBox="0 0 24 24"><path d="M20.01 15.38c-1.23 0-2.42-.2-3.53-.56-.35-.12-.74-.03-1.01.24l-1.57 1.97c-2.83-1.35-5.48-3.9-6.89-6.83l1.95-1.66c.27-.28.35-.67.24-1.02-.37-1.11-.56-2.3-.56-3.53 0-.54-.45-.99-.99-.99H4.19C3.65 3 3 3.24 3 3.99 3 13.28 10.73 21 20.01 21c.71 0 .99-.63.99-1.18v-3.45c0-.54-.45-.99-.99-.99z"/></svg>
            </button>
            <span class="call-btn-label">Accept</span>
          </div>
        </div>
      </div>

      <!-- Browser view (for checkout) -->
      <div id="browserView" class="browser-view">
        <div class="browser-bar">
          <div class="browser-url">
            <span class="lock">🔒</span>
            <span id="browserUrlText">checkout.square.com</span>
          </div>
          <button class="browser-close" onclick="window.closeBrowser()">Done</button>
        </div>
        <iframe id="browserFrame" class="browser-frame" sandbox="allow-scripts allow-same-origin allow-forms allow-popups"></iframe>
      </div>

      <!-- Payment success overlay -->
      <div id="paymentSuccess" class="payment-success">
        <div class="success-check">
          <svg viewBox="0 0 24 24"><path d="M9 16.17L4.83 12l-1.42 1.41L9 19 21 7l-1.41-1.41L9 16.17z"/></svg>
        </div>
        <div class="success-title">Payment Complete</div>
        <div class="success-amount" id="successAmount">$50.00 deposit confirmed</div>
      </div>

      <!-- Messages header -->
      <div class="msg-header">
        <div class="back">
          <svg viewBox="0 0 12 20" fill="currentColor"><path d="M11.67.33a1.13 1.13 0 00-1.6 0L.33 10l9.74 9.67a1.13 1.13 0 001.6-1.6L3.6 10l8.07-8.07a1.13 1.13 0 000-1.6z"/></svg>
        </div>
        <div class="avatar" id="contactAvatar">✨</div>
        <div class="contact">
          <div class="contact-name" id="contactName">Wolf Aesthetics</div>
          <div class="contact-status">Business Chat</div>
        </div>
      </div>

      <!-- Messages -->
      <div id="messages" class="messages"></div>

      <!-- Composer -->
      <div class="composer">
        <div class="input-wrap">
          <input id="messageInput" type="text" placeholder="iMessage" />
        </div>
        <button class="send-btn">
          <svg viewBox="0 0 24 24"><path d="M2.01 21L23 12 2.01 3 2 10l15 2-15 2z"/></svg>
        </button>
      </div>
    </div>
  </div>

  <!-- Illustrated hand overlay -->
  <div id="handOverlay" class="hand-overlay">
    <svg class="hand-svg" viewBox="0 0 200 200">
      <!-- Illustrated cartoon-style pointing hand -->
      <defs>
        <linearGradient id="skinGrad" x1="0%" y1="0%" x2="100%" y2="100%">
          <stop offset="0%" style="stop-color:#fce0c8"/>
          <stop offset="100%" style="stop-color:#e8c4a8"/>
        </linearGradient>
        <linearGradient id="nailGrad" x1="0%" y1="0%" x2="0%" y2="100%">
          <stop offset="0%" style="stop-color:#ffb6c1"/>
          <stop offset="100%" style="stop-color:#ff8da1"/>
        </linearGradient>
      </defs>
      <!-- Palm/hand base -->
      <ellipse cx="100" cy="150" rx="45" ry="40" fill="url(#skinGrad)" stroke="#d4a574" stroke-width="2"/>
      <!-- Index finger (pointing up) -->
      <rect x="85" y="30" width="30" height="90" rx="15" fill="url(#skinGrad)" stroke="#d4a574" stroke-width="2"/>
      <!-- Finger tip detail -->
      <ellipse cx="100" cy="35" rx="14" ry="12" fill="url(#skinGrad)" stroke="#d4a574" stroke-width="2"/>
      <!-- Fingernail -->
      <ellipse cx="100" cy="32" rx="8" ry="6" fill="url(#nailGrad)" stroke="#e57c8a" stroke-width="1"/>
      <!-- Knuckle line -->
      <path d="M88 85 Q100 80 112 85" stroke="#d4a574" stroke-width="2" fill="none"/>
      <!-- Curled fingers (behind) -->
      <ellipse cx="72" cy="125" rx="18" ry="22" fill="url(#skinGrad)" stroke="#d4a574" stroke-width="2"/>
      <ellipse cx="128" cy="125" rx="18" ry="22" fill="url(#skinGrad)" stroke="#d4a574" stroke-width="2"/>
      <!-- Thumb -->
      <ellipse cx="55" cy="145" rx="16" ry="25" fill="url(#skinGrad)" stroke="#d4a574" stroke-width="2" transform="rotate(-20 55 145)"/>
    </svg>
  </div>

  <!-- Tap ripple element -->
  <div id="tapRipple" class="tap-ripple"></div>

  <script>
    // ============================================
    // AUDIO - Sound effects
    // ============================================
    let audioContext = null;

    function getAudioContext() {
      if (!audioContext) {
        audioContext = new (window.AudioContext || window.webkitAudioContext)();
      }
      return audioContext;
    }

    // iPhone "sent" whoosh sound
    window.playSentSound = function() {
      try {
        const ctx = getAudioContext();
        const osc = ctx.createOscillator();
        const gain = ctx.createGain();
        osc.connect(gain);
        gain.connect(ctx.destination);
        osc.frequency.setValueAtTime(1200, ctx.currentTime);
        osc.frequency.exponentialRampToValueAtTime(1800, ctx.currentTime + 0.08);
        gain.gain.setValueAtTime(0.12, ctx.currentTime);
        gain.gain.exponentialRampToValueAtTime(0.01, ctx.currentTime + 0.12);
        osc.start(ctx.currentTime);
        osc.stop(ctx.currentTime + 0.12);
      } catch(e) {}
    };

    // iPhone tri-tone notification
    window.playTriTone = function() {
      try {
        const ctx = getAudioContext();
        const notes = [1046.5, 1318.5, 1568]; // C6, E6, G6
        notes.forEach((freq, i) => {
          const osc = ctx.createOscillator();
          const gain = ctx.createGain();
          osc.connect(gain);
          gain.connect(ctx.destination);
          osc.frequency.setValueAtTime(freq, ctx.currentTime + i * 0.1);
          gain.gain.setValueAtTime(0.1, ctx.currentTime + i * 0.1);
          gain.gain.exponentialRampToValueAtTime(0.01, ctx.currentTime + i * 0.1 + 0.12);
          osc.start(ctx.currentTime + i * 0.1);
          osc.stop(ctx.currentTime + i * 0.1 + 0.12);
        });
      } catch(e) {}
    };

    // Phone ring sound
    window.playRing = function() {
      try {
        const ctx = getAudioContext();
        [0, 0.15].forEach(delay => {
          const osc = ctx.createOscillator();
          const gain = ctx.createGain();
          osc.connect(gain);
          gain.connect(ctx.destination);
          osc.frequency.setValueAtTime(delay === 0 ? 440 : 480, ctx.currentTime + delay);
          gain.gain.setValueAtTime(0.12, ctx.currentTime + delay);
          gain.gain.exponentialRampToValueAtTime(0.01, ctx.currentTime + delay + 0.5);
          osc.start(ctx.currentTime + delay);
          osc.stop(ctx.currentTime + delay + 0.5);
        });
      } catch(e) {}
    };

    // Cha-ching payment sound
    window.playChaChing = function() {
      try {
        const ctx = getAudioContext();
        // First "cha"
        const osc1 = ctx.createOscillator();
        const gain1 = ctx.createGain();
        osc1.connect(gain1);
        gain1.connect(ctx.destination);
        osc1.frequency.setValueAtTime(2093, ctx.currentTime); // C7
        gain1.gain.setValueAtTime(0.15, ctx.currentTime);
        gain1.gain.exponentialRampToValueAtTime(0.01, ctx.currentTime + 0.08);
        osc1.start(ctx.currentTime);
        osc1.stop(ctx.currentTime + 0.08);

        // Second "ching"
        const osc2 = ctx.createOscillator();
        const gain2 = ctx.createGain();
        osc2.connect(gain2);
        gain2.connect(ctx.destination);
        osc2.frequency.setValueAtTime(2637, ctx.currentTime + 0.1); // E7
        gain2.gain.setValueAtTime(0.18, ctx.currentTime + 0.1);
        gain2.gain.exponentialRampToValueAtTime(0.01, ctx.currentTime + 0.25);
        osc2.start(ctx.currentTime + 0.1);
        osc2.stop(ctx.currentTime + 0.25);
      } catch(e) {}
    };

    // ============================================
    // NOTIFICATION BANNER
    // ============================================
    window.showNotification = function(title, preview, duration) {
      const banner = document.getElementById('notificationBanner');
      const titleEl = document.getElementById('notifTitle');
      const previewEl = document.getElementById('notifPreview');

      titleEl.textContent = title || 'Wolf Aesthetics';
      previewEl.textContent = preview || 'New message';

      banner.classList.add('show');
      window.playTriTone();

      setTimeout(function() {
        banner.classList.remove('show');
      }, duration || 4000);
    };

    window.hideNotification = function() {
      document.getElementById('notificationBanner').classList.remove('show');
    };

    // ============================================
    // INCOMING CALL
    // ============================================
    let callRingInterval = null;

    window.showIncomingCall = function(clinicName, avatar) {
      const overlay = document.getElementById('callOverlay');
      document.getElementById('callName').textContent = clinicName || 'Wolf Aesthetics';
      document.getElementById('callAvatar').textContent = avatar || '✨';
      overlay.classList.add('active');

      // Play ring every 2 seconds
      window.playRing();
      callRingInterval = setInterval(function() {
        window.playRing();
      }, 2000);
    };

    window.endCall = function(reason) {
      const overlay = document.getElementById('callOverlay');
      const status = document.getElementById('callStatus');

      if (callRingInterval) {
        clearInterval(callRingInterval);
        callRingInterval = null;
      }

      status.textContent = reason || 'call ended';
      setTimeout(function() {
        overlay.classList.remove('active');
        status.textContent = 'mobile';
      }, 1000);
    };

    window.declineCall = function() {
      window.endCall('declined');
    };

    window.acceptCall = function() {
      window.endCall('connected');
    };

    // ============================================
    // BROWSER VIEW (Checkout)
    // ============================================
    window.openBrowser = function(url) {
      const browserView = document.getElementById('browserView');
      const browserFrame = document.getElementById('browserFrame');
      const urlText = document.getElementById('browserUrlText');

      try {
        const parsed = new URL(url);
        urlText.textContent = parsed.hostname + parsed.pathname.substring(0, 25);
      } catch {
        urlText.textContent = url.substring(0, 35);
      }

      browserFrame.src = url;
      browserView.classList.add('active');
    };

    window.closeBrowser = function() {
      const browserView = document.getElementById('browserView');
      const browserFrame = document.getElementById('browserFrame');
      browserFrame.src = 'about:blank';
      browserView.classList.remove('active');
    };

    // ============================================
    // PAYMENT SUCCESS
    // ============================================
    window.showPaymentSuccess = function(amount) {
      const overlay = document.getElementById('paymentSuccess');
      document.getElementById('successAmount').textContent = amount || '$50.00 deposit confirmed';
      overlay.classList.add('active');
      window.playChaChing();
    };

    window.hidePaymentSuccess = function() {
      document.getElementById('paymentSuccess').classList.remove('active');
    };

    // ============================================
    // HAND OVERLAY
    // ============================================
    window.showHand = function(x, y) {
      const hand = document.getElementById('handOverlay');
      hand.style.left = x + 'px';
      hand.style.top = y + 'px';
      hand.classList.add('visible');
    };

    window.hideHand = function() {
      document.getElementById('handOverlay').classList.remove('visible');
    };

    window.tapHand = function(x, y) {
      const hand = document.getElementById('handOverlay');
      const ripple = document.getElementById('tapRipple');

      // Position hand if coordinates provided
      if (x !== undefined && y !== undefined) {
        hand.style.left = x + 'px';
        hand.style.top = y + 'px';
      }

      // Show hand and tap animation
      hand.classList.add('visible');
      hand.classList.add('tapping');

      // Show ripple at finger tip (offset from hand center)
      const handRect = hand.getBoundingClientRect();
      const tipX = parseFloat(hand.style.left);
      const tipY = parseFloat(hand.style.top) - 70; // Finger tip offset
      ripple.style.left = tipX + 'px';
      ripple.style.top = tipY + 'px';
      ripple.classList.remove('animate');
      void ripple.offsetWidth; // Trigger reflow
      ripple.classList.add('animate');

      setTimeout(function() {
        hand.classList.remove('tapping');
      }, 150);
    };

    window.moveHand = function(x, y) {
      const hand = document.getElementById('handOverlay');
      hand.style.left = x + 'px';
      hand.style.top = y + 'px';
    };

    // ============================================
    // MESSAGE RENDERING
    // ============================================
    function qs(key) {
      return new URLSearchParams(location.search).get(key) || "";
    }

    function escapeHTML(str) {
      return (str || "").replace(/[&<>"']/g, c => ({
        "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;"
      }[c]));
    }

    function linkify(text) {
      const safe = escapeHTML(text || "");
      return safe.replace(/(https?:\/\/[^\s<]+)/g, '<a href="$1" target="_blank">$1</a>');
    }

    function formatTime(iso) {
      try {
        const d = new Date(iso);
        if (Number.isNaN(d.getTime())) return "";
        return d.toLocaleTimeString([], { hour: "numeric", minute: "2-digit" });
      } catch { return ""; }
    }

    function dayLabel(iso) {
      try {
        const d = new Date(iso);
        if (Number.isNaN(d.getTime())) return "";
        return d.toLocaleDateString([], { weekday: "long", month: "long", day: "numeric" });
      } catch { return ""; }
    }

    function shouldHighlight(msg) {
      const kind = (msg.kind || "").toLowerCase();
      if (kind === "deposit_link" || kind === "payment_confirmation") return true;
      const body = (msg.body || "").toLowerCase();
      if (body.includes("deposit") || body.includes("priority booking")) return true;
      if (body.includes("payment received") || body.includes("payment of $")) return true;
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
          html += '<div class="day">' + escapeHTML(d) + '</div>';
          lastDay = d;
        }
        const bubbleClass = "bubble " + role + (shouldHighlight(m) ? " highlight" : "");
        html += '<div class="row ' + role + '"><div><div class="' + bubbleClass + '">' + linkify(m.body || "") + '</div><div class="time-label">' + escapeHTML(formatTime(ts)) + '</div></div></div>';
      }

      if (!html) {
        html = '<div class="day">No messages yet</div>';
      }

      // Typing indicator
      if (messages.length > 0 && window._showTyping) {
        const lastMsg = messages[messages.length - 1];
        if ((lastMsg.role || "").toLowerCase() === "user") {
          html += '<div class="row assistant"><div><div class="typing-indicator"><div class="typing-dot"></div><div class="typing-dot"></div><div class="typing-dot"></div></div></div></div>';
        }
      }

      const atBottom = (root.scrollHeight - root.clientHeight - root.scrollTop) < 50;
      root.innerHTML = html;
      if (atBottom) root.scrollTop = root.scrollHeight;
    }

    // Typing control
    window._showTyping = false;
    window.showTyping = function() { window._showTyping = true; pollOnce(); };
    window.hideTyping = function() { window._showTyping = false; };

    // ============================================
    // CONFIGURATION & POLLING
    // ============================================
    const POLL_MS = Number(qs("poll_ms") || "600");

    let state = {
      orgID: qs("orgID") || qs("org_id"),
      customerPhone: qs("phone") || qs("customer"),
      clinicPhone: qs("clinic"),
      clinicName: qs("clinic_name") || "Wolf Aesthetics",
      lastHash: "",
    };

    // Set clinic name in header
    document.getElementById("contactName").textContent = state.clinicName;

    function buildEndpoint() {
      if (!state.orgID || !state.customerPhone) return "";
      return "/admin/clinics/" + encodeURIComponent(state.orgID) + "/sms/" + encodeURIComponent(state.customerPhone) + "?limit=500";
    }

    // Update status bar time
    function updateStatusTime() {
      const now = new Date();
      document.getElementById("statusTime").textContent = now.toLocaleTimeString([], { hour: "numeric", minute: "2-digit" });
    }
    updateStatusTime();
    setInterval(updateStatusTime, 60000);

    async function pollOnce() {
      const endpoint = buildEndpoint();
      if (!endpoint) {
        render([]);
        return;
      }

      try {
        const resp = await fetch(endpoint, { cache: "no-store" });
        if (!resp.ok) return;
        const data = await resp.json();
        const msgs = (data && data.messages) || [];
        const hash = JSON.stringify(msgs.map(m => [m.id, m.role, m.body, m.timestamp, m.kind]));
        if (hash !== state.lastHash) {
          state.lastHash = hash;
          render(msgs);
        }
      } catch(e) {}
    }

    pollOnce();
    setInterval(pollOnce, POLL_MS);

    // Intercept link clicks to open in browser view
    document.addEventListener("click", function(e) {
      const link = e.target.closest("a");
      if (link && link.href && (link.href.includes("square") || link.href.includes("checkout") || link.href.includes("pay"))) {
        e.preventDefault();
        window.openBrowser(link.href);
      }
    });

    // Set contact name based on URL param
    window.setClinicName = function(name) {
      document.getElementById("contactName").textContent = name;
      state.clinicName = name;
    };

    window.setClinicAvatar = function(emoji) {
      document.getElementById("contactAvatar").textContent = emoji;
    };
  </script>
</body>
</html>`

// EnhancedPhoneSimulator renders an iOS-style chat UI for video demos.
// This version has a cleaner design optimized for recordings with:
// - Illustrated hand overlay with tap animations
// - iOS notification banners
// - Full iOS incoming call UI
// - Sound effects (ringing, whoosh, tri-tone, cha-ching)
// - Payment success overlay
// - No debug info visible
// Route: GET /admin/e2e/phone-simulator-demo?orgID=...&phone=...&clinic=...&clinic_name=...
func (h *Handler) EnhancedPhoneSimulator(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(enhancedPhoneSimulatorHTML))
}
