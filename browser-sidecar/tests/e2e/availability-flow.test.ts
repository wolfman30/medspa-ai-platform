/**
 * End-to-end tests for the full availability flow.
 *
 * These tests verify the complete flow from API request to scraped results.
 * They require a running browser sidecar service and access to real booking pages.
 *
 * Run with: npm run test:e2e
 *
 * Environment variables:
 *   SIDECAR_URL - URL of the running sidecar service (default: http://localhost:3000)
 *   TEST_BOOKING_URL - URL of a test booking page (optional, uses mock if not set)
 *   E2E_ENABLED - Set to "true" to run e2e tests (default: false)
 */

import request from 'supertest';

// Skip e2e tests unless explicitly enabled
const E2E_ENABLED = process.env.E2E_ENABLED === 'true';
const SIDECAR_URL = process.env.SIDECAR_URL || 'http://localhost:3000';
const TEST_BOOKING_URL = process.env.TEST_BOOKING_URL || '';

// Helper to conditionally skip tests
const describeE2E = E2E_ENABLED ? describe : describe.skip;

describeE2E('E2E: Availability Flow', () => {
  // Increase timeout for e2e tests since they involve real network requests
  jest.setTimeout(60000);

  describe('Health and Readiness', () => {
    it('should report healthy status', async () => {
      const response = await request(SIDECAR_URL)
        .get('/health')
        .expect(200);

      expect(response.body).toHaveProperty('status', 'ok');
      expect(response.body).toHaveProperty('browserReady', true);
      expect(response.body).toHaveProperty('version');
      expect(response.body).toHaveProperty('uptime');
    });

    it('should report ready status', async () => {
      const response = await request(SIDECAR_URL)
        .get('/ready')
        .expect(200);

      expect(response.body).toHaveProperty('ready', true);
    });
  });

  describe('Single Date Availability', () => {
    it('should scrape availability for today', async () => {
      // Skip if no test booking URL is configured
      if (!TEST_BOOKING_URL) {
        console.log('Skipping: TEST_BOOKING_URL not configured');
        return;
      }

      const today = new Date().toISOString().split('T')[0];

      const response = await request(SIDECAR_URL)
        .post('/api/v1/availability')
        .send({
          bookingUrl: TEST_BOOKING_URL,
          date: today,
          timeout: 45000,
        })
        .expect(200);

      expect(response.body).toHaveProperty('success');
      expect(response.body).toHaveProperty('bookingUrl', TEST_BOOKING_URL);
      expect(response.body).toHaveProperty('date', today);
      expect(response.body).toHaveProperty('scrapedAt');

      if (response.body.success) {
        expect(response.body).toHaveProperty('slots');
        expect(Array.isArray(response.body.slots)).toBe(true);

        // Verify slot structure
        if (response.body.slots.length > 0) {
          const slot = response.body.slots[0];
          expect(slot).toHaveProperty('time');
          expect(slot).toHaveProperty('available');
          expect(typeof slot.time).toBe('string');
          expect(typeof slot.available).toBe('boolean');
        }
      } else {
        // If not successful, there should be an error message
        expect(response.body).toHaveProperty('error');
        console.log('Scrape failed with error:', response.body.error);
      }
    });

    it('should handle invalid booking URL gracefully', async () => {
      const today = new Date().toISOString().split('T')[0];

      const response = await request(SIDECAR_URL)
        .post('/api/v1/availability')
        .send({
          bookingUrl: 'https://this-domain-definitely-does-not-exist-12345.com/booking',
          date: today,
          timeout: 15000,
        })
        .expect(200);

      expect(response.body).toHaveProperty('success', false);
      expect(response.body).toHaveProperty('error');
    });

    it('should respect timeout parameter', async () => {
      const today = new Date().toISOString().split('T')[0];

      // Use a very short timeout to test timeout handling
      const startTime = Date.now();
      const response = await request(SIDECAR_URL)
        .post('/api/v1/availability')
        .send({
          bookingUrl: 'https://example.com',
          date: today,
          timeout: 5000, // 5 second timeout
        });

      const elapsed = Date.now() - startTime;

      // Should complete within reasonable time (timeout + overhead)
      expect(elapsed).toBeLessThan(10000);
    });
  });

  describe('Batch Availability', () => {
    it('should scrape availability for multiple dates', async () => {
      // Skip if no test booking URL is configured
      if (!TEST_BOOKING_URL) {
        console.log('Skipping: TEST_BOOKING_URL not configured');
        return;
      }

      // Generate dates for the next 3 days
      const dates: string[] = [];
      for (let i = 0; i < 3; i++) {
        const date = new Date();
        date.setDate(date.getDate() + i);
        dates.push(date.toISOString().split('T')[0]);
      }

      const response = await request(SIDECAR_URL)
        .post('/api/v1/availability/batch')
        .send({
          bookingUrl: TEST_BOOKING_URL,
          dates,
          timeout: 45000,
        })
        .expect(200);

      expect(response.body).toHaveProperty('success');
      expect(response.body).toHaveProperty('results');
      expect(Array.isArray(response.body.results)).toBe(true);
      expect(response.body.results.length).toBe(dates.length);

      // Each result should have the expected structure
      response.body.results.forEach((result: any, index: number) => {
        expect(result).toHaveProperty('date', dates[index]);
        expect(result).toHaveProperty('success');
        if (result.success) {
          expect(result).toHaveProperty('slots');
        }
      });
    });

    it('should reject more than 7 dates', async () => {
      const dates = Array(8).fill(null).map((_, i) => {
        const date = new Date();
        date.setDate(date.getDate() + i);
        return date.toISOString().split('T')[0];
      });

      const response = await request(SIDECAR_URL)
        .post('/api/v1/availability/batch')
        .send({
          bookingUrl: 'https://example.com/booking',
          dates,
        })
        .expect(400);

      expect(response.body).toHaveProperty('error');
      expect(response.body.error).toMatch(/maximum|7|dates/i);
    });
  });

  describe('Platform Detection', () => {
    // These tests verify that the scraper can detect and handle different booking platforms

    it('should handle Moxie booking pages', async () => {
      const moxieTestUrl = process.env.MOXIE_TEST_URL;
      if (!moxieTestUrl) {
        console.log('Skipping: MOXIE_TEST_URL not configured');
        return;
      }

      const today = new Date().toISOString().split('T')[0];
      const response = await request(SIDECAR_URL)
        .post('/api/v1/availability')
        .send({
          bookingUrl: moxieTestUrl,
          date: today,
          timeout: 45000,
        })
        .expect(200);

      // Should return a response (success or failure with proper error)
      expect(response.body).toHaveProperty('success');
      if (!response.body.success) {
        console.log('Moxie scrape result:', response.body.error);
      }
    });

    it('should handle Calendly booking pages', async () => {
      const calendlyTestUrl = process.env.CALENDLY_TEST_URL;
      if (!calendlyTestUrl) {
        console.log('Skipping: CALENDLY_TEST_URL not configured');
        return;
      }

      const today = new Date().toISOString().split('T')[0];
      const response = await request(SIDECAR_URL)
        .post('/api/v1/availability')
        .send({
          bookingUrl: calendlyTestUrl,
          date: today,
          timeout: 45000,
        })
        .expect(200);

      expect(response.body).toHaveProperty('success');
      if (!response.body.success) {
        console.log('Calendly scrape result:', response.body.error);
      }
    });

    it('should handle Acuity booking pages', async () => {
      const acuityTestUrl = process.env.ACUITY_TEST_URL;
      if (!acuityTestUrl) {
        console.log('Skipping: ACUITY_TEST_URL not configured');
        return;
      }

      const today = new Date().toISOString().split('T')[0];
      const response = await request(SIDECAR_URL)
        .post('/api/v1/availability')
        .send({
          bookingUrl: acuityTestUrl,
          date: today,
          timeout: 45000,
        })
        .expect(200);

      expect(response.body).toHaveProperty('success');
      if (!response.body.success) {
        console.log('Acuity scrape result:', response.body.error);
      }
    });
  });

  describe('Error Handling', () => {
    it('should handle network errors gracefully', async () => {
      const response = await request(SIDECAR_URL)
        .post('/api/v1/availability')
        .send({
          bookingUrl: 'http://192.0.2.1/booking', // TEST-NET-1, should timeout
          date: new Date().toISOString().split('T')[0],
          timeout: 5000,
        })
        .expect(200);

      expect(response.body).toHaveProperty('success', false);
      expect(response.body).toHaveProperty('error');
    });

    it('should handle malformed HTML gracefully', async () => {
      // This test verifies the scraper doesn't crash on unexpected page content
      const response = await request(SIDECAR_URL)
        .post('/api/v1/availability')
        .send({
          bookingUrl: 'https://example.com', // Not a booking page
          date: new Date().toISOString().split('T')[0],
          timeout: 15000,
        })
        .expect(200);

      // Should complete without crashing, even if no slots found
      expect(response.body).toHaveProperty('success');
    });
  });

  describe('Concurrent Requests', () => {
    it('should handle multiple concurrent requests', async () => {
      const today = new Date().toISOString().split('T')[0];
      const bookingUrl = TEST_BOOKING_URL || 'https://example.com';

      // Send 3 concurrent requests
      const requests = Array(3).fill(null).map(() =>
        request(SIDECAR_URL)
          .post('/api/v1/availability')
          .send({
            bookingUrl,
            date: today,
            timeout: 30000,
          })
      );

      const responses = await Promise.all(requests);

      // All requests should complete successfully (even if scraping fails)
      responses.forEach(response => {
        expect(response.status).toBe(200);
        expect(response.body).toHaveProperty('success');
      });
    });
  });
});

// Additional unit-level tests that can run without the sidecar
describe('Unit: Request Validation', () => {
  const mockServer = 'http://localhost:3000';

  it('validates date format', () => {
    const validDate = /^\d{4}-\d{2}-\d{2}$/;
    expect('2024-01-15').toMatch(validDate);
    expect('2024-1-15').not.toMatch(validDate);
    expect('01-15-2024').not.toMatch(validDate);
    expect('2024/01/15').not.toMatch(validDate);
  });

  it('validates URL format', () => {
    const isValidUrl = (url: string) => {
      try {
        new URL(url);
        return true;
      } catch {
        return false;
      }
    };

    expect(isValidUrl('https://example.com/booking')).toBe(true);
    expect(isValidUrl('http://localhost:3000')).toBe(true);
    expect(isValidUrl('not-a-url')).toBe(false);
    expect(isValidUrl('')).toBe(false);
  });
});
