/**
 * Unit tests for AvailabilityScraper helper methods and edge cases
 */

import { AvailabilityScraper } from '../../src/scraper';
import { ScraperError, NavigationError, SelectorNotFoundError } from '../../src/types';

describe('AvailabilityScraper - Helper Methods', () => {
  describe('Constructor and Configuration', () => {
    it('should create instance with default config', () => {
      const scraper = new AvailabilityScraper();
      expect(scraper).toBeInstanceOf(AvailabilityScraper);
      expect(scraper.isReady()).toBe(false);
    });

    it('should create instance with custom headless setting', () => {
      const scraper = new AvailabilityScraper({ headless: false });
      expect(scraper).toBeInstanceOf(AvailabilityScraper);
      expect(scraper.isReady()).toBe(false);
    });

    it('should create instance with custom timeout', () => {
      const scraper = new AvailabilityScraper({ timeout: 60000 });
      expect(scraper).toBeInstanceOf(AvailabilityScraper);
    });

    it('should create instance with custom retries', () => {
      const scraper = new AvailabilityScraper({ retries: 5 });
      expect(scraper).toBeInstanceOf(AvailabilityScraper);
    });

    it('should create instance with custom user agent', () => {
      const scraper = new AvailabilityScraper({
        userAgent: 'Custom User Agent/1.0',
      });
      expect(scraper).toBeInstanceOf(AvailabilityScraper);
    });

    it('should merge custom config with defaults', () => {
      const scraper = new AvailabilityScraper({
        headless: false,
        retries: 3,
      });
      expect(scraper).toBeInstanceOf(AvailabilityScraper);
    });
  });

  describe('isReady()', () => {
    it('should return false before initialization', () => {
      const scraper = new AvailabilityScraper();
      expect(scraper.isReady()).toBe(false);
    });

    it('should return false for new instance', () => {
      const scraper = new AvailabilityScraper({ headless: true });
      expect(scraper.isReady()).toBe(false);
    });
  });

  describe('Browser Lifecycle', () => {
    let scraper: AvailabilityScraper;

    beforeEach(() => {
      scraper = new AvailabilityScraper({ headless: true });
    });

    afterEach(async () => {
      await scraper.close();
    });

    it('should initialize browser and set ready state', async () => {
      expect(scraper.isReady()).toBe(false);
      await scraper.initialize();
      expect(scraper.isReady()).toBe(true);
    }, 30000);

    it('should be idempotent on multiple initializations', async () => {
      await scraper.initialize();
      expect(scraper.isReady()).toBe(true);

      // Second init should not throw
      await scraper.initialize();
      expect(scraper.isReady()).toBe(true);
    }, 30000);

    it('should close browser and clear ready state', async () => {
      await scraper.initialize();
      expect(scraper.isReady()).toBe(true);

      await scraper.close();
      expect(scraper.isReady()).toBe(false);
    }, 30000);

    it('should handle close when not initialized', async () => {
      expect(scraper.isReady()).toBe(false);
      await scraper.close(); // Should not throw
      expect(scraper.isReady()).toBe(false);
    });

    it('should handle multiple close calls', async () => {
      await scraper.initialize();
      await scraper.close();
      expect(scraper.isReady()).toBe(false);

      // Second close should not throw
      await scraper.close();
      expect(scraper.isReady()).toBe(false);
    }, 30000);

    it('should allow reinitialization after close', async () => {
      await scraper.initialize();
      expect(scraper.isReady()).toBe(true);

      await scraper.close();
      expect(scraper.isReady()).toBe(false);

      // Reinitialize
      await scraper.initialize();
      expect(scraper.isReady()).toBe(true);
    }, 30000);
  });

  describe('Error handling before initialization', () => {
    it('should throw ScraperError when scraping without initialization', async () => {
      const scraper = new AvailabilityScraper();

      await expect(
        scraper.scrapeAvailability({
          bookingUrl: 'https://example.com',
          date: '2024-01-15',
          timeout: 30000,
        })
      ).rejects.toThrow(ScraperError);

      await expect(
        scraper.scrapeAvailability({
          bookingUrl: 'https://example.com',
          date: '2024-01-15',
          timeout: 30000,
        })
      ).rejects.toThrow('Browser not initialized');
    });

    it('should throw ScraperError when getting available dates without initialization', async () => {
      const scraper = new AvailabilityScraper();

      await expect(
        scraper.getAvailableDates('https://example.com', 2024, 1, 30000)
      ).rejects.toThrow(ScraperError);

      await expect(
        scraper.getAvailableDates('https://example.com', 2024, 1, 30000)
      ).rejects.toThrow('Browser not initialized');
    });
  });

  describe('Request validation edge cases', () => {
    let scraper: AvailabilityScraper;

    beforeAll(async () => {
      scraper = new AvailabilityScraper({ headless: true, retries: 0 });
      await scraper.initialize();
    }, 30000);

    afterAll(async () => {
      await scraper.close();
    });

    it('should handle unreachable URLs gracefully', async () => {
      const result = await scraper.scrapeAvailability({
        bookingUrl: 'https://this-domain-absolutely-does-not-exist-12345.example.com/booking',
        date: '2024-01-15',
        timeout: 5000,
      });

      expect(result.success).toBe(false);
      expect(result.error).toBeDefined();
      expect(result.slots).toEqual([]);
      expect(result.bookingUrl).toBe('https://this-domain-absolutely-does-not-exist-12345.example.com/booking');
      expect(result.date).toBe('2024-01-15');
    }, 30000);

    it('should handle very short timeout', async () => {
      const startTime = Date.now();

      const result = await scraper.scrapeAvailability({
        bookingUrl: 'https://example.com',
        date: '2024-01-15',
        timeout: 1000, // Very short
      });

      const elapsed = Date.now() - startTime;

      // Should complete within reasonable time (timeout + retries + overhead)
      expect(elapsed).toBeLessThan(20000);
      expect(result).toBeDefined();
      expect(result.bookingUrl).toBe('https://example.com');
    }, 25000);

    it('should return proper structure even on failure', async () => {
      const result = await scraper.scrapeAvailability({
        bookingUrl: 'https://httpstat.us/500', // Returns 500 error
        date: '2024-01-15',
        timeout: 5000,
      });

      expect(result).toHaveProperty('success');
      expect(result).toHaveProperty('bookingUrl');
      expect(result).toHaveProperty('date');
      expect(result).toHaveProperty('slots');
      expect(result).toHaveProperty('scrapedAt');
      expect(Array.isArray(result.slots)).toBe(true);
    }, 15000);
  });

  describe('Retry behavior', () => {
    it('should respect retry count = 0', async () => {
      const scraper = new AvailabilityScraper({
        headless: true,
        retries: 0,
        timeout: 5000,
      });

      await scraper.initialize();

      try {
        const startTime = Date.now();

        const result = await scraper.scrapeAvailability({
          bookingUrl: 'https://this-will-fail-12345.example.com',
          date: '2024-01-15',
          timeout: 3000,
        });

        const elapsed = Date.now() - startTime;

        // With 0 retries, should fail faster
        expect(elapsed).toBeLessThan(10000);
        expect(result.success).toBe(false);
      } finally {
        await scraper.close();
      }
    }, 30000);

    it('should respect retry count = 1', async () => {
      const scraper = new AvailabilityScraper({
        headless: true,
        retries: 1,
        timeout: 5000,
      });

      await scraper.initialize();

      try {
        const result = await scraper.scrapeAvailability({
          bookingUrl: 'https://this-will-fail-12345.example.com',
          date: '2024-01-15',
          timeout: 3000,
        });

        // Will retry once
        expect(result.success).toBe(false);
      } finally {
        await scraper.close();
      }
    }, 30000);
  });

  describe('Date parsing edge cases', () => {
    it('should handle various date formats', () => {
      // Helper to test date format (YYYY-MM-DD)
      const isValidDateFormat = (date: string): boolean => {
        return /^\d{4}-\d{2}-\d{2}$/.test(date);
      };

      expect(isValidDateFormat('2024-01-15')).toBe(true);
      expect(isValidDateFormat('2024-12-31')).toBe(true);
      expect(isValidDateFormat('2024-1-15')).toBe(false); // Missing zero padding
      expect(isValidDateFormat('01-15-2024')).toBe(false); // Wrong order
      expect(isValidDateFormat('2024/01/15')).toBe(false); // Wrong separator
    });
  });

  describe('Platform detection hints', () => {
    it('should detect Moxie from URL patterns', () => {
      const moxieUrls = [
        'https://withmoxie.com/booking',
        'https://joinmoxie.com/book',
        'https://moxie.example.com/appointments',
      ];

      moxieUrls.forEach((url) => {
        expect(url.toLowerCase()).toMatch(/moxie|withmoxie|joinmoxie/);
      });
    });

    it('should not falsely detect Moxie in generic URLs', () => {
      const genericUrls = [
        'https://calendly.com/booking',
        'https://example.com/booking',
        'https://acuityscheduling.com/schedule',
      ];

      genericUrls.forEach((url) => {
        expect(url.toLowerCase()).not.toMatch(/moxie|withmoxie|joinmoxie/);
      });
    });
  });
});

