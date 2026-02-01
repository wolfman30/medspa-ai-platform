package demo

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

// MockBookingHandler serves a simulated Moxie-style booking page for testing.
// This mimics the app.joinmoxie.com booking flow for demo purposes.
// URL parameters:
//   - clinic: Clinic name (default: "Forever 22")
//   - phone: Clinic phone (default: "+1 (440) 703-1022")
//   - email: Clinic email (default: "brandi.forever22@gmail.com")
//   - address: Clinic address (default: "6677 Center Road, Valley City, OH 44280")
//   - date: Pre-select date in YYYY-MM-DD format
//   - slots: JSON array of time slots, e.g. [{"time":"9:00am","available":true}]
type MockBookingHandler struct{}

func NewMockBookingHandler() *MockBookingHandler {
	return &MockBookingHandler{}
}

func (h *MockBookingHandler) Routes() chi.Router {
	r := chi.NewRouter()
	// Catch-all route for any clinic slug
	r.Get("/{clinicSlug}", h.HandleBookingPage)
	r.Get("/{clinicSlug}/", h.HandleBookingPage)
	return r
}

func (h *MockBookingHandler) HandleBookingPage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(mockMoxieBookingHTML))
}

const mockMoxieBookingHTML = `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>Book your appointment</title>
  <style>
    * { margin: 0; padding: 0; box-sizing: border-box; }

    :root {
      --primary: #4f46e5;
      --primary-dark: #4338ca;
      --text: #1f2937;
      --text-muted: #6b7280;
      --border: #e5e7eb;
      --bg: #f9fafb;
      --white: #ffffff;
      --success: #10b981;
      --coral: #f97316;
    }

    body {
      font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, 'Helvetica Neue', Arial, sans-serif;
      background: var(--bg);
      color: var(--text);
      line-height: 1.5;
    }

    /* Header */
    .header {
      background: linear-gradient(135deg, #4f46e5 0%, #7c3aed 100%);
      padding: 12px 24px;
      display: flex;
      align-items: center;
      justify-content: space-between;
    }

    .header-left {
      display: flex;
      align-items: center;
      gap: 16px;
    }

    .logo {
      width: 32px;
      height: 32px;
      background: var(--white);
      border-radius: 8px;
      display: flex;
      align-items: center;
      justify-content: center;
      font-weight: bold;
      font-size: 12px;
      color: var(--primary);
    }

    .header-title {
      color: var(--white);
      font-size: 14px;
      font-weight: 500;
    }

    .header-nav {
      color: rgba(255,255,255,0.9);
      font-size: 14px;
    }

    /* Cherry Banner */
    .cherry-banner {
      background: var(--white);
      padding: 10px;
      text-align: center;
      border-bottom: 1px solid var(--border);
      font-size: 14px;
      color: var(--text-muted);
    }

    .cherry-banner svg {
      vertical-align: middle;
      margin-right: 6px;
    }

    /* Main Layout */
    .main {
      max-width: 1200px;
      margin: 0 auto;
      padding: 32px 24px;
      display: grid;
      grid-template-columns: 280px 1fr;
      gap: 32px;
    }

    @media (max-width: 768px) {
      .main { grid-template-columns: 1fr; }
    }

    /* Sidebar */
    .sidebar {
      background: var(--white);
      border-radius: 12px;
      padding: 24px;
      box-shadow: 0 1px 3px rgba(0,0,0,0.1);
      height: fit-content;
    }

    .clinic-logo {
      width: 80px;
      height: 80px;
      margin: 0 auto 16px;
      background: #f3f4f6;
      border-radius: 12px;
      display: flex;
      align-items: center;
      justify-content: center;
      font-size: 28px;
      color: var(--text);
      font-weight: bold;
    }

    .clinic-name {
      text-align: center;
      font-size: 20px;
      font-weight: 600;
      margin-bottom: 24px;
    }

    .clinic-info {
      display: flex;
      flex-direction: column;
      gap: 16px;
    }

    .info-row {
      display: flex;
      align-items: flex-start;
      gap: 12px;
    }

    .info-icon {
      width: 20px;
      height: 20px;
      color: var(--text-muted);
      flex-shrink: 0;
      margin-top: 2px;
    }

    .info-label {
      font-size: 12px;
      color: var(--text-muted);
      margin-bottom: 2px;
    }

    .info-value {
      font-size: 14px;
      color: var(--primary);
    }

    .info-value.address {
      color: var(--text);
    }

    /* Content Area */
    .content {
      background: var(--white);
      border-radius: 12px;
      padding: 32px;
      box-shadow: 0 1px 3px rgba(0,0,0,0.1);
    }

    /* Progress Bar */
    .progress-bar {
      display: flex;
      gap: 8px;
      margin-bottom: 32px;
    }

    .progress-step {
      flex: 1;
      height: 4px;
      background: var(--border);
      border-radius: 2px;
      transition: background 0.3s;
    }

    .progress-step.active {
      background: var(--primary);
    }

    .progress-step.completed {
      background: var(--success);
    }

    /* Step Header */
    .step-header {
      margin-bottom: 24px;
    }

    .step-label {
      font-size: 12px;
      color: var(--text-muted);
      margin-bottom: 4px;
    }

    .step-title {
      font-size: 24px;
      font-weight: 600;
    }

    /* Step Content - hidden by default */
    .step { display: none; }
    .step.active { display: block; }

    /* Services */
    .search-box {
      position: relative;
      margin-bottom: 24px;
    }

    .search-box input {
      width: 100%;
      padding: 12px 16px 12px 44px;
      border: 1px solid var(--border);
      border-radius: 8px;
      font-size: 14px;
    }

    .search-box svg {
      position: absolute;
      left: 16px;
      top: 50%;
      transform: translateY(-50%);
      color: var(--text-muted);
    }

    .service-category {
      border: 1px solid var(--border);
      border-radius: 8px;
      margin-bottom: 12px;
      overflow: hidden;
    }

    .category-header {
      padding: 16px;
      display: flex;
      justify-content: space-between;
      align-items: center;
      cursor: pointer;
      background: var(--white);
      transition: background 0.2s;
    }

    .category-header:hover {
      background: var(--bg);
    }

    .category-title {
      font-weight: 600;
    }

    .category-count {
      font-size: 12px;
      color: var(--text-muted);
    }

    .category-arrow {
      transition: transform 0.2s;
    }

    .service-category.expanded .category-arrow {
      transform: rotate(180deg);
    }

    .category-services {
      display: none;
      border-top: 1px solid var(--border);
    }

    .service-category.expanded .category-services {
      display: block;
    }

    .service-item {
      padding: 16px;
      border-bottom: 1px solid var(--border);
      display: flex;
      justify-content: space-between;
      align-items: center;
      cursor: pointer;
      transition: background 0.2s;
    }

    .service-item:last-child {
      border-bottom: none;
    }

    .service-item:hover {
      background: var(--bg);
    }

    .service-item.selected {
      background: #eef2ff;
    }

    .service-name {
      font-weight: 500;
      margin-bottom: 4px;
    }

    .service-meta {
      font-size: 12px;
      color: var(--text-muted);
      display: flex;
      align-items: center;
      gap: 8px;
    }

    .service-checkbox {
      width: 20px;
      height: 20px;
      border: 2px solid var(--border);
      border-radius: 4px;
      display: flex;
      align-items: center;
      justify-content: center;
      transition: all 0.2s;
    }

    .service-item.selected .service-checkbox {
      background: var(--primary);
      border-color: var(--primary);
    }

    .service-checkbox svg {
      color: var(--white);
      opacity: 0;
      transition: opacity 0.2s;
    }

    .service-item.selected .service-checkbox svg {
      opacity: 1;
    }

    /* Provider Selection */
    .provider-panel {
      position: fixed;
      right: 0;
      top: 0;
      width: 320px;
      height: 100vh;
      background: var(--white);
      box-shadow: -4px 0 12px rgba(0,0,0,0.15);
      padding: 24px;
      display: none;
      flex-direction: column;
      z-index: 100;
    }

    .provider-panel.visible {
      display: flex;
    }

    .provider-panel-header {
      display: flex;
      justify-content: space-between;
      align-items: center;
      margin-bottom: 24px;
    }

    .provider-panel-title {
      font-size: 18px;
      font-weight: 600;
    }

    .provider-panel-close {
      background: none;
      border: none;
      cursor: pointer;
      padding: 4px;
    }

    .provider-service-name {
      font-size: 14px;
      color: var(--text-muted);
      margin-bottom: 4px;
    }

    .provider-service-duration {
      font-size: 12px;
      color: var(--text-muted);
      margin-bottom: 24px;
    }

    .provider-option {
      padding: 12px;
      border: 1px solid var(--border);
      border-radius: 8px;
      margin-bottom: 12px;
      cursor: pointer;
      display: flex;
      align-items: center;
      gap: 12px;
      transition: all 0.2s;
    }

    .provider-option:hover {
      border-color: var(--primary);
    }

    .provider-option.selected {
      border-color: var(--primary);
      background: #eef2ff;
    }

    .provider-radio {
      width: 20px;
      height: 20px;
      border: 2px solid var(--border);
      border-radius: 50%;
      display: flex;
      align-items: center;
      justify-content: center;
    }

    .provider-option.selected .provider-radio {
      border-color: var(--primary);
    }

    .provider-radio-inner {
      width: 10px;
      height: 10px;
      border-radius: 50%;
      background: var(--primary);
      opacity: 0;
      transition: opacity 0.2s;
    }

    .provider-option.selected .provider-radio-inner {
      opacity: 1;
    }

    .provider-panel-footer {
      margin-top: auto;
    }

    .confirm-btn {
      width: 100%;
      padding: 14px;
      background: var(--coral);
      color: var(--white);
      border: none;
      border-radius: 8px;
      font-size: 14px;
      font-weight: 600;
      cursor: pointer;
      transition: background 0.2s;
    }

    .confirm-btn:hover {
      background: #ea580c;
    }

    /* Calendar */
    .calendar-container {
      display: grid;
      grid-template-columns: 1fr 200px;
      gap: 32px;
    }

    @media (max-width: 640px) {
      .calendar-container { grid-template-columns: 1fr; }
    }

    .calendar {
      border: 1px solid var(--border);
      border-radius: 8px;
      padding: 16px;
    }

    .calendar-header {
      display: flex;
      justify-content: space-between;
      align-items: center;
      margin-bottom: 16px;
    }

    .calendar-month {
      font-weight: 600;
      display: flex;
      align-items: center;
      gap: 4px;
    }

    .calendar-nav {
      display: flex;
      gap: 8px;
    }

    .calendar-nav button {
      background: none;
      border: none;
      cursor: pointer;
      padding: 4px;
      color: var(--text-muted);
    }

    .calendar-weekdays {
      display: grid;
      grid-template-columns: repeat(7, 1fr);
      text-align: center;
      font-size: 12px;
      color: var(--text-muted);
      margin-bottom: 8px;
    }

    .calendar-days {
      display: grid;
      grid-template-columns: repeat(7, 1fr);
      gap: 4px;
    }

    .calendar-day {
      aspect-ratio: 1;
      display: flex;
      align-items: center;
      justify-content: center;
      border-radius: 50%;
      font-size: 14px;
      cursor: pointer;
      transition: all 0.2s;
    }

    .calendar-day:hover:not(.disabled):not(.selected) {
      background: var(--bg);
    }

    .calendar-day.disabled {
      color: #d1d5db;
      cursor: not-allowed;
    }

    .calendar-day.selected {
      background: var(--primary);
      color: var(--white);
    }

    .calendar-day.today {
      font-weight: 600;
    }

    /* Time Slots */
    .time-slots {
      padding-top: 8px;
    }

    .time-section-title {
      font-size: 14px;
      color: var(--text-muted);
      margin-bottom: 12px;
    }

    .time-grid {
      display: flex;
      flex-wrap: wrap;
      gap: 8px;
    }

    .time-slot {
      padding: 10px 16px;
      border: 1px solid var(--border);
      border-radius: 6px;
      font-size: 14px;
      cursor: pointer;
      transition: all 0.2s;
      background: var(--white);
    }

    .time-slot:hover:not(.unavailable):not(.selected) {
      border-color: var(--primary);
    }

    .time-slot.selected {
      background: var(--primary);
      color: var(--white);
      border-color: var(--primary);
    }

    .time-slot.unavailable {
      background: #f3f4f6;
      color: #9ca3af;
      cursor: not-allowed;
      text-decoration: line-through;
    }

    .timezone-note {
      font-size: 12px;
      color: var(--text-muted);
      margin-top: 16px;
    }

    /* Appointment Summary */
    .appointment-summary {
      background: var(--bg);
      border-radius: 8px;
      padding: 16px;
      margin-top: 24px;
    }

    .summary-title {
      font-size: 14px;
      font-weight: 600;
      margin-bottom: 12px;
      display: flex;
      justify-content: space-between;
    }

    .summary-item {
      display: flex;
      align-items: center;
      gap: 8px;
      padding: 8px 0;
      border-bottom: 1px solid var(--border);
    }

    .summary-item:last-child {
      border-bottom: none;
    }

    .summary-number {
      width: 24px;
      height: 24px;
      background: var(--primary);
      color: var(--white);
      border-radius: 50%;
      display: flex;
      align-items: center;
      justify-content: center;
      font-size: 12px;
    }

    .summary-service {
      flex: 1;
    }

    .summary-service-name {
      font-weight: 500;
      font-size: 14px;
    }

    .summary-service-time {
      font-size: 12px;
      color: var(--text-muted);
    }

    /* Form Inputs */
    .form-group {
      margin-bottom: 20px;
    }

    .form-label {
      display: block;
      font-size: 12px;
      color: var(--text-muted);
      margin-bottom: 6px;
    }

    .form-input {
      width: 100%;
      padding: 12px 16px;
      border: 1px solid var(--border);
      border-radius: 8px;
      font-size: 14px;
      transition: border-color 0.2s;
    }

    .form-input:focus {
      outline: none;
      border-color: var(--primary);
    }

    .phone-input-container {
      display: flex;
      align-items: center;
      border: 1px solid var(--border);
      border-radius: 8px;
      overflow: hidden;
    }

    .phone-flag {
      padding: 12px 16px;
      background: var(--bg);
      border-right: 1px solid var(--border);
      font-size: 20px;
    }

    .phone-input-container input {
      flex: 1;
      border: none;
      padding: 12px 16px;
      font-size: 14px;
    }

    .phone-input-container input:focus {
      outline: none;
    }

    textarea.form-input {
      min-height: 100px;
      resize: vertical;
    }

    /* Buttons */
    .btn {
      padding: 14px 24px;
      border-radius: 8px;
      font-size: 14px;
      font-weight: 600;
      cursor: pointer;
      transition: all 0.2s;
      border: none;
    }

    .btn-primary {
      background: var(--coral);
      color: var(--white);
    }

    .btn-primary:hover {
      background: #ea580c;
    }

    .btn-secondary {
      background: var(--white);
      color: var(--text);
      border: 1px solid var(--border);
    }

    .btn-secondary:hover {
      background: var(--bg);
    }

    .step-footer {
      display: flex;
      justify-content: space-between;
      align-items: center;
      margin-top: 32px;
      padding-top: 24px;
      border-top: 1px solid var(--border);
    }

    .moxie-powered {
      font-size: 12px;
      color: var(--text-muted);
    }

    /* Two Column Form */
    .form-row {
      display: grid;
      grid-template-columns: 1fr 1fr;
      gap: 16px;
    }

    @media (max-width: 480px) {
      .form-row { grid-template-columns: 1fr; }
    }

    /* Success Screen */
    .success-screen {
      text-align: center;
      padding: 48px 24px;
    }

    .success-icon {
      width: 80px;
      height: 80px;
      background: #dcfce7;
      border-radius: 50%;
      display: flex;
      align-items: center;
      justify-content: center;
      margin: 0 auto 24px;
    }

    .success-icon svg {
      color: var(--success);
      width: 40px;
      height: 40px;
    }

    .success-title {
      font-size: 24px;
      font-weight: 600;
      margin-bottom: 8px;
    }

    .success-message {
      color: var(--text-muted);
      margin-bottom: 24px;
    }

    /* Back link */
    .back-link {
      display: inline-flex;
      align-items: center;
      gap: 8px;
      color: var(--text-muted);
      font-size: 14px;
      margin-bottom: 16px;
      cursor: pointer;
    }

    .back-link:hover {
      color: var(--primary);
    }

    /* Hidden utility */
    .hidden { display: none !important; }
  </style>
</head>
<body>
  <!-- Header -->
  <header class="header">
    <div class="header-left">
      <div class="logo" id="headerLogo">22</div>
      <span class="header-title" id="headerClinicName">Forever 22</span>
      <span class="header-nav">|</span>
      <span class="header-nav">Book your appointment</span>
    </div>
  </header>

  <!-- Cherry Banner -->
  <div class="cherry-banner">
    <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="#10b981" stroke-width="2">
      <path d="M22 11.08V12a10 10 0 1 1-5.93-9.14"/>
      <polyline points="22 4 12 14.01 9 11.01"/>
    </svg>
    We offer Cherry payment plans.
  </div>

  <!-- Main Content -->
  <main class="main">
    <!-- Sidebar -->
    <aside class="sidebar">
      <div class="clinic-logo" id="sidebarLogo">22</div>
      <h2 class="clinic-name" id="clinicNameDisplay">Forever 22</h2>

      <div class="clinic-info">
        <div class="info-row">
          <svg class="info-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
            <path d="M22 16.92v3a2 2 0 0 1-2.18 2 19.79 19.79 0 0 1-8.63-3.07 19.5 19.5 0 0 1-6-6 19.79 19.79 0 0 1-3.07-8.67A2 2 0 0 1 4.11 2h3a2 2 0 0 1 2 1.72 12.84 12.84 0 0 0 .7 2.81 2 2 0 0 1-.45 2.11L8.09 9.91a16 16 0 0 0 6 6l1.27-1.27a2 2 0 0 1 2.11-.45 12.84 12.84 0 0 0 2.81.7A2 2 0 0 1 22 16.92z"/>
          </svg>
          <div>
            <div class="info-label">Phone number</div>
            <a href="#" class="info-value" id="clinicPhoneDisplay">+1 (440) 703-1022</a>
          </div>
        </div>

        <div class="info-row">
          <svg class="info-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
            <path d="M4 4h16c1.1 0 2 .9 2 2v12c0 1.1-.9 2-2 2H4c-1.1 0-2-.9-2-2V6c0-1.1.9-2 2-2z"/>
            <polyline points="22,6 12,13 2,6"/>
          </svg>
          <div>
            <div class="info-label">Email address</div>
            <a href="#" class="info-value" id="clinicEmailDisplay">contact@clinic.com</a>
          </div>
        </div>

        <div class="info-row">
          <svg class="info-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
            <path d="M21 10c0 7-9 13-9 13s-9-6-9-13a9 9 0 0 1 18 0z"/>
            <circle cx="12" cy="10" r="3"/>
          </svg>
          <div>
            <div class="info-label">Address</div>
            <div class="info-value address" id="clinicAddressDisplay">123 Main St, City, ST 12345</div>
          </div>
        </div>
      </div>

      <!-- Appointment Summary (shown after service selected) -->
      <div class="appointment-summary hidden" id="appointmentSummary">
        <div class="summary-title">
          <span>Appointment summary</span>
          <span id="totalDuration">45 min</span>
        </div>
        <div id="summaryItems"></div>
        <a href="#" class="info-value" style="font-size: 12px; display: block; margin-top: 12px;" onclick="addAnotherService()">+ Add another service</a>
      </div>
    </aside>

    <!-- Content -->
    <div class="content">
      <!-- Progress Bar -->
      <div class="progress-bar">
        <div class="progress-step active" data-step="1"></div>
        <div class="progress-step" data-step="2"></div>
        <div class="progress-step" data-step="3"></div>
        <div class="progress-step" data-step="4"></div>
        <div class="progress-step" data-step="5"></div>
        <div class="progress-step" data-step="6"></div>
      </div>

      <!-- Step 1: Select Services -->
      <div class="step active" id="step1">
        <div class="step-header">
          <div class="step-label">Step 1</div>
          <h1 class="step-title">Select service(s)</h1>
        </div>

        <div class="search-box">
          <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
            <circle cx="11" cy="11" r="8"/>
            <path d="m21 21-4.35-4.35"/>
          </svg>
          <input type="text" placeholder="Search services..." id="serviceSearch">
        </div>

        <div id="serviceCategories">
          <!-- Categories will be rendered by JS -->
        </div>
      </div>

      <!-- Step 2: Select Date/Time -->
      <div class="step" id="step2">
        <div class="step-header">
          <span class="back-link" onclick="goToStep(1)">
            <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
              <path d="m15 18-6-6 6-6"/>
            </svg>
            Back
          </span>
          <div class="step-label">Step 2</div>
          <h1 class="step-title">Select date & time</h1>
        </div>

        <div class="calendar-container">
          <div class="calendar">
            <div class="calendar-header">
              <span class="calendar-month" id="calendarMonth">February 2026</span>
              <div class="calendar-nav">
                <button onclick="prevMonth()">&lt;</button>
                <button onclick="nextMonth()">&gt;</button>
              </div>
            </div>
            <div class="calendar-weekdays">
              <div>S</div><div>M</div><div>T</div><div>W</div><div>T</div><div>F</div><div>S</div>
            </div>
            <div class="calendar-days" id="calendarDays"></div>
          </div>

          <div class="time-slots">
            <div class="time-section-title">Evening</div>
            <div class="time-grid" id="timeSlots">
              <!-- Time slots rendered by JS -->
            </div>
            <div class="timezone-note" id="timezoneNote">
              <span id="clinicNameTz">Clinic</span>'s timezone is <strong>America/New_York</strong>. All times shown will use this timezone.
            </div>
          </div>
        </div>

        <div class="step-footer">
          <div class="moxie-powered">Powered by Moxie</div>
          <button class="btn btn-primary" onclick="goToStep(3)" id="step2Next" disabled>Next step</button>
        </div>
      </div>

      <!-- Step 3: Enter Phone -->
      <div class="step" id="step3">
        <div class="step-header">
          <span class="back-link" onclick="goToStep(2)">
            <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
              <path d="m15 18-6-6 6-6"/>
            </svg>
            Back
          </span>
          <div class="step-label">Step 3</div>
          <h1 class="step-title">Enter your phone</h1>
        </div>

        <p style="color: var(--text-muted); margin-bottom: 24px;">Enter your phone number, so we can guide you through the booking process!</p>

        <div class="form-group">
          <label class="form-label">Phone</label>
          <div class="phone-input-container">
            <span class="phone-flag">🇺🇸</span>
            <input type="tel" id="phoneInput" placeholder="+1 555 123 4567" value="+1 ">
          </div>
        </div>

        <div class="step-footer">
          <div class="moxie-powered">Powered by Moxie</div>
          <button class="btn btn-primary" onclick="goToStep(4)">Next</button>
        </div>
      </div>

      <!-- Step 4: Complete Booking -->
      <div class="step" id="step4">
        <div class="step-header">
          <span class="back-link" onclick="goToStep(3)">
            <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
              <path d="m15 18-6-6 6-6"/>
            </svg>
            Back
          </span>
          <div class="step-label">Step 4</div>
          <h1 class="step-title">Complete booking</h1>
        </div>

        <p style="color: var(--text-muted); margin-bottom: 24px;">Enter your details below</p>

        <div class="form-row">
          <div class="form-group">
            <label class="form-label">First name</label>
            <input type="text" class="form-input" id="firstName" placeholder="First name">
          </div>
          <div class="form-group">
            <label class="form-label">Last name</label>
            <input type="text" class="form-input" id="lastName" placeholder="Last name">
          </div>
        </div>

        <div class="form-group">
          <label class="form-label">Email</label>
          <input type="email" class="form-input" id="email" placeholder="your@email.com">
        </div>

        <div class="form-group">
          <label class="form-label">Phone</label>
          <div class="phone-input-container">
            <span class="phone-flag">🇺🇸</span>
            <input type="tel" id="phoneConfirm" placeholder="+1 555 123 4567">
          </div>
        </div>

        <div class="form-group">
          <label class="form-label">Notes (optional)</label>
          <textarea class="form-input" id="notes" placeholder="Any special requests or notes..."></textarea>
        </div>

        <div class="step-footer">
          <div class="moxie-powered">Powered by Moxie</div>
          <button class="btn btn-primary" onclick="goToStep(5)">Next</button>
        </div>
      </div>

      <!-- Step 5: Payment -->
      <div class="step" id="step5">
        <div class="step-header">
          <span class="back-link" onclick="goToStep(4)">
            <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
              <path d="m15 18-6-6 6-6"/>
            </svg>
            Back
          </span>
          <div class="step-label">Step 5</div>
          <h1 class="step-title">Payment</h1>
        </div>

        <p style="color: var(--text-muted); margin-bottom: 24px;">Enter your card details to secure your appointment</p>

        <div class="payment-summary" style="background: var(--bg); border-radius: 8px; padding: 16px; margin-bottom: 24px;">
          <div style="display: flex; justify-content: space-between; margin-bottom: 8px;">
            <span>Deposit for appointment</span>
            <span style="font-weight: 600;">$50.00</span>
          </div>
          <div style="font-size: 12px; color: var(--text-muted);">
            This deposit will be applied to your service total
          </div>
        </div>

        <div class="form-group">
          <label class="form-label">Card number</label>
          <input type="text" class="form-input" id="cardNumber" placeholder="4111 1111 1111 1111" maxlength="19">
          <div style="font-size: 11px; color: var(--text-muted); margin-top: 4px;">
            Test cards: 4111... = success, 4000000000000002 = declined
          </div>
        </div>

        <div class="form-row">
          <div class="form-group">
            <label class="form-label">Expiry date</label>
            <input type="text" class="form-input" id="cardExpiry" placeholder="MM/YY" maxlength="5">
          </div>
          <div class="form-group">
            <label class="form-label">CVV</label>
            <input type="text" class="form-input" id="cardCvv" placeholder="123" maxlength="4">
          </div>
        </div>

        <div class="form-group">
          <label class="form-label">Name on card</label>
          <input type="text" class="form-input" id="cardName" placeholder="John Smith">
        </div>

        <div id="paymentError" style="display: none; background: #fef2f2; border: 1px solid #fecaca; color: #dc2626; padding: 12px; border-radius: 8px; margin-bottom: 16px;">
          <strong>Payment Failed:</strong> <span id="paymentErrorMsg">Your card was declined</span>
        </div>

        <div class="step-footer">
          <div class="moxie-powered">Powered by Moxie</div>
          <button class="btn btn-primary" id="bookAppointmentBtn" onclick="processPayment()">Book Appointment</button>
        </div>
      </div>

      <!-- Step 6: Success -->
      <div class="step" id="step6">
        <div class="success-screen">
          <div class="success-icon">
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
              <polyline points="20 6 9 17 4 12"/>
            </svg>
          </div>
          <h1 class="success-title">Booking Confirmed!</h1>
          <p class="success-message">Your appointment has been scheduled. You'll receive a confirmation via SMS and email.</p>

          <div class="confirmation-number" style="background: #ecfdf5; border: 1px solid #a7f3d0; color: #065f46; padding: 12px; border-radius: 8px; margin-bottom: 16px; text-align: center;">
            <div style="font-size: 12px; text-transform: uppercase; letter-spacing: 0.5px;">Confirmation Number</div>
            <div id="confirmationNumber" style="font-size: 24px; font-weight: 700; font-family: monospace;">MXE-2026-XXXXX</div>
          </div>

          <div style="background: var(--bg); border-radius: 8px; padding: 20px; text-align: left; max-width: 400px; margin: 0 auto;">
            <div style="margin-bottom: 12px;">
              <strong>Service:</strong> <span id="confirmService">1 Syringe of Lip Filler</span>
            </div>
            <div style="margin-bottom: 12px;">
              <strong>Provider:</strong> <span id="confirmProvider">Gale Tesar</span>
            </div>
            <div style="margin-bottom: 12px;">
              <strong>Date:</strong> <span id="confirmDate">Thursday, February 26th, 2026</span>
            </div>
            <div>
              <strong>Time:</strong> <span id="confirmTime">7:45 PM - 8:30 PM (America/New_York)</span>
            </div>
          </div>

          <div style="margin-top: 24px;">
            <button class="btn btn-secondary" onclick="location.reload()">Book another appointment</button>
          </div>
        </div>
      </div>
    </div>
  </main>

  <!-- Provider Panel (Slide-in) -->
  <div class="provider-panel" id="providerPanel">
    <div class="provider-panel-header">
      <h3 class="provider-panel-title">Choose a provider</h3>
      <button class="provider-panel-close" onclick="closeProviderPanel()">
        <svg width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
          <line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/>
        </svg>
      </button>
    </div>
    <div class="provider-service-name" id="providerServiceName">1 Syringe of Lip Filler</div>
    <div class="provider-service-duration" id="providerServiceDuration">⏱ 45 min</div>

    <div id="providerOptions">
      <!-- Providers rendered by JS -->
    </div>

    <div class="provider-panel-footer">
      <button class="confirm-btn" onclick="confirmProvider()">Confirm selection</button>
    </div>
  </div>

  <script>
    // Default clinic config (can be overridden via URL params)
    let clinicConfig = {
      name: 'Forever 22',
      phone: '+1 (440) 703-1022',
      email: 'brandi.forever22@gmail.com',
      address: '6677 Center Road, Valley City, OH 44280',
      timezone: 'America/New_York',
      providers: [
        { id: 'provider1', name: 'Gale Tesar' },
        { id: 'provider2', name: 'Brandi Sesock' }
      ]
    };

    // Default services (typical MedSpa offerings)
    let services = {
      'Consultation': [
        { name: 'Aesthetic Consultation', duration: 30, providers: ['provider1', 'provider2'] },
        { name: 'Weight Loss Consultation - In Person', duration: 30, providers: ['provider1', 'provider2'] },
        { name: 'Weight Loss Consultation - Virtual Visit', duration: 30, providers: ['provider1', 'provider2'] }
      ],
      'Wrinkle Relaxers': [
        { name: 'Botox/Dysport/Xeomin', duration: 30, providers: ['provider1', 'provider2'] }
      ],
      'Dermal Filler': [
        { name: 'Dermal Fillers (Cheeks, Lips, Chin, Jawline, Smile Lines, Marrionettes, Facial Balancing)', duration: 60, providers: ['provider2'] },
        { name: '1 Syringe of Lip Filler', duration: 45, providers: ['provider1', 'provider2'] },
        { name: 'Mini Lip Filler', duration: 45, providers: ['provider1', 'provider2'] },
        { name: 'Radiesse', duration: 60, providers: ['provider1'] },
        { name: 'Skinvive', duration: 45, providers: ['provider1', 'provider2'] },
        { name: 'Filler Dissolve / Hylenex', duration: 30, providers: ['provider1', 'provider2'] }
      ],
      'Weight Loss': [
        { name: 'Semaglutide Injection', duration: 15, providers: ['provider1', 'provider2'] },
        { name: 'Tirzepatide Injection', duration: 15, providers: ['provider1', 'provider2'] }
      ],
      'Laser Treatments': [
        { name: 'IPL - Full Face', duration: 45, providers: ['provider1'] },
        { name: 'Laser Hair Removal - Small Area', duration: 15, providers: ['provider1'] },
        { name: 'Laser Hair Removal - Medium Area', duration: 30, providers: ['provider1'] },
        { name: 'Laser Hair Removal - Large Area', duration: 45, providers: ['provider1'] }
      ],
      'Skin Rejuvenation': [
        { name: 'Microneedling - Face', duration: 60, providers: ['provider1'] },
        { name: 'Microneedling with PRP', duration: 75, providers: ['provider1'] },
        { name: 'Chemical Peel', duration: 45, providers: ['provider1', 'provider2'] }
      ],
      'Body Contouring': [
        { name: 'Kybella Treatment', duration: 45, providers: ['provider2'] }
      ],
      'Wellness': [
        { name: 'B12 Injection', duration: 10, providers: ['provider1', 'provider2'] },
        { name: 'Lipo-B Injection', duration: 10, providers: ['provider1', 'provider2'] },
        { name: 'IV Therapy', duration: 60, providers: ['provider1', 'provider2'] }
      ]
    };

    // Default time slots (these are what the browser scraper will find)
    let availableTimeSlots = [
      { time: '9:00am', available: true },
      { time: '9:30am', available: true },
      { time: '10:00am', available: false },
      { time: '10:30am', available: true },
      { time: '11:00am', available: true },
      { time: '11:30am', available: true },
      { time: '1:00pm', available: true },
      { time: '1:30pm', available: false },
      { time: '2:00pm', available: true },
      { time: '2:30pm', available: true },
      { time: '3:00pm', available: true },
      { time: '3:30pm', available: false },
      { time: '4:00pm', available: true },
      { time: '4:30pm', available: true },
      { time: '5:00pm', available: true },
      { time: '6:00pm', available: true },
      { time: '6:30pm', available: true },
      { time: '7:00pm', available: true },
      { time: '7:30pm', available: false }
    ];

    // State
    let currentStep = 1;
    let selectedServices = [];
    let selectedProvider = null;
    let selectedDate = null;
    let selectedTime = null;
    let currentMonth = new Date();
    let pendingService = null;

    // Initialize from URL parameters
    function initFromParams() {
      const params = new URLSearchParams(window.location.search);

      // Clinic info
      if (params.get('clinic')) clinicConfig.name = params.get('clinic');
      if (params.get('phone')) clinicConfig.phone = params.get('phone');
      if (params.get('email')) clinicConfig.email = params.get('email');
      if (params.get('address')) clinicConfig.address = params.get('address');
      if (params.get('timezone')) clinicConfig.timezone = params.get('timezone');

      // Providers (JSON array)
      if (params.get('providers')) {
        try {
          clinicConfig.providers = JSON.parse(decodeURIComponent(params.get('providers')));
        } catch (e) { console.warn('Invalid providers param'); }
      }

      // Services (JSON object)
      if (params.get('services')) {
        try {
          services = JSON.parse(decodeURIComponent(params.get('services')));
        } catch (e) { console.warn('Invalid services param'); }
      }

      // Time slots (JSON array)
      if (params.get('slots')) {
        try {
          availableTimeSlots = JSON.parse(decodeURIComponent(params.get('slots')));
        } catch (e) { console.warn('Invalid slots param'); }
      }

      // Pre-select date
      if (params.get('date')) {
        try {
          const d = new Date(params.get('date'));
          if (!isNaN(d.getTime())) {
            currentMonth = new Date(d.getFullYear(), d.getMonth(), 1);
            selectedDate = d;
          }
        } catch (e) {}
      }

      // Update display
      updateClinicDisplay();
    }

    function updateClinicDisplay() {
      // Generate logo from clinic name
      const words = clinicConfig.name.split(' ');
      let logo = words.length > 1 ? words[0][0] + words[1][0] : clinicConfig.name.substring(0, 2);
      logo = logo.toUpperCase();

      document.getElementById('headerLogo').textContent = logo;
      document.getElementById('sidebarLogo').textContent = logo;
      document.getElementById('headerClinicName').textContent = clinicConfig.name;
      document.getElementById('clinicNameDisplay').textContent = clinicConfig.name;
      document.getElementById('clinicPhoneDisplay').textContent = clinicConfig.phone;
      document.getElementById('clinicPhoneDisplay').href = 'tel:' + clinicConfig.phone.replace(/[^+\d]/g, '');
      document.getElementById('clinicEmailDisplay').textContent = clinicConfig.email;
      document.getElementById('clinicEmailDisplay').href = 'mailto:' + clinicConfig.email;
      document.getElementById('clinicAddressDisplay').textContent = clinicConfig.address;
      document.getElementById('clinicNameTz').textContent = clinicConfig.name;
      document.title = clinicConfig.name + ' | Book your appointment';
    }

    function getProviderName(id) {
      const p = clinicConfig.providers.find(p => p.id === id);
      return p ? p.name : id;
    }

    // Render service categories
    function renderServices() {
      const container = document.getElementById('serviceCategories');
      container.innerHTML = '';

      for (const [category, items] of Object.entries(services)) {
        const categoryDiv = document.createElement('div');
        categoryDiv.className = 'service-category';
        categoryDiv.innerHTML = ` + "`" + `
          <div class="category-header" onclick="toggleCategory(this.parentElement)">
            <div>
              <div class="category-title">${category}</div>
              <div class="category-count">${items.length} available service${items.length > 1 ? 's' : ''}</div>
            </div>
            <svg class="category-arrow" width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
              <path d="m6 9 6 6 6-6"/>
            </svg>
          </div>
          <div class="category-services">
            ${items.map(s => {
              const providerNames = s.providers.map(id => getProviderName(id));
              return ` + "`" + `
              <div class="service-item" onclick="selectService('${s.name.replace(/'/g, "\\'")}', ${s.duration}, '${s.providers.join(',')}')">
                <div>
                  <div class="service-name">${s.name}</div>
                  <div class="service-meta">
                    <span>⏱ ${s.duration} min</span>
                    ${s.providers.length > 1 ? ` + "`" + `<span>👤 ${s.providers.length} providers</span>` + "`" + ` : ` + "`" + `<span>👤 ${providerNames[0]}</span>` + "`" + `}
                  </div>
                </div>
                <div class="service-checkbox">
                  <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="3">
                    <polyline points="20 6 9 17 4 12"/>
                  </svg>
                </div>
              </div>
            ` + "`" + `}).join('')}
          </div>
        ` + "`" + `;
        container.appendChild(categoryDiv);
      }
    }

    function toggleCategory(el) {
      el.classList.toggle('expanded');
    }

    function selectService(name, duration, providersStr) {
      const providers = providersStr.split(',');
      pendingService = { name, duration, providers };

      // Show provider panel
      document.getElementById('providerServiceName').textContent = name;
      document.getElementById('providerServiceDuration').textContent = '⏱ ' + duration + ' min';

      // Render provider options
      const optionsContainer = document.getElementById('providerOptions');
      optionsContainer.innerHTML = ` + "`" + `
        <div class="provider-option ${!selectedProvider ? 'selected' : ''}" onclick="selectProvider(null, 'No preference')">
          <div class="provider-radio"><div class="provider-radio-inner"></div></div>
          <span>No preference (first available)</span>
        </div>
      ` + "`" + ` + providers.map(id => {
        const name = getProviderName(id);
        const isSelected = selectedProvider && selectedProvider.id === id;
        return ` + "`" + `
          <div class="provider-option ${isSelected ? 'selected' : ''}" onclick="selectProvider('${id}', '${name}')">
            <div class="provider-radio"><div class="provider-radio-inner"></div></div>
            <span>${name}</span>
          </div>
        ` + "`" + `;
      }).join('');

      document.getElementById('providerPanel').classList.add('visible');

      // Pre-select first provider if only one
      if (providers.length === 1) {
        selectedProvider = { id: providers[0], name: getProviderName(providers[0]) };
      }
    }

    function selectProvider(id, name) {
      selectedProvider = id ? { id, name } : null;
      document.querySelectorAll('.provider-option').forEach(el => el.classList.remove('selected'));
      event.currentTarget.classList.add('selected');
    }

    function closeProviderPanel() {
      document.getElementById('providerPanel').classList.remove('visible');
    }

    function confirmProvider() {
      if (!pendingService) return;

      selectedServices = [{ ...pendingService, provider: selectedProvider }];
      closeProviderPanel();
      updateSummary();
      goToStep(2);
    }

    function updateSummary() {
      const summary = document.getElementById('appointmentSummary');
      const items = document.getElementById('summaryItems');
      const duration = document.getElementById('totalDuration');

      if (selectedServices.length === 0) {
        summary.classList.add('hidden');
        return;
      }

      summary.classList.remove('hidden');

      let totalMin = 0;
      items.innerHTML = selectedServices.map((s, i) => {
        totalMin += s.duration;
        return ` + "`" + `
          <div class="summary-item">
            <div class="summary-number">${i + 1}</div>
            <div class="summary-service">
              <div class="summary-service-name">${s.name}</div>
              <div class="summary-service-time">${s.provider ? '👤 ' + s.provider.name : '👤 First available'}</div>
            </div>
            <button onclick="removeService(${i})" style="background:none;border:none;cursor:pointer;color:#9ca3af;">🗑</button>
          </div>
        ` + "`" + `;
      }).join('');

      duration.textContent = totalMin + ' min';
    }

    function removeService(index) {
      selectedServices.splice(index, 1);
      updateSummary();
      if (selectedServices.length === 0) {
        goToStep(1);
      }
    }

    function addAnotherService() {
      goToStep(1);
    }

    // Calendar
    function renderCalendar() {
      const monthEl = document.getElementById('calendarMonth');
      const daysEl = document.getElementById('calendarDays');

      const months = ['January', 'February', 'March', 'April', 'May', 'June', 'July', 'August', 'September', 'October', 'November', 'December'];
      monthEl.textContent = months[currentMonth.getMonth()] + ' ' + currentMonth.getFullYear();

      const firstDay = new Date(currentMonth.getFullYear(), currentMonth.getMonth(), 1).getDay();
      const daysInMonth = new Date(currentMonth.getFullYear(), currentMonth.getMonth() + 1, 0).getDate();
      const today = new Date();
      today.setHours(0,0,0,0);

      let html = '';

      // Empty cells for days before the first
      for (let i = 0; i < firstDay; i++) {
        html += '<div class="calendar-day disabled"></div>';
      }

      // Days
      for (let d = 1; d <= daysInMonth; d++) {
        const date = new Date(currentMonth.getFullYear(), currentMonth.getMonth(), d);
        const isPast = date < today;
        const isToday = date.getTime() === today.getTime();
        const isSelected = selectedDate && date.toDateString() === selectedDate.toDateString();

        let classes = 'calendar-day';
        if (isPast) classes += ' disabled';
        if (isToday) classes += ' today';
        if (isSelected) classes += ' selected';

        html += ` + "`" + `<div class="${classes}" onclick="selectDate(${currentMonth.getFullYear()}, ${currentMonth.getMonth()}, ${d})">${d}</div>` + "`" + `;
      }

      daysEl.innerHTML = html;
    }

    function prevMonth() {
      currentMonth = new Date(currentMonth.getFullYear(), currentMonth.getMonth() - 1, 1);
      renderCalendar();
    }

    function nextMonth() {
      currentMonth = new Date(currentMonth.getFullYear(), currentMonth.getMonth() + 1, 1);
      renderCalendar();
    }

    function selectDate(year, month, day) {
      const date = new Date(year, month, day);
      const today = new Date();
      today.setHours(0,0,0,0);
      if (date < today) return;

      selectedDate = date;
      selectedTime = null;
      document.getElementById('step2Next').disabled = true;
      renderCalendar();
      renderTimeSlots();
    }

    function renderTimeSlots() {
      const container = document.getElementById('timeSlots');

      container.innerHTML = availableTimeSlots.map(slot => ` + "`" + `
        <button class="time-slot ${slot.available ? 'available' : 'unavailable'} ${selectedTime === slot.time ? 'selected' : ''}"
                onclick="selectTime('${slot.time}')"
                ${!slot.available ? 'disabled' : ''}>
          ${slot.time}
        </button>
      ` + "`" + `).join('');
    }

    function selectTime(time) {
      selectedTime = time;
      renderTimeSlots();
      document.getElementById('step2Next').disabled = false;
    }

    // Step navigation
    function goToStep(step) {
      currentStep = step;

      // Update progress bar
      document.querySelectorAll('.progress-step').forEach((el, i) => {
        el.classList.remove('active', 'completed');
        if (i + 1 < step) el.classList.add('completed');
        if (i + 1 === step) el.classList.add('active');
      });

      // Show correct step
      document.querySelectorAll('.step').forEach(el => el.classList.remove('active'));
      document.getElementById('step' + step).classList.add('active');

      // Initialize step-specific content
      if (step === 2) {
        renderCalendar();
        renderTimeSlots();
      }

      if (step === 4) {
        const phone = document.getElementById('phoneInput').value;
        document.getElementById('phoneConfirm').value = phone;
      }
    }

    function completeBooking() {
      // Update confirmation
      if (selectedServices.length > 0) {
        document.getElementById('confirmService').textContent = selectedServices[0].name;
        document.getElementById('confirmProvider').textContent = selectedServices[0].provider?.name || 'First available';
      }

      if (selectedDate) {
        const options = { weekday: 'long', year: 'numeric', month: 'long', day: 'numeric' };
        document.getElementById('confirmDate').textContent = selectedDate.toLocaleDateString('en-US', options);
      }

      if (selectedTime) {
        const duration = selectedServices[0]?.duration || 45;
        // Parse the time and add duration
        const timeMatch = selectedTime.match(/(\d+):(\d+)(am|pm)/i);
        if (timeMatch) {
          let hour = parseInt(timeMatch[1]);
          let min = parseInt(timeMatch[2]);
          const isPM = timeMatch[3].toLowerCase() === 'pm';
          if (isPM && hour !== 12) hour += 12;
          if (!isPM && hour === 12) hour = 0;

          const startDate = new Date();
          startDate.setHours(hour, min, 0, 0);
          const endDate = new Date(startDate.getTime() + duration * 60000);

          const formatTime = (d) => {
            let h = d.getHours();
            let m = d.getMinutes();
            const ampm = h >= 12 ? 'PM' : 'AM';
            h = h % 12 || 12;
            return h + ':' + (m < 10 ? '0' : '') + m + ' ' + ampm;
          };

          document.getElementById('confirmTime').textContent = formatTime(startDate) + ' - ' + formatTime(endDate) + ' (' + clinicConfig.timezone + ')';
        }
      }

      // Generate confirmation number
      const confirmNum = 'MXE-' + new Date().getFullYear() + '-' + Math.random().toString(36).substring(2, 7).toUpperCase();
      document.getElementById('confirmationNumber').textContent = confirmNum;

      goToStep(6);
    }

    // Process payment (simulated)
    function processPayment() {
      const cardNumber = document.getElementById('cardNumber').value.replace(/\s/g, '');
      const errorDiv = document.getElementById('paymentError');
      const errorMsg = document.getElementById('paymentErrorMsg');
      const bookBtn = document.getElementById('bookAppointmentBtn');

      // Hide any previous error
      errorDiv.style.display = 'none';

      // Disable button and show loading
      bookBtn.disabled = true;
      bookBtn.textContent = 'Processing...';

      // Simulate payment processing delay
      setTimeout(() => {
        // Test card numbers for different outcomes
        if (cardNumber === '4000000000000002') {
          // Declined card
          errorMsg.textContent = 'Your card was declined. Please try a different card.';
          errorDiv.style.display = 'block';
          bookBtn.disabled = false;
          bookBtn.textContent = 'Book Appointment';
          return;
        }

        if (cardNumber === '4000000000000069') {
          // Expired card
          errorMsg.textContent = 'Your card has expired. Please use a valid card.';
          errorDiv.style.display = 'block';
          bookBtn.disabled = false;
          bookBtn.textContent = 'Book Appointment';
          return;
        }

        if (cardNumber === '4000000000000127') {
          // Incorrect CVV
          errorMsg.textContent = 'Incorrect CVV. Please check your card details.';
          errorDiv.style.display = 'block';
          bookBtn.disabled = false;
          bookBtn.textContent = 'Book Appointment';
          return;
        }

        // Success - complete the booking
        completeBooking();
      }, 1500); // 1.5 second delay to simulate processing
    }

    // Initialize
    document.addEventListener('DOMContentLoaded', () => {
      initFromParams();
      renderServices();

      // Auto-expand first category with multiple items for demo
      setTimeout(() => {
        const categories = document.querySelectorAll('.service-category');
        for (const cat of categories) {
          const count = cat.querySelectorAll('.service-item').length;
          if (count > 2) {
            cat.classList.add('expanded');
            break;
          }
        }
      }, 100);
    });
  </script>
</body>
</html>`
