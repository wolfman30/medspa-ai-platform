import {
  AvailabilityRequestSchema,
  ScraperError,
  TimeoutError,
  NavigationError,
  SelectorNotFoundError,
  MOXIE_SELECTORS,
} from '../../src/types';

describe('AvailabilityRequestSchema', () => {
  describe('valid requests', () => {
    it('should accept a valid request with all fields', () => {
      const request = {
        bookingUrl: 'https://example.com/booking',
        date: '2024-01-15',
        serviceName: 'Botox',
        providerName: 'Dr. Smith',
        timeout: 30000,
      };

      const result = AvailabilityRequestSchema.parse(request);
      expect(result.bookingUrl).toBe(request.bookingUrl);
      expect(result.date).toBe(request.date);
      expect(result.serviceName).toBe(request.serviceName);
      expect(result.providerName).toBe(request.providerName);
      expect(result.timeout).toBe(request.timeout);
    });

    it('should accept a minimal valid request', () => {
      const request = {
        bookingUrl: 'https://example.com/booking',
        date: '2024-01-15',
      };

      const result = AvailabilityRequestSchema.parse(request);
      expect(result.bookingUrl).toBe(request.bookingUrl);
      expect(result.date).toBe(request.date);
      expect(result.timeout).toBe(30000); // default value
    });

    it('should accept timeout at minimum bound', () => {
      const request = {
        bookingUrl: 'https://example.com/booking',
        date: '2024-01-15',
        timeout: 1000,
      };

      const result = AvailabilityRequestSchema.parse(request);
      expect(result.timeout).toBe(1000);
    });

    it('should accept timeout at maximum bound', () => {
      const request = {
        bookingUrl: 'https://example.com/booking',
        date: '2024-01-15',
        timeout: 60000,
      };

      const result = AvailabilityRequestSchema.parse(request);
      expect(result.timeout).toBe(60000);
    });
  });

  describe('invalid requests', () => {
    it('should reject invalid URL', () => {
      const request = {
        bookingUrl: 'not-a-valid-url',
        date: '2024-01-15',
      };

      expect(() => AvailabilityRequestSchema.parse(request)).toThrow();
    });

    it('should reject invalid date format', () => {
      const request = {
        bookingUrl: 'https://example.com/booking',
        date: '01-15-2024', // Wrong format
      };

      expect(() => AvailabilityRequestSchema.parse(request)).toThrow();
    });

    it('should reject invalid date format (no dashes)', () => {
      const request = {
        bookingUrl: 'https://example.com/booking',
        date: '20240115',
      };

      expect(() => AvailabilityRequestSchema.parse(request)).toThrow();
    });

    it('should reject timeout below minimum', () => {
      const request = {
        bookingUrl: 'https://example.com/booking',
        date: '2024-01-15',
        timeout: 500,
      };

      expect(() => AvailabilityRequestSchema.parse(request)).toThrow();
    });

    it('should reject timeout above maximum', () => {
      const request = {
        bookingUrl: 'https://example.com/booking',
        date: '2024-01-15',
        timeout: 70000,
      };

      expect(() => AvailabilityRequestSchema.parse(request)).toThrow();
    });

    it('should reject missing bookingUrl', () => {
      const request = {
        date: '2024-01-15',
      };

      expect(() => AvailabilityRequestSchema.parse(request)).toThrow();
    });

    it('should reject missing date', () => {
      const request = {
        bookingUrl: 'https://example.com/booking',
      };

      expect(() => AvailabilityRequestSchema.parse(request)).toThrow();
    });
  });
});

describe('Error classes', () => {
  describe('ScraperError', () => {
    it('should create error with correct properties', () => {
      const error = new ScraperError('Test error', 'TEST_CODE', true);

      expect(error.message).toBe('Test error');
      expect(error.code).toBe('TEST_CODE');
      expect(error.retryable).toBe(true);
      expect(error.name).toBe('ScraperError');
    });

    it('should default retryable to false', () => {
      const error = new ScraperError('Test error', 'TEST_CODE');

      expect(error.retryable).toBe(false);
    });
  });

  describe('TimeoutError', () => {
    it('should be retryable', () => {
      const error = new TimeoutError('Request timed out');

      expect(error.retryable).toBe(true);
      expect(error.code).toBe('TIMEOUT');
      expect(error.name).toBe('TimeoutError');
    });
  });

  describe('NavigationError', () => {
    it('should be retryable', () => {
      const error = new NavigationError('Failed to navigate');

      expect(error.retryable).toBe(true);
      expect(error.code).toBe('NAVIGATION_ERROR');
      expect(error.name).toBe('NavigationError');
    });
  });

  describe('SelectorNotFoundError', () => {
    it('should not be retryable', () => {
      const error = new SelectorNotFoundError('.missing-selector');

      expect(error.retryable).toBe(false);
      expect(error.code).toBe('SELECTOR_NOT_FOUND');
      expect(error.message).toContain('.missing-selector');
      expect(error.name).toBe('SelectorNotFoundError');
    });
  });
});

describe('MOXIE_SELECTORS', () => {
  it('should have required selector properties', () => {
    expect(MOXIE_SELECTORS.platform).toBe('moxie');
    expect(MOXIE_SELECTORS.dateSelector).toBeDefined();
    expect(MOXIE_SELECTORS.timeSlotSelector).toBeDefined();
    expect(MOXIE_SELECTORS.availableSlotClass).toBeDefined();
  });

  it('should have optional navigation selectors', () => {
    expect(MOXIE_SELECTORS.nextDayButton).toBeDefined();
    expect(MOXIE_SELECTORS.prevDayButton).toBeDefined();
  });
});