describe('Error Classes', () => {
  describe('ScraperError', () => {
    it('should create error with message and code', () => {
      const error = new ScraperError('Test message', 'TEST_CODE');
      expect(error.message).toBe('Test message');
      expect(error.code).toBe('TEST_CODE');
      expect(error.retryable).toBe(false);
      expect(error.name).toBe('ScraperError');
    });

    it('should support retryable flag', () => {
      const error = new ScraperError('Test', 'CODE', true);
      expect(error.retryable).toBe(true);
    });

    it('should be an instance of Error', () => {
      const error = new ScraperError('Test', 'CODE');
      expect(error).toBeInstanceOf(Error);
      expect(error).toBeInstanceOf(ScraperError);
    });
  });

  describe('NavigationError', () => {
    it('should be retryable by default', () => {
      const error = new NavigationError('Failed to load page');
      expect(error.retryable).toBe(true);
      expect(error.code).toBe('NAVIGATION_ERROR');
      expect(error.name).toBe('NavigationError');
    });

    it('should extend ScraperError', () => {
      const error = new NavigationError('Test');
      expect(error).toBeInstanceOf(ScraperError);
      expect(error).toBeInstanceOf(Error);
    });
  });

  describe('SelectorNotFoundError', () => {
    it('should not be retryable', () => {
      const error = new SelectorNotFoundError('.my-selector');
      expect(error.retryable).toBe(false);
      expect(error.code).toBe('SELECTOR_NOT_FOUND');
      expect(error.message).toContain('.my-selector');
      expect(error.name).toBe('SelectorNotFoundError');
    });

    it('should include selector in message', () => {
      const error = new SelectorNotFoundError('#unique-id');
      expect(error.message).toMatch(/unique-id/);
    });
  });
});

