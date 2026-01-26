import { Browser, BrowserContext, Page, chromium } from 'playwright';
import {
  AvailabilityRequest,
  AvailabilityResponse,
  TimeSlot,
  ScraperConfig,
  BookingPlatformSelectors,
  MOXIE_SELECTORS,
  ScraperError,
  TimeoutError,
  NavigationError,
} from './types';
import logger from './logger';

const DEFAULT_CONFIG: ScraperConfig = {
  headless: true,
  timeout: 30000,
  retries: 2,
  userAgent: 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36',
};

export class AvailabilityScraper {
  private browser: Browser | null = null;
  private config: ScraperConfig;
  private ready: boolean = false;

  constructor(config: Partial<ScraperConfig> = {}) {
    this.config = { ...DEFAULT_CONFIG, ...config };
  }

  async initialize(): Promise<void> {
    if (this.browser) {
      return;
    }

    logger.info('Initializing browser...');

    this.browser = await chromium.launch({
      headless: this.config.headless,
      args: [
        '--disable-blink-features=AutomationControlled',
        '--disable-dev-shm-usage',
        '--no-sandbox',
        '--disable-setuid-sandbox',
        '--disable-gpu',
        '--disable-web-security',
        '--disable-features=IsolateOrigins,site-per-process',
      ],
    });

    this.ready = true;
    logger.info('Browser initialized successfully');
  }

  async close(): Promise<void> {
    if (this.browser) {
      await this.browser.close();
      this.browser = null;
      this.ready = false;
      logger.info('Browser closed');
    }
  }

  isReady(): boolean {
    return this.ready && this.browser !== null;
  }

  async scrapeAvailability(request: AvailabilityRequest): Promise<AvailabilityResponse> {
    if (!this.browser) {
      throw new ScraperError('Browser not initialized', 'NOT_INITIALIZED');
    }

    const startTime = Date.now();
    let context: BrowserContext | null = null;
    let page: Page | null = null;
    let lastError: Error | null = null;

    for (let attempt = 0; attempt <= this.config.retries; attempt++) {
      try {
        if (attempt > 0) {
          logger.info(`Retry attempt ${attempt} of ${this.config.retries}`);
          await this.delay(1000 * attempt); // Exponential backoff
        }

        context = await this.browser.newContext({
          userAgent: this.config.userAgent,
          viewport: { width: 1920, height: 1080 },
          locale: 'en-US',
          timezoneId: 'America/New_York',
        });

        page = await context.newPage();

        // Add stealth measures
        await this.applyStealthMeasures(page);

        logger.info(`Navigating to ${request.bookingUrl}`, { date: request.date });

        const response = await page.goto(request.bookingUrl, {
          timeout: request.timeout,
          waitUntil: 'networkidle',
        });

        if (!response || !response.ok()) {
          throw new NavigationError(`Failed to load page: ${response?.status() || 'unknown'}`);
        }

        // Wait for page to be interactive
        await page.waitForLoadState('domcontentloaded');
        await this.delay(2000); // Allow dynamic content to load

        // Detect the booking platform
        const selectors = await this.detectPlatform(page);

        // Navigate to the requested date
        await this.navigateToDate(page, request.date, selectors);

        // Extract available time slots
        const slots = await this.extractTimeSlots(page, selectors);

        const duration = Date.now() - startTime;
        logger.info(`Successfully scraped ${slots.length} slots in ${duration}ms`);

        return {
          success: true,
          bookingUrl: request.bookingUrl,
          date: request.date,
          slots,
          provider: request.providerName,
          service: request.serviceName,
          scrapedAt: new Date().toISOString(),
        };

      } catch (error) {
        lastError = error as Error;
        logger.warn(`Scrape attempt ${attempt + 1} failed`, {
          error: lastError.message,
          url: request.bookingUrl,
        });

        if (error instanceof ScraperError && !error.retryable) {
          break; // Don't retry non-retryable errors
        }
      } finally {
        if (page) await page.close().catch(() => {});
        if (context) await context.close().catch(() => {});
      }
    }

    // All retries failed
    const duration = Date.now() - startTime;
    logger.error(`Failed to scrape after ${this.config.retries + 1} attempts`, {
      error: lastError?.message,
      duration,
    });

    return {
      success: false,
      bookingUrl: request.bookingUrl,
      date: request.date,
      slots: [],
      scrapedAt: new Date().toISOString(),
      error: lastError?.message || 'Unknown error',
    };
  }

  private async applyStealthMeasures(page: Page): Promise<void> {
    // Override navigator.webdriver
    await page.addInitScript(() => {
      Object.defineProperty(navigator, 'webdriver', { get: () => false });

      // Override plugins
      Object.defineProperty(navigator, 'plugins', {
        get: () => [1, 2, 3, 4, 5],
      });

      // Override languages
      Object.defineProperty(navigator, 'languages', {
        get: () => ['en-US', 'en'],
      });

      // Override platform
      Object.defineProperty(navigator, 'platform', {
        get: () => 'Win32',
      });
    });
  }

