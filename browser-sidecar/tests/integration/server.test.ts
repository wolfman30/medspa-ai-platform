import request from 'supertest';
import { app } from '../../src/server';
import { getScraper, closeScraper } from '../../src/scraper';

// Add supertest to devDependencies
// npm install --save-dev supertest @types/supertest

describe('Browser Sidecar API', () => {
  beforeAll(async () => {
    // Initialize the scraper before tests
    await getScraper();
  }, 60000);

  afterAll(async () => {
    await closeScraper();
  });

  describe('GET /health', () => {
    it('should return health status', async () => {
      const response = await request(app).get('/health');

      expect(response.status).toBe(200);
      expect(response.body).toHaveProperty('status');
      expect(response.body).toHaveProperty('version');
      expect(response.body).toHaveProperty('browserReady');
      expect(response.body).toHaveProperty('uptime');
      expect(response.body.browserReady).toBe(true);
    });
  });

  describe('GET /ready', () => {
    it('should return ready status when browser is initialized', async () => {
      const response = await request(app).get('/ready');

      expect(response.status).toBe(200);
      expect(response.body.ready).toBe(true);
    });
  });

  describe('POST /api/v1/availability', () => {
    it('should reject request without bookingUrl', async () => {
      const response = await request(app)
        .post('/api/v1/availability')
        .send({ date: '2024-01-15' });

      expect(response.status).toBe(400);
      expect(response.body.success).toBe(false);
      expect(response.body.error).toBe('Invalid request');
    });

    it('should reject request without date', async () => {
      const response = await request(app)
        .post('/api/v1/availability')
        .send({ bookingUrl: 'https://example.com' });

      expect(response.status).toBe(400);
      expect(response.body.success).toBe(false);
    });

    it('should reject request with invalid date format', async () => {
      const response = await request(app)
        .post('/api/v1/availability')
        .send({
          bookingUrl: 'https://example.com',
          date: '01-15-2024',
        });

      expect(response.status).toBe(400);
      expect(response.body.success).toBe(false);
    });

    it('should reject request with invalid URL', async () => {
      const response = await request(app)
        .post('/api/v1/availability')
        .send({
          bookingUrl: 'not-a-url',
          date: '2024-01-15',
        });

      expect(response.status).toBe(400);
      expect(response.body.success).toBe(false);
    });

    it('should handle unreachable URL gracefully', async () => {
      const response = await request(app)
        .post('/api/v1/availability')
        .send({
          bookingUrl: 'https://this-domain-does-not-exist-12345.com',
          date: '2024-01-15',
          timeout: 5000,
        });

      // Should return 502 (Bad Gateway) for upstream errors
      expect(response.status).toBe(502);
      expect(response.body.success).toBe(false);
      expect(response.body.error).toBeDefined();
    }, 30000);

    it('should scrape a real booking page', async () => {
      // Use a known public booking page for testing
      // This is an integration test that hits a real website
      const response = await request(app)
        .post('/api/v1/availability')
        .send({
          bookingUrl: 'https://calendly.com',
          date: '2024-02-15',
          timeout: 30000,
        });

      // The response structure should be valid regardless of success
      expect(response.body).toHaveProperty('success');
      expect(response.body).toHaveProperty('bookingUrl');
      expect(response.body).toHaveProperty('date');
      expect(response.body).toHaveProperty('slots');
      expect(response.body).toHaveProperty('scrapedAt');
      expect(Array.isArray(response.body.slots)).toBe(true);
    }, 60000);
  });

  describe('POST /api/v1/availability/batch', () => {
    it('should reject request without bookingUrl', async () => {
      const response = await request(app)
        .post('/api/v1/availability/batch')
        .send({ dates: ['2024-01-15'] });

      expect(response.status).toBe(400);
      expect(response.body.success).toBe(false);
    });

    it('should reject request without dates', async () => {
      const response = await request(app)
        .post('/api/v1/availability/batch')
        .send({ bookingUrl: 'https://example.com' });

      expect(response.status).toBe(400);
      expect(response.body.success).toBe(false);
    });

    it('should reject request with empty dates array', async () => {
      const response = await request(app)
        .post('/api/v1/availability/batch')
        .send({
          bookingUrl: 'https://example.com',
          dates: [],
        });

      expect(response.status).toBe(400);
      expect(response.body.success).toBe(false);
    });

    it('should reject request with more than 7 dates', async () => {
      const response = await request(app)
        .post('/api/v1/availability/batch')
        .send({
          bookingUrl: 'https://example.com',
          dates: [
            '2024-01-15',
            '2024-01-16',
            '2024-01-17',
            '2024-01-18',
            '2024-01-19',
            '2024-01-20',
            '2024-01-21',
            '2024-01-22', // 8th date
          ],
        });

      expect(response.status).toBe(400);
      expect(response.body.success).toBe(false);
      expect(response.body.error).toContain('Maximum 7 dates');
    });
  });

  describe('Error handling', () => {
    it('should return 404 for unknown routes', async () => {
      const response = await request(app).get('/unknown-route');
      expect(response.status).toBe(404);
    });

    it('should handle malformed JSON', async () => {
      const response = await request(app)
        .post('/api/v1/availability')
        .set('Content-Type', 'application/json')
        .send('{ invalid json }');

      expect(response.status).toBe(400);
    });
  });
});
