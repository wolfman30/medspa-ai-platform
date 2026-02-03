import { z } from 'zod';

// Request schemas
export const AvailabilityRequestSchema = z.object({
  bookingUrl: z.string().url(),
  date: z.string().regex(/^\d{4}-\d{2}-\d{2}$/, 'Date must be YYYY-MM-DD format'),
  serviceName: z.string().optional(),
  providerName: z.string().optional(),
  timeout: z.number().min(1000).max(60000).default(30000),
  /**
   * dryRun: If true, only check availability without booking (default: true)
   * Currently, the scraper ALWAYS runs in dry-run mode (only checks availability).
   * This flag is reserved for future functionality when actual booking is implemented.
   * Production mode (dryRun: false) would complete the booking after deposit collection.
   */
  dryRun: z.boolean().optional(),
});

export type AvailabilityRequest = z.infer<typeof AvailabilityRequestSchema>;

// Response types
export interface TimeSlot {
  time: string;        // e.g., "10:00 AM"
  available: boolean;
  provider?: string;
  duration?: number;   // minutes
}

export interface AvailabilityResponse {
  success: boolean;
  bookingUrl: string;
  date: string;
  slots: TimeSlot[];
  provider?: string;
  service?: string;
  /**
   * List of available providers for the selected service.
   * If only one provider exists, AI should skip asking for preference.
   * If multiple providers exist, AI should ask which one the patient prefers.
   */
  providers?: string[];
  scrapedAt: string;   // ISO timestamp
  error?: string;
}

export interface HealthResponse {
  status: 'ok' | 'degraded' | 'error';
  version: string;
  browserReady: boolean;
  uptime: number;
}

// Scraper configuration
export interface ScraperConfig {
  headless: boolean;
  timeout: number;
  retries: number;
  userAgent?: string;
}

// Provider-specific selectors (extensible for different booking platforms)
export interface BookingPlatformSelectors {
  platform: 'moxie' | 'acuity' | 'calendly' | 'generic';
  dateSelector: string;
  timeSlotSelector: string;
  availableSlotClass: string;
  unavailableSlotClass?: string;
  providerSelector?: string;
  serviceSelector?: string;
  nextDayButton?: string;
  prevDayButton?: string;
}

// Moxie-specific selectors
export const MOXIE_SELECTORS: BookingPlatformSelectors = {
  platform: 'moxie',
  // Moxie uses a calendar with clickable day cells
  dateSelector: '.calendar-day:not(.disabled), [class*="calendar"] [class*="day"]:not([class*="disabled"])',
  // Time slots are buttons with times like "6:00pm", "9:30am"
  timeSlotSelector: 'button:has-text("am"), button:has-text("pm"), .time-slot, [class*="time-slot"], [class*="timeslot"]',
  availableSlotClass: 'available',
  unavailableSlotClass: 'unavailable',
  providerSelector: '.provider-name, [data-testid="provider"]',
  serviceSelector: '.service-name, [data-testid="service"]',
  // Moxie calendar uses month navigation arrows
  nextDayButton: '.calendar-nav button:last-child, [class*="calendar"] button:has-text(">"), button[aria-label*="next"]',
  prevDayButton: '.calendar-nav button:first-child, [class*="calendar"] button:has-text("<"), button[aria-label*="prev"]',
};

// Error types
export class ScraperError extends Error {
  constructor(
    message: string,
    public readonly code: string,
    public readonly retryable: boolean = false
  ) {
    super(message);
    this.name = 'ScraperError';
  }
}

export class TimeoutError extends ScraperError {
  constructor(message: string) {
    super(message, 'TIMEOUT', true);
    this.name = 'TimeoutError';
  }
}

export class NavigationError extends ScraperError {
  constructor(message: string) {
    super(message, 'NAVIGATION_ERROR', true);
    this.name = 'NavigationError';
  }
}

export class SelectorNotFoundError extends ScraperError {
  constructor(selector: string) {
    super(`Selector not found: ${selector}`, 'SELECTOR_NOT_FOUND', false);
    this.name = 'SelectorNotFoundError';
  }
}

// ============================================================================
// BOOKING SESSION TYPES
// ============================================================================

/**
 * Booking session state machine:
 * created → navigating → ready_for_handoff → monitoring → completed/failed/abandoned
 */
export type BookingSessionState =
  | 'created'           // Session created, not started
  | 'navigating'        // Browser automating steps 1-4
  | 'ready_for_handoff' // Reached step 5, waiting for lead
  | 'monitoring'        // Lead has handoff URL, watching for outcome
  | 'completed'         // Booking succeeded
  | 'failed'            // Booking failed (payment error, etc.)
  | 'abandoned'         // Timeout - lead didn't complete
  | 'cancelled';        // Manually cancelled

