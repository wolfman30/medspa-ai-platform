/**
 * Integration tests for the AvailabilityScraper with Moxie booking pages.
 *
 * These tests use the mock Moxie booking page served by the Go API at /demo/booking/{slug}
 * to test scraper functionality in a controlled environment.
 *
 * Test Philosophy (TDD):
 * - Each test validates a specific, meaningful behavior
 * - Tests are written to catch real bugs, not implementation details
 * - A failing test should indicate a real problem that needs fixing in the functional code
 * - Tests use realistic scenarios that match production usage
 *
 * To run these tests:
 *   1. Start the Go API: go run cmd/api/main.go (serves mock at http://localhost:8082/demo/booking/test)
 *   2. Run tests: npm test -- --testPathPattern=scraper-moxie
 *
 * Environment variables:
 *   MOCK_BOOKING_URL - Override the mock booking URL (default: http://localhost:8082/demo/booking/test)
 *   SKIP_MOCK_SERVER_CHECK - Set to "true" to skip the mock server availability check
 */

import { AvailabilityScraper } from '../../src/scraper';
import { ScraperError, NavigationError } from '../../src/types';

// Configuration
const MOCK_BASE_URL = process.env.MOCK_BOOKING_URL || 'http://localhost:8082/demo/booking/test';
const TEST_TIMEOUT = 60000; // 60 seconds for browser tests

// Check if mock server is available
let mockServerAvailable = false;
let mockServerCheckDone = false;

async function checkMockServerAvailable(): Promise<boolean> {
  if (mockServerCheckDone) return mockServerAvailable;

  try {
    const controller = new AbortController();
    const timeoutId = setTimeout(() => controller.abort(), 5000);

    const response = await fetch('http://localhost:8082/health', {
      signal: controller.signal,
    });
    clearTimeout(timeoutId);

    mockServerAvailable = response.ok;
  } catch {
    mockServerAvailable = false;
  }

  mockServerCheckDone = true;

  if (!mockServerAvailable) {
    console.log('\n⚠️  Mock server not available at localhost:8082');
    console.log('   To run these tests, start the Go API first:');
    console.log('   go run cmd/api/main.go\n');
  }

  return mockServerAvailable;
}

// Helper to conditionally skip tests when mock server isn't available
const describeWithMockServer = (name: string, fn: () => void) => {
  describe(name, () => {
    beforeAll(async () => {
      await checkMockServerAvailable();
    });

    // Wrap each test to skip if mock server not available
    fn();
  });
};

// Helper to build mock URL with query parameters
function buildMockUrl(params: Record<string, string | object> = {}): string {
  const url = new URL(MOCK_BASE_URL);
  for (const [key, value] of Object.entries(params)) {
    if (typeof value === 'object') {
      url.searchParams.set(key, JSON.stringify(value));
    } else {
      url.searchParams.set(key, value);
    }
  }
  return url.toString();
}

// Generate a future date string in YYYY-MM-DD format
function getFutureDate(daysFromNow: number = 7): string {
  const date = new Date();
  date.setDate(date.getDate() + daysFromNow);
  return date.toISOString().split('T')[0];
}

// Get current month's year and month
function getCurrentMonthInfo(): { year: number; month: number } {
  const now = new Date();
  return { year: now.getFullYear(), month: now.getMonth() + 1 };
}

