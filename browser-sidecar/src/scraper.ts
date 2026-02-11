import { Browser, BrowserContext, Page, chromium } from 'playwright';
import * as fs from 'fs';
import * as path from 'path';
import {
  AvailabilityRequest,
  AvailabilityResponse,
  CalendarSlotsRequest,
  CalendarSlotsResponse,
  CalendarSlotResult,
  TimeSlot,
  ScraperConfig,
  BookingPlatformSelectors,
  MOXIE_SELECTORS,
  ScraperError,
  NavigationError,
} from './types';
import logger from './logger';

// Debug mode for saving screenshots
const DEBUG_MODE = process.env.DEBUG_SCRAPER === 'true';
const DEBUG_DIR = path.join(process.cwd(), 'debug-screenshots');

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
          await this.delay(1000 * attempt);
        }

        context = await this.browser.newContext({
          userAgent: this.config.userAgent,
          viewport: { width: 1920, height: 1080 },
          locale: 'en-US',
          timezoneId: 'America/New_York',
        });

        page = await context.newPage();
        await this.applyStealthMeasures(page);

        logger.info(`Navigating to ${request.bookingUrl}`, { date: request.date });

        const response = await page.goto(request.bookingUrl, {
          timeout: request.timeout,
          waitUntil: 'domcontentloaded',
        });

        if (!response || !response.ok()) {
          throw new NavigationError(`Failed to load page: ${response?.status() || 'unknown'}`);
        }

        await this.delay(3000);

        const selectors = await this.detectPlatform(page);

        // For Moxie, handle the multi-step booking flow and get available providers
        let detectedProviders: string[] = [];
        if (selectors.platform === 'moxie') {
          detectedProviders = await this.handleMoxieServiceSelection(page, request.serviceName);
        }

        await this.navigateToDate(page, request.date, selectors);
        const slots = await this.extractTimeSlots(page, selectors);

        const duration = Date.now() - startTime;
        logger.info(`Successfully scraped ${slots.length} slots in ${duration}ms`, {
          providers: detectedProviders.length,
        });

        return {
          success: true,
          bookingUrl: request.bookingUrl,
          date: request.date,
          slots,
          provider: request.providerName,
          service: request.serviceName,
          providers: detectedProviders.length > 0 ? detectedProviders : undefined,
          scrapedAt: new Date().toISOString(),
        };

      } catch (error) {
        lastError = error as Error;
        logger.warn(`Scrape attempt ${attempt + 1} failed`, {
          error: lastError.message,
          url: request.bookingUrl,
        });

        if (error instanceof ScraperError && !error.retryable) {
          break;
        }
      } finally {
        if (page) await page.close().catch(() => {});
        if (context) await context.close().catch(() => {});
      }
    }

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

  /**
   * Get all available dates from the booking calendar for a given month.
   * This method autonomously detects which calendar days have availability.
   */
  async getAvailableDates(
    bookingUrl: string,
    year: number,
    month: number, // 1-indexed (1 = January, 2 = February, etc.)
    timeout: number = 60000,
    serviceName?: string
  ): Promise<{ success: boolean; dates: string[]; providers?: string[]; error?: string }> {
    if (!this.browser) {
      throw new ScraperError('Browser not initialized', 'NOT_INITIALIZED');
    }

    const startTime = Date.now();
    let context: BrowserContext | null = null;
    let page: Page | null = null;

    try {
      context = await this.browser.newContext({
        userAgent: this.config.userAgent,
        viewport: { width: 1920, height: 1080 },
        locale: 'en-US',
        timezoneId: 'America/New_York',
      });

      page = await context.newPage();
      await this.applyStealthMeasures(page);

      logger.info(`Getting available dates from ${bookingUrl} for ${year}-${month}`);

      const response = await page.goto(bookingUrl, {
        timeout,
        waitUntil: 'domcontentloaded',
      });

      if (!response || !response.ok()) {
        throw new NavigationError(`Failed to load page: ${response?.status() || 'unknown'}`);
      }

      await page.waitForLoadState('domcontentloaded');
      await this.delay(2000);

      const selectors = await this.detectPlatform(page);

      // Navigate through service selection to get to calendar and detect providers
      let detectedProviders: string[] = [];
      if (selectors.platform === 'moxie') {
        detectedProviders = await this.handleMoxieServiceSelection(page, serviceName);
      }

      await this.saveDebugScreenshot(page, 'available-dates-calendar');

      // Navigate to the target month if needed
      const monthNames = ['January', 'February', 'March', 'April', 'May', 'June',
        'July', 'August', 'September', 'October', 'November', 'December'];
      const targetMonthName = monthNames[month - 1];
      const targetMonthStr = `${targetMonthName} ${year}`;

      // Check current month and navigate if needed
      let attempts = 0;
      while (attempts < 12) {
        const currentMonth = await page.evaluate(() => {
          const bodyText = document.body.innerText;
          const monthMatch = bodyText.match(/(January|February|March|April|May|June|July|August|September|October|November|December)\s+(\d{4})/i);
          return monthMatch ? `${monthMatch[1]} ${monthMatch[2]}` : '';
        });

        if (currentMonth.toLowerCase() === targetMonthStr.toLowerCase()) {
          logger.info(`On target month: ${targetMonthStr}`);
          break;
        }

        // Click next month
        const clicked = await page.evaluate(() => {
          const buttons = Array.from(document.querySelectorAll('button, [role="button"]'));
          for (const btn of buttons) {
            const text = btn.textContent?.trim();
            const label = btn.getAttribute('aria-label')?.toLowerCase() || '';
            if (text === '>' || text === '›' || text === '→' ||
                label.includes('next') || label.includes('forward')) {
              (btn as HTMLElement).click();
              return true;
            }
          }
          return false;
        });

        if (!clicked) break;
        await this.delay(1500);
        attempts++;
      }

      await this.delay(1000);
      await this.saveDebugScreenshot(page, 'available-dates-on-month');

      // Extract available dates from the calendar
      const availableDays = await page.evaluate(() => {
        const available: number[] = [];
        const gridCells = Array.from(document.querySelectorAll('[role="gridcell"]'));

        for (const cell of gridCells) {
          const text = cell.textContent?.trim();
          const dayNum = parseInt(text || '', 10);

          if (isNaN(dayNum) || dayNum < 1 || dayNum > 31) continue;

          const htmlCell = cell as HTMLElement;

          // Check if this day is available (not disabled)
          const isDisabled = htmlCell.hasAttribute('disabled') ||
                            htmlCell.getAttribute('aria-disabled') === 'true' ||
                            htmlCell.classList.contains('disabled');

          // Check computed styles for visual indicators
          const style = window.getComputedStyle(htmlCell);
          const opacity = parseFloat(style.opacity);
          const pointerEvents = style.pointerEvents;

          // Also check the button inside the cell
          const button = htmlCell.querySelector('button');
          let buttonClickable = true;
          if (button) {
            buttonClickable = !button.hasAttribute('disabled') &&
                             button.getAttribute('aria-disabled') !== 'true';
          }

          const isAvailable = !isDisabled &&
                             buttonClickable &&
                             opacity > 0.5 &&
                             pointerEvents !== 'none';

          if (isAvailable) {
            available.push(dayNum);
          }
        }

        return available.sort((a, b) => a - b);
      });

      logger.info(`Found ${availableDays.length} available days: ${availableDays.join(', ')}`);

      // Format as YYYY-MM-DD strings
      const dates = availableDays.map(day => {
        const mm = String(month).padStart(2, '0');
        const dd = String(day).padStart(2, '0');
        return `${year}-${mm}-${dd}`;
      });

      const duration = Date.now() - startTime;
      logger.info(`Got available dates in ${duration}ms`, {
        providers: detectedProviders.length,
      });

      return {
        success: true,
        dates,
        providers: detectedProviders.length > 0 ? detectedProviders : undefined,
      };

    } catch (error) {
      logger.error('Failed to get available dates', { error: (error as Error).message });
      return {
        success: false,
        dates: [],
        error: (error as Error).message,
      };
    } finally {
      if (page) await page.close().catch(() => {});
      if (context) await context.close().catch(() => {});
    }
  }

  /**
   * Smart calendar search: opens ONE session, navigates service selection ONCE,
   * then scans months clicking through available dates and extracting time slots.
   */
  async scrapeCalendarSlots(request: CalendarSlotsRequest): Promise<CalendarSlotsResponse> {
    if (!this.browser) {
      throw new ScraperError('Browser not initialized', 'NOT_INITIALIZED');
    }

    const startTime = Date.now();
    let context: BrowserContext | null = null;
    let page: Page | null = null;
    const results: CalendarSlotResult[] = [];
    let totalDatesScanned = 0;
    let totalDatesWithSlots = 0;
    let detectedProviders: string[] = [];

    try {
      context = await this.browser.newContext({
        userAgent: this.config.userAgent,
        viewport: { width: 1920, height: 1080 },
        locale: 'en-US',
        timezoneId: 'America/New_York',
      });

      page = await context.newPage();
      await this.applyStealthMeasures(page);

      logger.info(`Smart search: navigating to ${request.bookingUrl}`);

      const response = await page.goto(request.bookingUrl, {
        timeout: request.timeout,
        waitUntil: 'domcontentloaded',
      });

      if (!response || !response.ok()) {
        throw new NavigationError(`Failed to load page: ${response?.status() || 'unknown'}`);
      }

      await page.waitForLoadState('domcontentloaded');
      await this.delay(2000);

      const selectors = await this.detectPlatform(page);

      // Handle service selection ONCE
      if (selectors.platform === 'moxie') {
        detectedProviders = await this.handleMoxieServiceSelection(page, request.serviceName);
      }

      // Now on the calendar — scan month by month
      const maxMonths = request.maxMonths || 3;
      const maxSlots = request.maxSlots || 6;
      let collectedSlots = 0;

      for (let monthIdx = 0; monthIdx < maxMonths; monthIdx++) {
        // Check timeout
        if (Date.now() - startTime > request.timeout - 5000) {
          logger.info('Smart search: approaching timeout, stopping');
          break;
        }

        // Navigate to next month (skip for first month — we're already on it)
        if (monthIdx > 0) {
          const clicked = await page.evaluate(() => {
            const buttons = Array.from(document.querySelectorAll('button, [role="button"], [class*="arrow"], [class*="nav"]'));
            for (const btn of buttons) {
              const text = btn.textContent?.trim();
              const label = btn.getAttribute('aria-label')?.toLowerCase() || '';
              if (text === '>' || text === '›' || text === '→' ||
                  label.includes('next') || label.includes('forward')) {
                (btn as HTMLElement).click();
                return true;
              }
            }
            return false;
          });

          if (!clicked) {
            logger.warn('Smart search: could not navigate to next month');
            break;
          }
          await this.delay(1500);
        }

        // Read the current month/year from calendar header
        const currentMonth = await page.evaluate(() => {
          const bodyText = document.body.innerText;
          const monthMatch = bodyText.match(/(January|February|March|April|May|June|July|August|September|October|November|December)\s+(\d{4})/i);
          return monthMatch ? { name: monthMatch[1], year: parseInt(monthMatch[2], 10) } : null;
        });

        if (!currentMonth) {
          logger.warn('Smart search: could not detect current month');
          break;
        }

        const monthNames = ['January', 'February', 'March', 'April', 'May', 'June',
          'July', 'August', 'September', 'October', 'November', 'December'];
        const monthNum = monthNames.findIndex(
          m => m.toLowerCase() === currentMonth.name.toLowerCase()
        ) + 1; // 1-indexed

        logger.info(`Smart search: scanning ${currentMonth.name} ${currentMonth.year}`);

        // Extract available dates from the calendar DOM
        const availableDays = await page.evaluate(() => {
          const available: number[] = [];
          const gridCells = Array.from(document.querySelectorAll('[role="gridcell"]'));

          for (const cell of gridCells) {
            const text = cell.textContent?.trim();
            const dayNum = parseInt(text || '', 10);
            if (isNaN(dayNum) || dayNum < 1 || dayNum > 31) continue;

            const htmlCell = cell as HTMLElement;
            const isDisabled = htmlCell.hasAttribute('disabled') ||
                              htmlCell.getAttribute('aria-disabled') === 'true' ||
                              htmlCell.classList.contains('disabled');
            const style = window.getComputedStyle(htmlCell);
            const opacity = parseFloat(style.opacity);
            const pointerEvents = style.pointerEvents;

            const button = htmlCell.querySelector('button');
            let buttonClickable = true;
            if (button) {
              buttonClickable = !button.hasAttribute('disabled') &&
                               button.getAttribute('aria-disabled') !== 'true';
            }

            const isAvailable = !isDisabled && buttonClickable &&
                               opacity > 0.5 && pointerEvents !== 'none';

            if (isAvailable) {
              available.push(dayNum);
            }
          }

          return available.sort((a, b) => a - b);
        });

        logger.info(`Smart search: ${availableDays.length} available days in ${currentMonth.name}`);

        // Filter by requested days of week
        const filteredDays = availableDays.filter(dayNum => {
          if (!request.daysOfWeek || request.daysOfWeek.length === 0) return true;
          const date = new Date(currentMonth.year, monthNum - 1, dayNum);
          return request.daysOfWeek.includes(date.getDay());
        });

        logger.info(`Smart search: ${filteredDays.length} days match day-of-week filter`);
        totalDatesScanned += filteredDays.length;

        // Click each matching day and extract slots
        for (const dayNum of filteredDays) {
          if (collectedSlots >= maxSlots) break;
          if (Date.now() - startTime > request.timeout - 5000) break;

          try {
            const dayClicked = await this.clickMoxieDay(page, dayNum);
            if (!dayClicked) continue;

            const slots = await this.extractMoxieTimeSlots(page);

            // Filter by time preferences
            const filteredSlots = slots.filter(slot => {
              if (!slot.available) return false;
              if (!request.afterTime && !request.beforeTime) return true;

              const slotMinutes = this.parseTime(slot.time);
              if (request.afterTime) {
                const [ah, am] = request.afterTime.split(':').map(Number);
                if (slotMinutes < ah * 60 + am) return false;
              }
              if (request.beforeTime) {
                const [bh, bm] = request.beforeTime.split(':').map(Number);
                if (slotMinutes >= bh * 60 + bm) return false;
              }
              return true;
            });

            if (filteredSlots.length > 0) {
              const mm = String(monthNum).padStart(2, '0');
              const dd = String(dayNum).padStart(2, '0');
              const dateStr = `${currentMonth.year}-${mm}-${dd}`;
              const date = new Date(currentMonth.year, monthNum - 1, dayNum);

              results.push({
                date: dateStr,
                dayOfWeek: date.getDay(),
                slots: filteredSlots,
              });
              totalDatesWithSlots++;
              collectedSlots += filteredSlots.length;
              logger.info(`Smart search: ${filteredSlots.length} slots on ${dateStr}`);
            }
          } catch (err) {
            logger.warn(`Smart search: failed to extract day ${dayNum}`, {
              error: (err as Error).message,
            });
          }
        }

        if (collectedSlots >= maxSlots) {
          logger.info('Smart search: reached maxSlots, stopping');
          break;
        }
      }

      const duration = Date.now() - startTime;
      logger.info(`Smart search complete: ${results.length} dates with slots in ${duration}ms`);

      return {
        success: true,
        bookingUrl: request.bookingUrl,
        service: request.serviceName,
        providers: detectedProviders.length > 0 ? detectedProviders : undefined,
        results,
        totalDatesScanned,
        totalDatesWithSlots,
        scrapedAt: new Date().toISOString(),
        durationMs: duration,
      };

    } catch (error) {
      const duration = Date.now() - startTime;
      logger.error('Smart search failed', {
        error: (error as Error).message,
        duration,
        resultsCollected: results.length,
      });

      // Return partial results if we have any
      if (results.length > 0) {
        return {
          success: true,
          bookingUrl: request.bookingUrl,
          service: request.serviceName,
          providers: detectedProviders.length > 0 ? detectedProviders : undefined,
          results,
          totalDatesScanned,
          totalDatesWithSlots,
          scrapedAt: new Date().toISOString(),
          durationMs: duration,
        };
      }

      return {
        success: false,
        bookingUrl: request.bookingUrl,
        results: [],
        totalDatesScanned: 0,
        totalDatesWithSlots: 0,
        scrapedAt: new Date().toISOString(),
        durationMs: duration,
        error: (error as Error).message,
      };
    } finally {
      if (page) await page.close().catch(() => {});
      if (context) await context.close().catch(() => {});
    }
  }

  private async applyStealthMeasures(page: Page): Promise<void> {
    await page.addInitScript(() => {
      Object.defineProperty(navigator, 'webdriver', { get: () => false });
      Object.defineProperty(navigator, 'plugins', { get: () => [1, 2, 3, 4, 5] });
      Object.defineProperty(navigator, 'languages', { get: () => ['en-US', 'en'] });
      Object.defineProperty(navigator, 'platform', { get: () => 'Win32' });
    });
  }

  private async detectPlatform(page: Page): Promise<BookingPlatformSelectors> {
    const moxieIndicators = ['moxie', 'withmoxie', 'joinmoxie'];
    const pageUrl = page.url().toLowerCase();
    const pageContent = await page.content();

    for (const indicator of moxieIndicators) {
      if (pageUrl.includes(indicator) || pageContent.toLowerCase().includes(indicator)) {
        logger.info('Detected Moxie booking platform');
        return MOXIE_SELECTORS;
      }
    }

    logger.info('Using generic booking selectors');
    return {
      platform: 'generic',
      dateSelector: 'input[type="date"], .date-picker, [data-date]',
      timeSlotSelector: '.time-slot, .appointment-slot, [data-time], button[class*="time"]',
      availableSlotClass: 'available',
      unavailableSlotClass: 'unavailable',
    };
  }

  private async saveDebugScreenshot(page: Page, name: string): Promise<void> {
    if (!DEBUG_MODE) return;
    try {
      if (!fs.existsSync(DEBUG_DIR)) {
        fs.mkdirSync(DEBUG_DIR, { recursive: true });
      }
      const timestamp = new Date().toISOString().replace(/[:.]/g, '-');
      const filename = `${timestamp}-${name}.png`;
      await page.screenshot({ path: path.join(DEBUG_DIR, filename), fullPage: true });
      logger.info(`Debug screenshot saved: ${filename}`);
    } catch (error) {
      logger.warn('Failed to save debug screenshot', { error });
    }
  }

  /**
   * Handle Moxie's multi-step booking flow
   * Returns the list of available providers detected from the provider selection panel
   */
  private async handleMoxieServiceSelection(page: Page, serviceName?: string): Promise<string[]> {
    logger.info('Handling Moxie service selection flow...', serviceName ? { requestedService: serviceName } : {});
    await this.saveDebugScreenshot(page, '01-initial-page');

    // Will be populated with detected providers
    let detectedProviders: string[] = [];

    // Step 1: Click on the requested service, or fallback to multi-provider service
    let serviceClicked = false;
    let selectedServiceName = '';

    // If specific service requested, try to find and click it
    if (serviceName) {
      try {
        logger.info(`Searching for specific service: "${serviceName}"`);

        // First try using the search box if available
        const searchBox = page.locator('input[placeholder*="Search" i], input[placeholder*="search" i]').first();
        if (await searchBox.isVisible({ timeout: 2000 })) {
          logger.info('Using search box to find service');
          await searchBox.fill(serviceName);
          await this.delay(1000);
          await this.saveDebugScreenshot(page, '01b-after-search');

          // Now look for the service in the filtered results
          // Try multiple strategies to find the service card/button
          let serviceElement = null;

          // Strategy 1: Look for exact text match in any clickable container
          const exactMatch = page.locator(`div, button, a, [role="button"]`).filter({
            hasText: new RegExp(`^${serviceName.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')}`, 'i')
          }).first();

          if (await exactMatch.isVisible({ timeout: 2000 })) {
            serviceElement = exactMatch;
          } else {
            // Strategy 2: Look for partial text match
            serviceElement = page.locator(`div, button, a`).filter({
              hasText: new RegExp(serviceName, 'i')
            }).first();
          }

          if (serviceElement && await serviceElement.isVisible({ timeout: 3000 })) {
            const elementText = await serviceElement.textContent();
            selectedServiceName = elementText?.split('\n')[0]?.trim() || serviceName;
            await serviceElement.click();
            await this.delay(2000);
            serviceClicked = true;
            logger.info(`✅ Clicked on requested service: "${selectedServiceName}"`);
            await this.saveDebugScreenshot(page, '02-after-service-click');
          }
        } else {
          // Fallback: Try to find the service by text without search
          const serviceButton = page.locator(`button, div[role="button"], a`).filter({
            hasText: new RegExp(serviceName, 'i')
          }).first();

          if (await serviceButton.isVisible({ timeout: 3000 })) {
            const buttonText = await serviceButton.textContent();
            selectedServiceName = buttonText?.trim() || serviceName;
            await serviceButton.click();
            await this.delay(2000);
            serviceClicked = true;
            logger.info(`✅ Clicked on requested service: "${selectedServiceName}"`);
            await this.saveDebugScreenshot(page, '02-after-service-click');
          }
        }

        if (!serviceClicked) {
          logger.warn(`Service "${serviceName}" not found, falling back to auto-select`);
        }
      } catch (err) {
        logger.warn(`Failed to find/click service "${serviceName}"`, { error: (err as Error).message });
      }
    }

    // Fallback: Click on a multi-provider service (for more availability)
    if (!serviceClicked) {
      try {
        // Get all service cards/buttons
        const serviceCards = await page.$$('div[class*="service"], button:has-text("provider")');

        if (serviceCards.length > 0) {
          // Try to extract service name from the first card
          const firstCard = serviceCards[0];
          const cardText = await firstCard.textContent();
          selectedServiceName = cardText?.split('\n')[0]?.trim() || 'Unknown Service';

          // Look for multi-provider indicator
          const multiProviderElements = await page.$$('text=/\\d+\\s*providers/i');
          logger.info(`Found ${multiProviderElements.length} elements with "X providers" text`);

          if (multiProviderElements.length > 0) {
            await multiProviderElements[0].click();
            await this.delay(2000);
            serviceClicked = true;
            logger.info(`⚠️  Auto-selected first service with multiple providers: "${selectedServiceName}"`);
            await this.saveDebugScreenshot(page, '02-after-service-click');
          }
        }
      } catch (err) {
        logger.warn('Failed to find/click multi-provider service', { error: (err as Error).message });
      }
    }

    // Fallback: click any service with duration
    if (!serviceClicked) {
      const durationElements = await page.$$('text=/\\d+\\s*(min|hour|hr)/i');
      if (durationElements.length > 0) {
        try {
          await durationElements[0].click();
          await this.delay(1500);
          serviceClicked = true;
        } catch (err) {
          logger.warn('Failed to click duration element', { error: (err as Error).message });
        }
      }
    }

    await this.saveDebugScreenshot(page, '03-provider-panel');
    await this.delay(1000);

    // Step 2a: Detect available providers from the provider selection panel
    try {
      detectedProviders = await page.evaluate(() => {
        const providers: string[] = [];

        // Look for provider names near radio buttons
        const radioLabels = Array.from(document.querySelectorAll('label, [role="radio"], input[type="radio"]'));

        for (const el of radioLabels) {
          // Get the text content, which should be the provider name
          let labelText = '';

          if (el.tagName === 'LABEL') {
            labelText = (el as HTMLElement).textContent?.trim() || '';
          } else if (el.tagName === 'INPUT') {
            // For input[type="radio"], look for associated label or nearby text
            const id = el.getAttribute('id');
            if (id) {
              const associatedLabel = document.querySelector(`label[for="${id}"]`);
              if (associatedLabel) {
                labelText = associatedLabel.textContent?.trim() || '';
              }
            }
            // Also check parent/sibling elements
            const parent = el.parentElement;
            if (parent && !labelText) {
              labelText = parent.textContent?.trim() || '';
            }
          }

          // Filter out generic options and empty strings
          if (labelText &&
              !labelText.toLowerCase().includes('no preference') &&
              !labelText.toLowerCase().includes('first available') &&
              labelText.length > 2 &&
              labelText.length < 100) {
            // Clean up the text - remove extra whitespace and non-name content
            const cleanName = labelText.split('\n')[0].trim();
            if (cleanName && !providers.includes(cleanName)) {
              providers.push(cleanName);
            }
          }
        }

        // Also look for provider cards/buttons with names
        const providerElements = Array.from(document.querySelectorAll('[class*="provider"], [data-provider], .provider-option'));
        for (const el of providerElements) {
          const text = (el as HTMLElement).textContent?.trim();
          if (text &&
              !text.toLowerCase().includes('no preference') &&
              !text.toLowerCase().includes('first available') &&
              text.length > 2 &&
              text.length < 100) {
            const cleanName = text.split('\n')[0].trim();
            if (cleanName && !providers.includes(cleanName)) {
              providers.push(cleanName);
            }
          }
        }

        return providers;
      });

      logger.info(`Detected ${detectedProviders.length} providers: ${detectedProviders.join(', ')}`);
    } catch (err) {
      logger.warn('Failed to detect providers', { error: (err as Error).message });
    }

    // Step 2b: Select "No preference (first available)" provider
    let providerSelected = false;
    try {
      const noPreferenceLocator = page.locator('text=/no preference|first available/i').first();
      if (await noPreferenceLocator.isVisible({ timeout: 2000 })) {
        await noPreferenceLocator.click({ force: true });
        await this.delay(500);
        providerSelected = true;
        logger.info('Clicked "No preference" using locator');
      }
    } catch (err) {
      logger.debug('Locator method for No preference failed');
    }

    if (!providerSelected) {
      const radioButtons = await page.$$('input[type="radio"]');
      if (radioButtons.length > 0) {
        try {
          await radioButtons[0].click({ force: true });
          providerSelected = true;
        } catch (err) {
          logger.warn('Failed to click radio button');
        }
      }
    }

    await this.saveDebugScreenshot(page, '03b-after-provider-select');

    // Step 3: Click "Confirm selection" button
    try {
      const confirmBtn = page.locator('text=/confirm selection/i').first();
      if (await confirmBtn.isVisible({ timeout: 2000 })) {
        await confirmBtn.click({ force: true });
        logger.info('Clicked "Confirm selection" button');
        await this.delay(2000);
      }
    } catch (err) {
      // Try fallback
      await page.evaluate(() => {
        const buttons = Array.from(document.querySelectorAll('button'));
        for (const btn of buttons) {
          if (btn.textContent?.toLowerCase().includes('confirm') && !btn.disabled) {
            btn.click();
            return;
          }
        }
      });
      await this.delay(2000);
    }

    await this.saveDebugScreenshot(page, '04-after-confirm');

    // Step 4: Click "Next step" button
    await page.evaluate(() => window.scrollTo(0, document.body.scrollHeight));
    await this.delay(500);

    try {
      const nextStepBtn = page.locator('text=/next step/i').first();
      if (await nextStepBtn.isVisible({ timeout: 2000 })) {
        await nextStepBtn.click({ force: true });
        logger.info('Clicked "Next step" button');
        await this.delay(2000);
      }
    } catch (err) {
      await page.evaluate(() => {
        const buttons = Array.from(document.querySelectorAll('button, [role="button"]'));
        for (const btn of buttons) {
          if (btn.textContent?.toLowerCase().includes('next step')) {
            (btn as HTMLElement).click();
            return;
          }
        }
      });
      await this.delay(2000);
    }

    await this.saveDebugScreenshot(page, '05-after-next-step');
    await this.delay(2000);
    await this.saveDebugScreenshot(page, '06-calendar-view');
    logger.info('Moxie service selection flow complete', { providers: detectedProviders });

    return detectedProviders;
  }

  private async navigateToDate(
    page: Page,
    targetDate: string,
    selectors: BookingPlatformSelectors
  ): Promise<void> {
    logger.info(`Navigating to date: ${targetDate}`);

    // Parse date string directly to avoid timezone issues
    // Format expected: YYYY-MM-DD
    const dateParts = targetDate.split('-');
    const targetYear = parseInt(dateParts[0], 10);
    const targetMonth = parseInt(dateParts[1], 10) - 1; // 0-indexed month
    const targetDay = parseInt(dateParts[2], 10);

    logger.info(`Parsed date: year=${targetYear}, month=${targetMonth}, day=${targetDay}`);

    if (selectors.platform === 'moxie') {
      await this.navigateMoxieCalendar(page, targetYear, targetMonth, targetDay);
      return;
    }

    // Generic date navigation
    const dateInput = await page.$(selectors.dateSelector);
    if (dateInput) {
      try {
        await dateInput.fill(targetDate);
        await this.delay(1000);
        return;
      } catch {
        // Fall through
      }
    }

    const today = new Date();
    today.setHours(0, 0, 0, 0);
    const targetDateObj = new Date(targetYear, targetMonth, targetDay);
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

    await this.delay(1000);
  }

  /**
   * Navigate Moxie's monthly calendar to target date
   */
  private async navigateMoxieCalendar(
    page: Page,
    targetYear: number,
    targetMonth: number,
    targetDay: number
  ): Promise<void> {
    const monthNames = ['January', 'February', 'March', 'April', 'May', 'June',
      'July', 'August', 'September', 'October', 'November', 'December'];

    const targetMonthName = monthNames[targetMonth];
    logger.info(`Navigating Moxie calendar to ${targetMonthName} ${targetYear}, day ${targetDay}`);

    await this.delay(1000);

    // Check if we're already on the target month (calendar might already show February 2026)
    const getCurrentMonth = async (): Promise<string> => {
      return await page.evaluate(() => {
        // Look specifically for the calendar month header
        // It's usually displayed prominently as "February 2026" with dropdown arrow
        const bodyText = document.body.innerText;
        const monthMatch = bodyText.match(/(January|February|March|April|May|June|July|August|September|October|November|December)\s+(\d{4})/i);
        return monthMatch ? `${monthMatch[1]} ${monthMatch[2]}` : '';
      });
    };

    let currentMonth = await getCurrentMonth();
    logger.info(`Initial calendar month: "${currentMonth}"`);

    // Check if already on target month
    const targetMonthStr = `${targetMonthName} ${targetYear}`;
    if (currentMonth.toLowerCase() === targetMonthStr.toLowerCase()) {
      logger.info('Already on target month, no navigation needed');
    } else {
      // Navigate to correct month
      let attempts = 0;
      while (attempts < 12) {
        currentMonth = await getCurrentMonth();
        logger.info(`Current calendar shows: "${currentMonth}"`);

        if (currentMonth.toLowerCase() === targetMonthStr.toLowerCase()) {
          logger.info('Reached target month!');
          break;
        }

        // Click next month button
        const clicked = await page.evaluate(() => {
          const buttons = Array.from(document.querySelectorAll('button, [role="button"], [class*="arrow"], [class*="nav"]'));
          for (const btn of buttons) {
            const text = btn.textContent?.trim();
            const label = btn.getAttribute('aria-label')?.toLowerCase() || '';
            if (text === '>' || text === '›' || text === '→' ||
                label.includes('next') || label.includes('forward')) {
              (btn as HTMLElement).click();
              return true;
            }
          }
          return false;
        });

        if (clicked) {
          logger.info('Clicked next month button');
          await this.delay(1500); // Longer delay for calendar to update
        } else {
          logger.warn('Could not find next month button');
          break;
        }
        attempts++;
      }
    }

    await this.saveDebugScreenshot(page, '08-after-month-nav');
    await this.delay(1000);

    await this.clickMoxieDay(page, targetDay);
  }

  /**
   * Click a specific day on the Moxie calendar using multiple fallback strategies.
   * Returns true if the click was successful and time slots appeared.
   */
  private async clickMoxieDay(page: Page, targetDay: number): Promise<boolean> {
    const dayStr = String(targetDay);
    logger.info(`Looking for day ${targetDay} to click...`);

    let clicked = false;

    // Strategy 1: Use Playwright's getByRole to find grid cells
    try {
      const gridCells = page.getByRole('gridcell');
      const cellCount = await gridCells.count();

      for (let i = 0; i < cellCount; i++) {
        const cell = gridCells.nth(i);
        const text = await cell.textContent();

        if (text?.trim() === dayStr) {
          const box = await cell.boundingBox();
          if (box) {
            await page.mouse.click(box.x + box.width / 2, box.y + box.height / 2);
            clicked = true;
            await this.delay(2000);
            break;
          }
        }
      }
    } catch (err) {
      logger.warn('Strategy 1 failed:', (err as Error).message);
    }

    // Strategy 2: Find buttons containing the day number
    if (!clicked) {
      try {
        const buttons = page.locator('button');
        const buttonCount = await buttons.count();

        for (let i = 0; i < buttonCount; i++) {
          const btn = buttons.nth(i);
          const text = await btn.textContent();

          if (text?.trim() === dayStr) {
            const box = await btn.boundingBox();
            if (box && box.x > 550 && box.width < 100) {
              await btn.click({ force: true, timeout: 5000 });
              clicked = true;
              await this.delay(2000);
              break;
            }
          }
        }
      } catch (err) {
        logger.warn('Strategy 2 failed:', (err as Error).message);
      }
    }

    // Strategy 3: Use CSS selector for button with exact text match
    if (!clicked) {
      try {
        const dayButton = page.locator(`button:has-text("${dayStr}")`).filter({
          has: page.locator(`text=/^${dayStr}$/`)
        });

        const count = await dayButton.count();

        for (let i = 0; i < count; i++) {
          const btn = dayButton.nth(i);
          const box = await btn.boundingBox();

          if (box && box.x > 550) {
            await btn.click({ force: true });
            clicked = true;
            await this.delay(2000);
            break;
          }
        }
      } catch (err) {
        logger.warn('Strategy 3 failed:', (err as Error).message);
      }
    }

    // Strategy 4: Find by inspecting the DOM and clicking parent containers
    if (!clicked) {
      const clickResult = await page.evaluate((day) => {
        const allElements = Array.from(document.querySelectorAll('button, div[role="button"], span'));

        for (const el of allElements) {
          const text = (el as HTMLElement).textContent?.trim();
          if (text === day) {
            const rect = (el as HTMLElement).getBoundingClientRect();

            if (rect.x > 550 && rect.width < 100 && rect.height < 80) {
              let target = el as HTMLElement;
              let parent = el.parentElement;

              while (parent && parent !== document.body) {
                if (parent.tagName === 'BUTTON' ||
                    parent.getAttribute('role') === 'gridcell' ||
                    parent.getAttribute('role') === 'button') {
                  target = parent;
                  break;
                }
                parent = parent.parentElement;
              }

              const targetRect = target.getBoundingClientRect();
              const eventInit = {
                bubbles: true,
                cancelable: true,
                view: window,
                clientX: targetRect.x + targetRect.width / 2,
                clientY: targetRect.y + targetRect.height / 2
              };

              target.dispatchEvent(new PointerEvent('pointerdown', eventInit));
              target.dispatchEvent(new MouseEvent('mousedown', eventInit));
              target.dispatchEvent(new PointerEvent('pointerup', eventInit));
              target.dispatchEvent(new MouseEvent('mouseup', eventInit));
              target.dispatchEvent(new MouseEvent('click', eventInit));

              return { success: true };
            }
          }
        }

        return { success: false };
      }, dayStr);

      if (clickResult.success) {
        clicked = true;
        await this.delay(2000);
      }
    }

    // Check if time slots appeared
    if (clicked) {
      const pageText = await page.evaluate(() => document.body.innerText);
      const hasTimeSlots = /\d{1,2}:\d{2}\s*(am|pm)/i.test(pageText);
      if (!hasTimeSlots) {
        logger.warn(`Day click may not have worked for day ${dayStr}`);
        await this.saveDebugScreenshot(page, `no-time-slots-day-${dayStr}`);
      }
      return hasTimeSlots;
    }

    logger.warn(`Could not click day ${targetDay}`);
    return false;
  }

  private async extractTimeSlots(
    page: Page,
    selectors: BookingPlatformSelectors
  ): Promise<TimeSlot[]> {
    await this.saveDebugScreenshot(page, '11-extracting-slots');

    if (selectors.platform === 'moxie') {
      return this.extractMoxieTimeSlots(page);
    }

    const slots: TimeSlot[] = [];

    try {
      await page.waitForSelector(selectors.timeSlotSelector, { timeout: 10000 });
    } catch {
      logger.warn('No time slots found on page');
      return slots;
    }

    const slotElements = await page.$$(selectors.timeSlotSelector);
    logger.info(`Found ${slotElements.length} slot elements`);

    for (const element of slotElements) {
      try {
        const text = await element.textContent();
        if (!text) continue;

        const timeMatch = text.match(/\d{1,2}:\d{2}\s*(AM|PM)?|\d{1,2}\s*(AM|PM)/i);
        if (!timeMatch) continue;

        const time = timeMatch[0].trim();
        const classList = await element.getAttribute('class') || '';
        const isDisabled = await element.getAttribute('disabled');
        const ariaDisabled = await element.getAttribute('aria-disabled');

        const available =
          !isDisabled &&
          ariaDisabled !== 'true' &&
          !classList.includes(selectors.unavailableSlotClass || 'unavailable') &&
          !classList.includes('disabled') &&
          !classList.includes('booked');

        slots.push({ time, available });
      } catch (error) {
        logger.debug('Failed to extract slot', { error });
      }
    }

    slots.sort((a, b) => this.parseTime(a.time) - this.parseTime(b.time));
    return slots;
  }

  /**
   * Extract time slots from Moxie's booking UI
   */
  private async extractMoxieTimeSlots(page: Page): Promise<TimeSlot[]> {
    const slots: TimeSlot[] = [];
    logger.info('Extracting Moxie time slots...');

    const allTimeSlots = await page.evaluate(() => {
      const results: Array<{ time: string; available: boolean }> = [];
      const seenTimes = new Set<string>();

      const allElements = Array.from(document.querySelectorAll('button, div, span'));
      for (const el of allElements) {
        const text = el.textContent?.trim() || '';
        const timeMatch = text.match(/^(\d{1,2}:\d{2}\s*(am|pm))$/i);
        if (timeMatch) {
          const time = timeMatch[1].toLowerCase();
          if (seenTimes.has(time)) continue;

          const rect = (el as HTMLElement).getBoundingClientRect();
          if (rect.x > 600 && rect.width > 20 && rect.width < 200 && rect.height > 0) {
            const isDisabled = (el as HTMLElement).hasAttribute('disabled') ||
                              el.getAttribute('aria-disabled') === 'true' ||
                              (el as HTMLElement).className.includes('disabled');

            seenTimes.add(time);
            results.push({ time: timeMatch[1], available: !isDisabled });
          }
        }
      }
      return results;
    });

    logger.info(`Found ${allTimeSlots.length} Moxie time slots`);

    for (const slot of allTimeSlots) {
      slots.push({ time: slot.time, available: slot.available });
    }

    // Fallback: extract from page text
    if (slots.length === 0) {
      const pageText = await page.evaluate(() => document.body.innerText);
      const timeMatches = pageText.match(/\d{1,2}:\d{2}\s*(am|pm)/gi);
      if (timeMatches) {
        const uniqueTimes = [...new Set(timeMatches.map(t => t.toLowerCase()))];
        for (const time of uniqueTimes) {
          slots.push({ time, available: true });
        }
      }
    }

    slots.sort((a, b) => this.parseTime(a.time) - this.parseTime(b.time));
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

  /**
   * Scrape availability for multiple dates in a single browser session.
   * Does service selection ONCE, then navigates the calendar for each date.
   * ~2-3s per date instead of ~15-25s.
   */
  async scrapeMultipleDates(
    bookingUrl: string,
    dates: string[],
    serviceName?: string,
    providerName?: string,
    timeout: number = 30000,
  ): Promise<Map<string, AvailabilityResponse>> {
    if (!this.browser) {
      throw new ScraperError('Browser not initialized', 'NOT_INITIALIZED');
    }

    const results = new Map<string, AvailabilityResponse>();
    const startTime = Date.now();
    let context: BrowserContext | null = null;
    let page: Page | null = null;

    try {
      context = await this.browser.newContext({
        userAgent: this.config.userAgent,
        viewport: { width: 1920, height: 1080 },
        locale: 'en-US',
        timezoneId: 'America/New_York',
      });

      page = await context.newPage();
      await this.applyStealthMeasures(page);

      logger.info(`[batch] Navigating to ${bookingUrl} for ${dates.length} dates`);

      const response = await page.goto(bookingUrl, {
        timeout,
        waitUntil: 'domcontentloaded',
      });

      if (!response || !response.ok()) {
        throw new NavigationError(`Failed to load page: ${response?.status() || 'unknown'}`);
      }

      await page.waitForLoadState('domcontentloaded');
      await this.delay(2000);

      const selectors = await this.detectPlatform(page);

      // Do service selection ONCE
      let detectedProviders: string[] = [];
      if (selectors.platform === 'moxie') {
        detectedProviders = await this.handleMoxieServiceSelection(page, serviceName);
      }

      const setupDuration = Date.now() - startTime;
      logger.info(`[batch] Setup complete in ${setupDuration}ms, scraping ${dates.length} dates...`);

      // Now iterate through dates — only calendar navigation + slot extraction
      for (const date of dates) {
        const dateStart = Date.now();
        try {
          await this.navigateToDate(page, date, selectors);
          const slots = await this.extractTimeSlots(page, selectors);

          const dateDuration = Date.now() - dateStart;
          logger.info(`[batch] Date ${date}: ${slots.length} slots in ${dateDuration}ms`);

          results.set(date, {
            success: true,
            bookingUrl,
            date,
            slots,
            provider: providerName,
            service: serviceName,
            providers: detectedProviders.length > 0 ? detectedProviders : undefined,
            scrapedAt: new Date().toISOString(),
          });
        } catch (err) {
          const dateDuration = Date.now() - dateStart;
          logger.warn(`[batch] Date ${date} failed in ${dateDuration}ms: ${(err as Error).message}`);
          results.set(date, {
            success: false,
            bookingUrl,
            date,
            slots: [],
            scrapedAt: new Date().toISOString(),
            error: (err as Error).message,
          });
        }
      }

      const totalDuration = Date.now() - startTime;
      logger.info(`[batch] All ${dates.length} dates scraped in ${totalDuration}ms`);

    } catch (error) {
      logger.error(`[batch] Session failed: ${(error as Error).message}`);
      // Fill remaining dates with errors
      for (const date of dates) {
        if (!results.has(date)) {
          results.set(date, {
            success: false,
            bookingUrl,
            date,
            slots: [],
            scrapedAt: new Date().toISOString(),
            error: (error as Error).message,
          });
        }
      }
    } finally {
      if (page) await page.close().catch(() => {});
      if (context) await context.close().catch(() => {});
    }

    return results;
  }

  private delay(ms: number): Promise<void> {
    return new Promise(resolve => setTimeout(resolve, ms));
  }
}

// Singleton instance
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