  private async detectPlatform(page: Page): Promise<BookingPlatformSelectors> {
    // Try to detect Moxie
    const moxieIndicators = [
      'moxie',
      'withmoxie',
      'joinmoxie',
    ];

    const pageUrl = page.url().toLowerCase();
    const pageContent = await page.content();

    for (const indicator of moxieIndicators) {
      if (pageUrl.includes(indicator) || pageContent.toLowerCase().includes(indicator)) {
        logger.info('Detected Moxie booking platform');
        return MOXIE_SELECTORS;
      }
    }

    // Default to generic selectors that work with most platforms
    logger.info('Using generic booking selectors');
    return {
      platform: 'generic',
      dateSelector: 'input[type="date"], .date-picker, [data-date]',
      timeSlotSelector: '.time-slot, .appointment-slot, [data-time], button[class*="time"]',
      availableSlotClass: 'available',
      unavailableSlotClass: 'unavailable',
    };
  }

  private async navigateToDate(
    page: Page,
    targetDate: string,
    selectors: BookingPlatformSelectors
  ): Promise<void> {
    logger.info(`Navigating to date: ${targetDate}`);

    // Try to find and interact with date picker
    const dateInput = await page.$(selectors.dateSelector);

    if (dateInput) {
      // Try to fill the date directly
      try {
        await dateInput.fill(targetDate);
        await this.delay(1000);
        return;
      } catch {
        // Fall through to click-based navigation
      }
    }

    // Try clicking date navigation buttons
    const targetDateObj = new Date(targetDate);
    const today = new Date();
    today.setHours(0, 0, 0, 0);

    const daysDiff = Math.floor((targetDateObj.getTime() - today.getTime()) / (1000 * 60 * 60 * 24));

    if (daysDiff > 0 && selectors.nextDayButton) {
      for (let i = 0; i < daysDiff && i < 30; i++) {
        const nextBtn = await page.$(selectors.nextDayButton);
        if (nextBtn) {
          await nextBtn.click();
          await this.delay(500);
        } else {
          break;
        }
      }
    }

    await this.delay(1000); // Wait for slots to update
  }

  private async extractTimeSlots(
    page: Page,
    selectors: BookingPlatformSelectors
  ): Promise<TimeSlot[]> {
    const slots: TimeSlot[] = [];

    // Wait for time slots to appear
    try {
      await page.waitForSelector(selectors.timeSlotSelector, { timeout: 10000 });
    } catch {
      logger.warn('No time slots found on page');
      return slots;
    }

    // Extract all time slot elements
    const slotElements = await page.$$(selectors.timeSlotSelector);
    logger.info(`Found ${slotElements.length} slot elements`);

    for (const element of slotElements) {
      try {
        const text = await element.textContent();
        if (!text) continue;

        // Extract time from text (handles formats like "10:00 AM", "10:00", "10 AM")
        const timeMatch = text.match(/\d{1,2}:\d{2}\s*(AM|PM)?|\d{1,2}\s*(AM|PM)/i);
        if (!timeMatch) continue;

        const time = timeMatch[0].trim();

        // Check if available
        const classList = await element.getAttribute('class') || '';
        const isDisabled = await element.getAttribute('disabled');
        const ariaDisabled = await element.getAttribute('aria-disabled');

        const available =
          !isDisabled &&
          ariaDisabled !== 'true' &&
          !classList.includes(selectors.unavailableSlotClass || 'unavailable') &&
          !classList.includes('disabled') &&
          !classList.includes('booked');

        slots.push({
          time,
          available,
        });
      } catch (error) {
        logger.debug('Failed to extract slot', { error });
      }
    }

    // Sort slots by time
    slots.sort((a, b) => {
      const timeA = this.parseTime(a.time);
      const timeB = this.parseTime(b.time);
      return timeA - timeB;
    });

    return slots;
  }

  private parseTime(timeStr: string): number {
    const match = timeStr.match(/(\d{1,2}):?(\d{2})?\s*(AM|PM)?/i);
    if (!match) return 0;

    let hours = parseInt(match[1], 10);
    const minutes = parseInt(match[2] || '0', 10);
    const meridiem = match[3]?.toUpperCase();

    if (meridiem === 'PM' && hours !== 12) hours += 12;
    if (meridiem === 'AM' && hours === 12) hours = 0;

    return hours * 60 + minutes;
  }

  private delay(ms: number): Promise<void> {
    return new Promise(resolve => setTimeout(resolve, ms));
  }
}

// Singleton instance for the service
let scraperInstance: AvailabilityScraper | null = null;

export async function getScraper(): Promise<AvailabilityScraper> {
  if (!scraperInstance) {
    scraperInstance = new AvailabilityScraper({
      headless: process.env.HEADLESS !== 'false',
    });
    await scraperInstance.initialize();
  }
  return scraperInstance;
}

export async function closeScraper(): Promise<void> {
  if (scraperInstance) {
    await scraperInstance.close();
    scraperInstance = null;
  }
}