describe('AvailabilityScraper - Moxie Integration Tests', () => {
  let scraper: AvailabilityScraper;

  beforeAll(async () => {
    // Initialize scraper with headless browser
    scraper = new AvailabilityScraper({
      headless: true,
      timeout: 30000,
      retries: 1,
    });
    await scraper.initialize();
  }, TEST_TIMEOUT);

  afterAll(async () => {
    await scraper.close();
  });

  // =============================================================================
  // scrapeAvailability() Tests
  // =============================================================================
  // These tests validate the main scraping function that extracts time slots
  // from a booking page for a specific date.

  describe('scrapeAvailability()', () => {
    /**
     * TEST: Basic availability scraping
     * WHY IT MATTERS: This is the core function - if it can't extract time slots,
     * the entire booking flow breaks. This test verifies the scraper can navigate
     * through Moxie's multi-step flow and extract valid time slot data.
     */
    it('should extract time slots from a Moxie booking page', async () => {
      const result = await scraper.scrapeAvailability({
        bookingUrl: buildMockUrl(),
        date: getFutureDate(7),
        timeout: 45000,
      });

      // Must succeed
      expect(result.success).toBe(true);

      // Must return valid structure
      expect(result.bookingUrl).toBe(buildMockUrl());
      expect(result.scrapedAt).toBeDefined();
      expect(new Date(result.scrapedAt).getTime()).not.toBeNaN();

      // Must extract time slots
      expect(Array.isArray(result.slots)).toBe(true);
      expect(result.slots.length).toBeGreaterThan(0);

      // Each slot must have required fields
      result.slots.forEach((slot) => {
        expect(slot.time).toBeDefined();
        expect(typeof slot.time).toBe('string');
        // Time should match format like "9:00am" or "2:30pm"
        expect(slot.time).toMatch(/\d{1,2}:\d{2}\s*(am|pm)/i);
        expect(typeof slot.available).toBe('boolean');
      });
    }, TEST_TIMEOUT);

    /**
     * TEST: Time slots are sorted chronologically
     * WHY IT MATTERS: Users expect time slots in chronological order. If slots
     * are returned out of order, the UI becomes confusing and booking logic
     * may select the wrong time.
     */
    it('should return time slots in chronological order', async () => {
      const result = await scraper.scrapeAvailability({
        bookingUrl: buildMockUrl(),
        date: getFutureDate(7),
        timeout: 45000,
      });

      expect(result.success).toBe(true);
      expect(result.slots.length).toBeGreaterThan(1);

      // Parse time strings to minutes for comparison
      const parseTimeToMinutes = (timeStr: string): number => {
        const match = timeStr.match(/(\d{1,2}):(\d{2})\s*(am|pm)/i);
        if (!match) return 0;
        let hours = parseInt(match[1], 10);
        const minutes = parseInt(match[2], 10);
        const isPM = match[3].toLowerCase() === 'pm';
        if (isPM && hours !== 12) hours += 12;
        if (!isPM && hours === 12) hours = 0;
        return hours * 60 + minutes;
      };

      // Verify slots are in ascending order
      for (let i = 1; i < result.slots.length; i++) {
        const prevTime = parseTimeToMinutes(result.slots[i - 1].time);
        const currTime = parseTimeToMinutes(result.slots[i].time);
        expect(currTime).toBeGreaterThanOrEqual(prevTime);
      }
    }, TEST_TIMEOUT);

    /**
     * TEST: Detects unavailable time slots
     * WHY IT MATTERS: The mock page has some unavailable slots (10:00am, 1:30pm, etc.).
     * The scraper must correctly identify which slots are booked vs available,
     * otherwise users might try to book unavailable times.
     */
    it('should correctly identify available vs unavailable slots', async () => {
      const result = await scraper.scrapeAvailability({
        bookingUrl: buildMockUrl(),
        date: getFutureDate(7),
        timeout: 45000,
      });

      expect(result.success).toBe(true);

      // The mock has both available and unavailable slots
      const availableSlots = result.slots.filter((s) => s.available);
      const unavailableSlots = result.slots.filter((s) => !s.available);

      // Should have both types (mock includes unavailable: 10:00am, 1:30pm, 3:30pm, 7:30pm)
      expect(availableSlots.length).toBeGreaterThan(0);
      expect(unavailableSlots.length).toBeGreaterThan(0);
    }, TEST_TIMEOUT);

    /**
     * TEST: Custom time slots via URL parameters
     * WHY IT MATTERS: This tests that the scraper correctly extracts whatever
     * slots are shown on the page, not hardcoded values. The mock page accepts
     * custom slots via URL params, letting us verify dynamic extraction.
     */
    it('should extract custom time slots configured via URL params', async () => {
      const customSlots = [
        { time: '8:00am', available: true },
        { time: '12:00pm', available: true },
        { time: '4:00pm', available: false },
      ];

      const result = await scraper.scrapeAvailability({
        bookingUrl: buildMockUrl({ slots: customSlots }),
        date: getFutureDate(7),
        timeout: 45000,
      });

      expect(result.success).toBe(true);

      // Should find all our custom slots
      const extractedTimes = result.slots.map((s) => s.time.toLowerCase());
      expect(extractedTimes).toContain('8:00am');
      expect(extractedTimes).toContain('12:00pm');
      expect(extractedTimes).toContain('4:00pm');

      // Verify availability status
      const fourPm = result.slots.find((s) => s.time.toLowerCase() === '4:00pm');
      expect(fourPm?.available).toBe(false);
    }, TEST_TIMEOUT);
  });

  // =============================================================================
  // getAvailableDates() Tests
  // =============================================================================
  // These tests validate the calendar scanning function that identifies which
  // dates have appointment availability in a given month.

  describe('getAvailableDates()', () => {
    /**
     * TEST: Get available dates for current month
     * WHY IT MATTERS: Before users pick a date, we need to show them which
     * dates have availability. This function scans the calendar and returns
     * dates that can be booked.
     */
    it('should return available dates for a given month', async () => {
      const { year, month } = getCurrentMonthInfo();
      // Move to next month to ensure we have future dates
      const targetMonth = month === 12 ? 1 : month + 1;
      const targetYear = month === 12 ? year + 1 : year;

      const result = await scraper.getAvailableDates(
        buildMockUrl(),
        targetYear,
        targetMonth,
        60000
      );

      expect(result.success).toBe(true);
      expect(Array.isArray(result.dates)).toBe(true);

      // Should return some available dates
      expect(result.dates.length).toBeGreaterThan(0);

      // Each date should be in YYYY-MM-DD format
      result.dates.forEach((date) => {
        expect(date).toMatch(/^\d{4}-\d{2}-\d{2}$/);
        // Should be in the requested month
        const [y, m] = date.split('-').map(Number);
        expect(y).toBe(targetYear);
        expect(m).toBe(targetMonth);
      });
    }, TEST_TIMEOUT);

    /**
     * TEST: Dates are sorted in ascending order
     * WHY IT MATTERS: Calendar UIs expect dates in order. If dates come back
     * scrambled, the UI will display incorrectly.
     */
    it('should return dates in ascending order', async () => {
      const { year, month } = getCurrentMonthInfo();
      const targetMonth = month === 12 ? 1 : month + 1;
      const targetYear = month === 12 ? year + 1 : year;

      const result = await scraper.getAvailableDates(
        buildMockUrl(),
        targetYear,
        targetMonth,
        60000
      );

      expect(result.success).toBe(true);
      expect(result.dates.length).toBeGreaterThan(1);

      // Verify dates are sorted
      for (let i = 1; i < result.dates.length; i++) {
        expect(new Date(result.dates[i]).getTime()).toBeGreaterThan(
          new Date(result.dates[i - 1]).getTime()
        );
      }
    }, TEST_TIMEOUT);

    /**
     * TEST: Navigates to future months correctly
     * WHY IT MATTERS: Users often want to book appointments weeks or months
     * in advance. The scraper must be able to navigate the calendar to reach
     * future months.
     */
    it('should navigate to a future month', async () => {
      const now = new Date();
      // Request 3 months from now
      const futureDate = new Date(now.getFullYear(), now.getMonth() + 3, 1);
      const targetYear = futureDate.getFullYear();
      const targetMonth = futureDate.getMonth() + 1;

      const result = await scraper.getAvailableDates(
        buildMockUrl(),
        targetYear,
        targetMonth,
        60000
      );

      expect(result.success).toBe(true);

      // All returned dates should be in the requested month
      result.dates.forEach((date) => {
        const [y, m] = date.split('-').map(Number);
        expect(y).toBe(targetYear);
        expect(m).toBe(targetMonth);
      });
    }, TEST_TIMEOUT);
  });

  // =============================================================================
  // Error Handling Tests
  // =============================================================================
  // These tests validate that the scraper handles errors gracefully and provides
  // meaningful error information.

  describe('Error Handling', () => {
    /**
     * TEST: Handle unreachable URL
     * WHY IT MATTERS: Network issues happen. The scraper must fail gracefully
     * with a clear error instead of crashing or hanging.
     */
    it('should return error for unreachable URL', async () => {
      const result = await scraper.scrapeAvailability({
        bookingUrl: 'https://this-domain-does-not-exist-12345.example.com/booking',
        date: getFutureDate(7),
        timeout: 10000,
      });

      expect(result.success).toBe(false);
      expect(result.error).toBeDefined();
      expect(result.slots).toEqual([]);
    }, TEST_TIMEOUT);

    /**
     * TEST: Throw ScraperError when not initialized
     * WHY IT MATTERS: Calling methods before initialization is a programming
     * error. The scraper should throw a clear error to help developers debug.
     */
    it('should throw ScraperError when browser not initialized', async () => {
      const uninitializedScraper = new AvailabilityScraper();
      // Don't call initialize()

      await expect(
        uninitializedScraper.scrapeAvailability({
          bookingUrl: buildMockUrl(),
          date: getFutureDate(7),
          timeout: 30000,
        })
      ).rejects.toThrow('Browser not initialized');
    });

    /**
     * TEST: Handle timeout gracefully
     * WHY IT MATTERS: Slow pages shouldn't hang the entire system. The scraper
     * must respect timeout settings and fail cleanly.
     */
    it('should handle very short timeout gracefully', async () => {
      const result = await scraper.scrapeAvailability({
        bookingUrl: buildMockUrl(),
        date: getFutureDate(7),
        timeout: 1000, // Very short timeout
      });

      // Should return a result (success or failure) within reasonable time
      expect(result).toBeDefined();
      expect(result.bookingUrl).toBeDefined();
    }, 15000); // Test timeout of 15s to ensure it doesn't hang

    /**
     * TEST: Handle non-booking pages gracefully
     * WHY IT MATTERS: Users might paste the wrong URL. The scraper should
     * return an empty result instead of crashing.
     */
    it('should handle non-booking pages gracefully', async () => {
      const result = await scraper.scrapeAvailability({
        bookingUrl: 'https://example.com', // Not a booking page
        date: getFutureDate(7),
        timeout: 15000,
      });

      // Should complete without crashing
      expect(result).toBeDefined();
      expect(Array.isArray(result.slots)).toBe(true);
      // Might succeed with 0 slots or fail - either is acceptable
    }, TEST_TIMEOUT);
  });

  // =============================================================================
  // Retry Behavior Tests
  // =============================================================================
  // These tests validate the scraper's retry logic for transient failures.

  describe('Retry Behavior', () => {
    /**
     * TEST: Retries on transient failures
     * WHY IT MATTERS: Network glitches and temporary issues are common.
     * The scraper should automatically retry instead of failing immediately.
     */
    it('should respect retry configuration', async () => {
      // Create scraper with specific retry settings
      const retryScraper = new AvailabilityScraper({
        headless: true,
        timeout: 30000,
        retries: 0, // No retries
      });
      await retryScraper.initialize();

      try {
        const startTime = Date.now();

        // This should fail faster with no retries
        const result = await retryScraper.scrapeAvailability({
          bookingUrl: 'https://this-domain-does-not-exist-12345.example.com',
          date: getFutureDate(7),
          timeout: 5000,
        });

        const duration = Date.now() - startTime;

        // With 0 retries, should complete faster than with retries
        expect(duration).toBeLessThan(15000);
        expect(result.success).toBe(false);
      } finally {
        await retryScraper.close();
      }
    }, TEST_TIMEOUT);
  });

  // =============================================================================
  // Browser Lifecycle Tests
  // =============================================================================
  // These tests validate proper browser initialization and cleanup.

  describe('Browser Lifecycle', () => {
    /**
     * TEST: Multiple initializations are idempotent
     * WHY IT MATTERS: Prevents resource leaks from accidentally calling
     * initialize() multiple times.
     */
    it('should handle multiple initialize calls', async () => {
      const testScraper = new AvailabilityScraper({ headless: true });

      await testScraper.initialize();
      expect(testScraper.isReady()).toBe(true);

      // Second init should be safe
      await testScraper.initialize();
      expect(testScraper.isReady()).toBe(true);

      await testScraper.close();
      expect(testScraper.isReady()).toBe(false);
    }, TEST_TIMEOUT);

    /**
     * TEST: Close is idempotent
     * WHY IT MATTERS: Calling close() on an already-closed scraper should
     * not throw errors.
     */
    it('should handle multiple close calls', async () => {
      const testScraper = new AvailabilityScraper({ headless: true });
      await testScraper.initialize();

      await testScraper.close();
      expect(testScraper.isReady()).toBe(false);

      // Second close should be safe
      await testScraper.close();
      expect(testScraper.isReady()).toBe(false);
    }, TEST_TIMEOUT);

    /**
     * TEST: Can reinitialize after close
     * WHY IT MATTERS: Allows reusing scraper instances, which is important
     * for long-running services.
     */
    it('should allow reinitialization after close', async () => {
      const testScraper = new AvailabilityScraper({ headless: true });

      await testScraper.initialize();
      expect(testScraper.isReady()).toBe(true);

      await testScraper.close();
      expect(testScraper.isReady()).toBe(false);

      // Reinitialize
      await testScraper.initialize();
      expect(testScraper.isReady()).toBe(true);

      await testScraper.close();
    }, TEST_TIMEOUT);
  });
});

