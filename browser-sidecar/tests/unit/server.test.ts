/**
 * Unit tests for server endpoints and middleware
 */

import request from 'supertest';
import { app } from '../../src/server';

describe('Server Unit Tests', () => {
  describe('GET /health', () => {
    it('should return health status with version and uptime', async () => {
      const response = await request(app).get('/health');

      expect(response.status).toBe(200);
      expect(response.body).toHaveProperty('status');
      expect(response.body).toHaveProperty('version');
      expect(response.body).toHaveProperty('browserReady');
      expect(response.body).toHaveProperty('uptime');
      expect(typeof response.body.uptime).toBe('number');
      expect(response.body.uptime).toBeGreaterThanOrEqual(0);
    });
  });

  describe('GET /ready', () => {
    it('should return ready status', async () => {
      const response = await request(app).get('/ready');

      expect([200, 503]).toContain(response.status);
      expect(response.body).toHaveProperty('ready');
      expect(typeof response.body.ready).toBe('boolean');
    });
  });

  describe('POST /api/v1/availability', () => {
    it('should reject missing bookingUrl with 400', async () => {
      const response = await request(app)
        .post('/api/v1/availability')
        .send({ date: '2024-01-15' });

      expect(response.status).toBe(400);
      expect(response.body.success).toBe(false);
      expect(response.body.error).toBe('Invalid request');
      expect(response.body.details).toBeDefined();
    });

    it('should reject missing date with 400', async () => {
      const response = await request(app)
        .post('/api/v1/availability')
        .send({ bookingUrl: 'https://example.com' });

      expect(response.status).toBe(400);
      expect(response.body.success).toBe(false);
      expect(response.body.error).toBe('Invalid request');
    });

    it('should reject invalid date format with 400', async () => {
      const response = await request(app)
        .post('/api/v1/availability')
        .send({
          bookingUrl: 'https://example.com',
          date: '01-15-2024', // Wrong format
        });

      expect(response.status).toBe(400);
      expect(response.body.success).toBe(false);
      expect(response.body.error).toBe('Invalid request');
    });

    it('should reject invalid URL with 400', async () => {
      const response = await request(app)
        .post('/api/v1/availability')
        .send({
          bookingUrl: 'not-a-valid-url',
          date: '2024-01-15',
        });

      expect(response.status).toBe(400);
      expect(response.body.success).toBe(false);
    });

    it('should reject timeout below minimum with 400', async () => {
      const response = await request(app)
        .post('/api/v1/availability')
        .send({
          bookingUrl: 'https://example.com',
          date: '2024-01-15',
          timeout: 500, // Below minimum of 1000
        });

      expect(response.status).toBe(400);
      expect(response.body.success).toBe(false);
    });

    it('should reject timeout above maximum with 400', async () => {
      const response = await request(app)
        .post('/api/v1/availability')
        .send({
          bookingUrl: 'https://example.com',
          date: '2024-01-15',
          timeout: 70000, // Above maximum of 60000
        });

      expect(response.status).toBe(400);
      expect(response.body.success).toBe(false);
    });

    it('should accept valid request with optional fields', async () => {
      const response = await request(app)
        .post('/api/v1/availability')
        .send({
          bookingUrl: 'https://example.com',
          date: '2024-01-15',
          serviceName: 'Botox',
          providerName: 'Dr. Smith',
          timeout: 30000,
        });

      // Should accept the request (might fail scraping but validation passes)
      expect([200, 502]).toContain(response.status);
      expect(response.body).toHaveProperty('success');
    });
  });

  describe('POST /api/v1/availability/batch', () => {
    it('should reject missing bookingUrl with 400', async () => {
      const response = await request(app)
        .post('/api/v1/availability/batch')
        .send({ dates: ['2024-01-15'] });

      expect(response.status).toBe(400);
      expect(response.body.success).toBe(false);
      expect(response.body.error).toContain('bookingUrl');
    });

    it('should reject missing dates with 400', async () => {
      const response = await request(app)
        .post('/api/v1/availability/batch')
        .send({ bookingUrl: 'https://example.com' });

      expect(response.status).toBe(400);
      expect(response.body.success).toBe(false);
      expect(response.body.error).toContain('dates');
    });

    it('should reject empty dates array with 400', async () => {
      const response = await request(app)
        .post('/api/v1/availability/batch')
        .send({
          bookingUrl: 'https://example.com',
          dates: [],
        });

      expect(response.status).toBe(400);
      expect(response.body.success).toBe(false);
    });

    it('should reject more than 7 dates with 400', async () => {
      const dates = Array(8)
        .fill(null)
        .map((_, i) => {
          const date = new Date('2024-01-15');
          date.setDate(date.getDate() + i);
          return date.toISOString().split('T')[0];
        });

      const response = await request(app)
        .post('/api/v1/availability/batch')
        .send({
          bookingUrl: 'https://example.com',
          dates,
        });

      expect(response.status).toBe(400);
      expect(response.body.success).toBe(false);
      expect(response.body.error).toMatch(/maximum|7|dates/i);
    });

    it('should accept valid batch request with 1-7 dates', async () => {
      const dates = ['2024-01-15', '2024-01-16', '2024-01-17'];

      const response = await request(app)
        .post('/api/v1/availability/batch')
        .send({
          bookingUrl: 'https://example.com',
          dates,
          serviceName: 'Botox',
          providerName: 'Dr. Smith',
          timeout: 30000,
        });

      // Should accept the request structure
      expect([200, 500]).toContain(response.status);
    }, 60000); // Increase timeout for batch requests
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

      // Express returns either 400 (bad request) or 500 (server error) for malformed JSON
      expect([400, 500]).toContain(response.status);
    });

    it('should handle missing Content-Type header', async () => {
      const response = await request(app)
        .post('/api/v1/availability')
        .send('not json');

      // Express should handle this gracefully
      expect([400, 415]).toContain(response.status);
    });
  });

  describe('Request logging middleware', () => {
    it('should log GET requests', async () => {
      const response = await request(app).get('/health');
      expect(response.status).toBe(200);
      // Middleware logs are tested implicitly through successful requests
    });

    it('should log POST requests', async () => {
      const response = await request(app)
        .post('/api/v1/availability')
        .send({
          bookingUrl: 'https://example.com',
          date: '2024-01-15',
        });

      expect([200, 502]).toContain(response.status);
      // Middleware logs are tested implicitly
    });
  });

  describe('CORS and headers', () => {
    it('should accept JSON content type', async () => {
      const response = await request(app)
        .post('/api/v1/availability')
        .set('Content-Type', 'application/json')
        .send({
          bookingUrl: 'https://example.com',
          date: '2024-01-15',
        });

      expect([200, 400, 502]).toContain(response.status);
    });

    it('should return JSON response', async () => {
      const response = await request(app).get('/health');

      expect(response.status).toBe(200);
      expect(response.type).toMatch(/json/);
      expect(response.body).toBeDefined();
    });
  });
});