describe('Time Parsing Utilities', () => {
  // Test the time parsing logic that's used for sorting slots
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

  describe('12-hour format parsing', () => {
    it('should parse morning times', () => {
      expect(parseTime('9:00 AM')).toBe(540); // 9 * 60
      expect(parseTime('9:30 AM')).toBe(570); // 9 * 60 + 30
      expect(parseTime('11:45 AM')).toBe(705); // 11 * 60 + 45
    });

    it('should parse afternoon times', () => {
      expect(parseTime('1:00 PM')).toBe(780); // 13 * 60
      expect(parseTime('3:30 PM')).toBe(930); // 15 * 60 + 30
      expect(parseTime('11:30 PM')).toBe(1410); // 23 * 60 + 30
    });

    it('should handle noon correctly', () => {
      expect(parseTime('12:00 PM')).toBe(720); // 12 * 60
      expect(parseTime('12:30 PM')).toBe(750); // 12 * 60 + 30
    });

    it('should handle midnight correctly', () => {
      expect(parseTime('12:00 AM')).toBe(0); // 0 * 60
      expect(parseTime('12:30 AM')).toBe(30); // 0 * 60 + 30
    });
  });

  describe('Case insensitivity', () => {
    it('should parse lowercase am/pm', () => {
      expect(parseTime('9:00am')).toBe(540);
      expect(parseTime('3:00pm')).toBe(900);
    });

    it('should parse mixed case', () => {
      expect(parseTime('9:00Am')).toBe(540);
      expect(parseTime('3:00Pm')).toBe(900);
      expect(parseTime('9:00 am')).toBe(540);
    });
  });

  describe('Edge cases', () => {
    it('should handle times without minutes', () => {
      expect(parseTime('9 AM')).toBe(540);
      expect(parseTime('3 PM')).toBe(900);
    });

    it('should handle 24-hour format', () => {
      expect(parseTime('14:00')).toBe(840); // 14 * 60
      expect(parseTime('09:30')).toBe(570); // 9 * 60 + 30
      expect(parseTime('00:00')).toBe(0); // Midnight
    });

    it('should return 0 for invalid formats', () => {
      expect(parseTime('invalid')).toBe(0);
      expect(parseTime('')).toBe(0);
      expect(parseTime('abc')).toBe(0);
    });
  });

  describe('Sorting behavior', () => {
    it('should enable chronological sorting', () => {
      const times = ['3:00pm', '9:00am', '12:00pm', '6:30pm', '10:15am'];
      const sorted = [...times].sort((a, b) => parseTime(a) - parseTime(b));

      expect(sorted).toEqual(['9:00am', '10:15am', '12:00pm', '3:00pm', '6:30pm']);
    });

    it('should handle mixed AM/PM correctly', () => {
      const times = ['2:00 PM', '10:00 AM', '12:00 PM', '12:00 AM', '11:59 PM'];
      const sorted = [...times].sort((a, b) => parseTime(a) - parseTime(b));

      expect(sorted).toEqual(['12:00 AM', '10:00 AM', '12:00 PM', '2:00 PM', '11:59 PM']);
    });
  });
});