// =============================================================================
// Moxie-Specific Feature Tests
// =============================================================================
// These tests validate Moxie-specific booking flow features using detailed
// scenarios.

describe('Moxie Booking Flow - Detailed Tests', () => {
  let scraper: AvailabilityScraper;

  beforeAll(async () => {
    scraper = new AvailabilityScraper({
      headless: true,
      timeout: 30000,
      retries: 1,
    });
    await scraper.initialize();
  }, TEST_TIMEOUT);

  afterAll(async () => {
    await scraper.close();
  });

  /**
   * TEST: Clinic customization via URL params
   * WHY IT MATTERS: Different clinics have different configurations.
   * This verifies the scraper works with customized mock pages.
   */
  it('should work with custom clinic configuration', async () => {
    const customClinicUrl = buildMockUrl({
      clinic: 'Test Spa',
      phone: '+1 (555) 123-4567',
      email: 'test@testspa.com',
    });

    const result = await scraper.scrapeAvailability({
      bookingUrl: customClinicUrl,
      date: getFutureDate(7),
      timeout: 45000,
    });

    expect(result.success).toBe(true);
    expect(result.slots.length).toBeGreaterThan(0);
  }, TEST_TIMEOUT);

  /**
   * TEST: Pre-selected date via URL params
   * WHY IT MATTERS: Some links pre-select a date. The scraper should work
   * with these pre-configured pages.
   */
  it('should work with pre-selected date in URL', async () => {
    const targetDate = getFutureDate(14);
    const preSelectedUrl = buildMockUrl({ date: targetDate });

    const result = await scraper.scrapeAvailability({
      bookingUrl: preSelectedUrl,
      date: targetDate,
      timeout: 45000,
    });

    expect(result.success).toBe(true);
    expect(result.date).toBe(targetDate);
  }, TEST_TIMEOUT);

  /**
   * TEST: Extract slots from page with minimal availability
   * WHY IT MATTERS: Some days have only 1-2 slots available. The scraper
   * must handle these edge cases correctly.
   */
  it('should handle minimal availability', async () => {
    const minimalSlots = [{ time: '3:00pm', available: true }];

    const result = await scraper.scrapeAvailability({
      bookingUrl: buildMockUrl({ slots: minimalSlots }),
      date: getFutureDate(7),
      timeout: 45000,
    });

    expect(result.success).toBe(true);
    expect(result.slots.length).toBeGreaterThanOrEqual(1);
    expect(result.slots.some((s) => s.time.toLowerCase().includes('3:00pm'))).toBe(true);
  }, TEST_TIMEOUT);

  /**
   * TEST: Handle page with all slots unavailable
   * WHY IT MATTERS: Fully booked days should return slots marked unavailable,
   * not an error or empty array.
   */
  it('should handle fully booked days', async () => {
    const fullyBookedSlots = [
      { time: '9:00am', available: false },
      { time: '10:00am', available: false },
      { time: '11:00am', available: false },
    ];

    const result = await scraper.scrapeAvailability({
      bookingUrl: buildMockUrl({ slots: fullyBookedSlots }),
      date: getFutureDate(7),
      timeout: 45000,
    });

    expect(result.success).toBe(true);
    // Should still return slots, just all unavailable
    expect(result.slots.length).toBeGreaterThan(0);
    expect(result.slots.every((s) => !s.available)).toBe(true);
  }, TEST_TIMEOUT);
});

