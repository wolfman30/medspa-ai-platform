import { z } from 'zod';

// Request schemas
export const AvailabilityRequestSchema = z.object({
  bookingUrl: z.string().url(),
  date: z.string().regex(/^\d{4}-\d{2}-\d{2}$/, 'Date must be YYYY-MM-DD format'),
  serviceName: z.string().optional(),
  providerName: z.string().optional(),
  timeout: z.number().min(1000).max(60000).default(30000),
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