/**
 * Booking outcome - the final result of a booking attempt
 */
export type BookingOutcome =
  | 'success'           // Booking confirmed
  | 'payment_failed'    // Card declined, payment error
  | 'slot_unavailable'  // Time slot was taken
  | 'timeout'           // Lead didn't complete in time
  | 'cancelled'         // Manually cancelled
  | 'error';            // Unknown error

/**
 * Lead information for booking
 */
export interface LeadInfo {
  firstName: string;
  lastName: string;
  phone: string;
  email: string;
  notes?: string;
}

/**
 * Confirmation details returned after successful booking
 */
export interface ConfirmationDetails {
  confirmationNumber?: string;
  appointmentTime?: string;
  provider?: string;
  service?: string;
  rawText?: string;  // Raw confirmation text from page
}

/**
 * Request to start a booking session
 */
export const BookingStartRequestSchema = z.object({
  bookingUrl: z.string().url(),
  date: z.string().regex(/^\d{4}-\d{2}-\d{2}$/, 'Date must be YYYY-MM-DD format'),
  time: z.string().regex(/^\d{1,2}:\d{2}\s*(am|pm)$/i, 'Time must be like "2:30pm"'),
  lead: z.object({
    firstName: z.string().min(1),
    lastName: z.string().min(1),
    phone: z.string().min(10),
    email: z.string().email(),
    notes: z.string().optional(),
  }),
  service: z.string().optional(),
  provider: z.string().optional(),
  callbackUrl: z.string().url().optional(),
  timeout: z.number().min(30000).max(300000).default(120000), // 2 min default
});

export type BookingStartRequest = z.infer<typeof BookingStartRequestSchema>;

/**
 * Response from starting a booking session
 */
export interface BookingStartResponse {
  success: boolean;
  sessionId: string;
  state: BookingSessionState;
  error?: string;
}

/**
 * Response with handoff URL
 */
export interface BookingHandoffResponse {
  success: boolean;
  sessionId: string;
  handoffUrl: string;
  expiresAt: string;  // ISO timestamp
  state: BookingSessionState;
  error?: string;
}

/**
 * Response with session status
 */
export interface BookingStatusResponse {
  success: boolean;
  sessionId: string;
  state: BookingSessionState;
  outcome?: BookingOutcome;
  confirmationDetails?: ConfirmationDetails;
  error?: string;
  createdAt: string;
  updatedAt: string;
}

/**
 * Full booking session object (internal)
 */
export interface BookingSession {
  id: string;
  state: BookingSessionState;
  outcome?: BookingOutcome;

  // Request data
  bookingUrl: string;
  date: string;
  time: string;
  lead: LeadInfo;
  service?: string;
  provider?: string;
  callbackUrl?: string;

  // Handoff
  handoffUrl?: string;
  handoffExpiresAt?: Date;

  // Outcome
  confirmationDetails?: ConfirmationDetails;
  errorMessage?: string;

  // Timestamps
  createdAt: Date;
  updatedAt: Date;
  completedAt?: Date;

  // Browser context (internal - not serialized)
  browserContextId?: string;
  pageUrl?: string;
}

/**
 * Indicators used to detect booking outcome
 */
export interface OutcomeIndicators {
  // Success indicators
  successUrlPatterns: RegExp[];
  successTextPatterns: RegExp[];
  successSelectors: string[];

  // Failure indicators
  failureTextPatterns: RegExp[];
  failureSelectors: string[];
}

/**
 * Default outcome indicators for Moxie
 */
export const MOXIE_OUTCOME_INDICATORS: OutcomeIndicators = {
  successUrlPatterns: [
    /\/confirm/i,
    /\/success/i,
    /\/thank-?you/i,
    /\/complete/i,
    /step=6/i,  // Moxie might use step numbers
  ],
  successTextPatterns: [
    /booking\s+confirmed/i,
    /appointment\s+scheduled/i,
    /you('re| are)\s+all\s+set/i,
    /confirmation\s+(number|#|code)/i,
    /thank\s+you\s+for\s+(booking|your\s+appointment)/i,
  ],
  successSelectors: [
    '.confirmation-number',
    '[data-testid="confirmation"]',
    '[data-testid="success"]',
    '.booking-success',
    '.appointment-confirmed',
  ],
  failureTextPatterns: [
    /payment\s+failed/i,
    /card\s+declined/i,
    /transaction\s+failed/i,
    /try\s+again/i,
    /unable\s+to\s+process/i,
    /slot\s+(no\s+longer\s+)?available/i,
    /time\s+slot\s+taken/i,
  ],
  failureSelectors: [
    '.error-message',
    '.payment-error',
    '[data-testid="error"]',
    '.booking-failed',
  ],
};