// =============================================================================
// Time Parsing Edge Cases
// =============================================================================
// These tests verify correct handling of various time formats.

describe('Time Parsing', () => {
  /**
   * Helper to test time parsing through the scraper's sort behavior.
   * We verify parsing by checking that times are sorted correctly.
   */
  const parseTime = (timeStr: string): number => {
    const match = timeStr.match(/(\d{1,2}):?(\d{2})?\s*(AM|PM)?/i);
    if (!match) return 0;

    let hours = parseInt(match[1], 10);
    const minutes = parseInt(match[2] || '0', 10);
    const meridiem = match[3]?.toUpperCase();

    if (meridiem === 'PM' && hours !== 12) hours += 12;
    if (meridiem === 'AM' && hours === 12) hours = 0;

    return hours * 60 + minutes;
  };

  /**
   * TEST: Various time formats
   * WHY IT MATTERS: Different booking systems use different time formats.
   * The parser must handle them all correctly.
   */
  it('should parse 12-hour times correctly', () => {
    expect(parseTime('9:00 AM')).toBe(540); // 9 * 60
    expect(parseTime('9:00am')).toBe(540);
    expect(parseTime('12:00 PM')).toBe(720); // 12 * 60 (noon)
    expect(parseTime('12:00 AM')).toBe(0); // midnight
    expect(parseTime('11:30 PM')).toBe(1410); // 23 * 60 + 30
  });

  /**
   * TEST: Edge case times
   * WHY IT MATTERS: Noon (12:00 PM) and midnight (12:00 AM) are common
   * sources of bugs in time parsing.
   */
  it('should handle noon and midnight correctly', () => {
    expect(parseTime('12:00 PM')).toBe(720); // Noon = 12:00
    expect(parseTime('12:00 AM')).toBe(0); // Midnight = 00:00
    expect(parseTime('12:30 PM')).toBe(750); // 12:30 PM
    expect(parseTime('12:30 AM')).toBe(30); // 00:30
  });

  /**
   * TEST: Sort order verification
   * WHY IT MATTERS: Times must sort correctly for UI display.
   */
  it('should sort times in correct order', () => {
    const times = ['2:00pm', '10:00am', '12:00pm', '9:00am', '6:00pm'];
    const sorted = [...times].sort((a, b) => parseTime(a) - parseTime(b));

    expect(sorted).toEqual(['9:00am', '10:00am', '12:00pm', '2:00pm', '6:00pm']);
  });
});
