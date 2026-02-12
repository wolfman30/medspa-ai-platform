import { Browser, BrowserContext, Page, chromium } from 'playwright';
import { v4 as uuidv4 } from 'uuid';
import {
  BookingSession,
  BookingSessionState,
  BookingOutcome,
  BookingStartRequest,
  BookingStartResponse,
  BookingHandoffResponse,
  BookingStatusResponse,
  LeadInfo,
  ConfirmationDetails,
  OutcomeIndicators,
  MOXIE_OUTCOME_INDICATORS,
  ScraperError,
  NavigationError,
} from './types';
import logger from './logger';

const DEFAULT_USER_AGENT = 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36';

/**
 * Configuration for the BookingSessionManager
 */
export interface BookingSessionManagerConfig {
  headless: boolean;
  maxConcurrentSessions: number;
  sessionTTL: number;        // Time before session expires (ms)
  monitoringPollInterval: number;  // How often to check for outcome (ms)
  monitoringTimeout: number; // Max time to wait for outcome (ms)
}

const DEFAULT_CONFIG: BookingSessionManagerConfig = {
  headless: true,
  maxConcurrentSessions: 5,
  sessionTTL: 15 * 60 * 1000,         // 15 minutes
  monitoringPollInterval: 2000,       // 2 seconds
  monitoringTimeout: 10 * 60 * 1000,  // 10 minutes
};

/**
 * Internal session state including browser references
 */
interface InternalSession extends BookingSession {
  context?: BrowserContext;
  page?: Page;
  monitoringTimerId?: NodeJS.Timeout;
  cleanupTimerId?: NodeJS.Timeout;
}

/**
 * BookingSessionManager handles the full booking flow:
 * 1. Automates Steps 1-4 (service, provider, date/time, contact details)
 * 2. Stops at Step 5 (payment page) and provides handoff URL
 * 3. Monitors for booking outcome (success/failure)
 */
export class BookingSessionManager {
  private browser: Browser | null = null;
  private sessions: Map<string, InternalSession> = new Map();
  private config: BookingSessionManagerConfig;
  private outcomeIndicators: OutcomeIndicators;

  constructor(
    config: Partial<BookingSessionManagerConfig> = {},
    outcomeIndicators: OutcomeIndicators = MOXIE_OUTCOME_INDICATORS
  ) {
    this.config = { ...DEFAULT_CONFIG, ...config };
    this.outcomeIndicators = outcomeIndicators;
  }

  /**
   * Initialize the browser
   */
  async initialize(): Promise<void> {
    if (this.browser) {
      return;
    }

    logger.info('BookingSessionManager: Initializing browser...');

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

    logger.info('BookingSessionManager: Browser initialized');
  }

  /**
   * Close browser and cleanup all sessions
   */
  async close(): Promise<void> {
    // Cancel all monitoring and cleanup timers
    for (const session of this.sessions.values()) {
      if (session.monitoringTimerId) {
        clearInterval(session.monitoringTimerId);
      }
      if (session.cleanupTimerId) {
        clearTimeout(session.cleanupTimerId);
      }
      await session.context?.close().catch(() => {});
    }

    this.sessions.clear();

    if (this.browser) {
      await this.browser.close();
      this.browser = null;
    }

    logger.info('BookingSessionManager: Closed');
  }

  /**
   * Start a new booking session
   */
  async startSession(request: BookingStartRequest): Promise<BookingStartResponse> {
    if (!this.browser) {
      throw new ScraperError('Browser not initialized', 'NOT_INITIALIZED');
    }

    // Check concurrent session limit
    const activeSessions = Array.from(this.sessions.values()).filter(
      s => !['completed', 'failed', 'abandoned', 'cancelled'].includes(s.state)
    ).length;

    if (activeSessions >= this.config.maxConcurrentSessions) {
      return {
        success: false,
        sessionId: '',
        state: 'created',
        error: `Max concurrent sessions (${this.config.maxConcurrentSessions}) reached`,
      };
    }

    const sessionId = uuidv4();
    const session: InternalSession = {
      id: sessionId,
      state: 'created',
      bookingUrl: request.bookingUrl,
      date: request.date,
      time: request.time,
      lead: request.lead,
      service: request.service,
      provider: request.provider,
      callbackUrl: request.callbackUrl,
      createdAt: new Date(),
      updatedAt: new Date(),
    };

    this.sessions.set(sessionId, session);
    logger.info(`BookingSession ${sessionId}: Created`, { url: request.bookingUrl });

    // Start the booking flow asynchronously
    this.runBookingFlow(sessionId, request).catch(error => {
      logger.error(`BookingSession ${sessionId}: Flow failed`, { error: error.message });
    });

    // Schedule session cleanup
    session.cleanupTimerId = setTimeout(() => {
      this.cleanupSession(sessionId);
    }, this.config.sessionTTL);

    return {
      success: true,
      sessionId,
      state: session.state,
    };
  }

  /**
   * Get handoff URL for a session
   */
  async getHandoffUrl(sessionId: string): Promise<BookingHandoffResponse> {
    const session = this.sessions.get(sessionId);

    if (!session) {
      return {
        success: false,
        sessionId,
        handoffUrl: '',
        expiresAt: '',
        state: 'cancelled',
        error: 'Session not found',
      };
    }

    if (session.state === 'navigating') {
      return {
        success: false,
        sessionId,
        handoffUrl: '',
        expiresAt: '',
        state: session.state,
        error: 'Session still navigating, please wait',
      };
    }

    if (!session.handoffUrl) {
      return {
        success: false,
        sessionId,
        handoffUrl: '',
        expiresAt: '',
        state: session.state,
        error: 'No handoff URL available',
      };
    }

    // Transition to monitoring state when handoff URL is retrieved
    if (session.state === 'ready_for_handoff') {
      session.state = 'monitoring';
      session.updatedAt = new Date();
      this.startOutcomeMonitoring(sessionId);
    }

    return {
      success: true,
      sessionId,
      handoffUrl: session.handoffUrl,
      expiresAt: session.handoffExpiresAt?.toISOString() || '',
      state: session.state,
    };
  }

  /**
   * Get session status
   */
  getSessionStatus(sessionId: string): BookingStatusResponse {
    const session = this.sessions.get(sessionId);

    if (!session) {
      return {
        success: false,
        sessionId,
        state: 'cancelled',
        error: 'Session not found',
        createdAt: '',
        updatedAt: '',
      };
    }

    return {
      success: true,
      sessionId,
      state: session.state,
      outcome: session.outcome,
      confirmationDetails: session.confirmationDetails,
      error: session.errorMessage,
      createdAt: session.createdAt.toISOString(),
      updatedAt: session.updatedAt.toISOString(),
    };
  }

  /**
   * Cancel a session
   */
  async cancelSession(sessionId: string): Promise<boolean> {
    const session = this.sessions.get(sessionId);

    if (!session) {
      return false;
    }

    session.state = 'cancelled';
    session.outcome = 'cancelled';
    session.updatedAt = new Date();
    session.completedAt = new Date();

    await this.cleanupSession(sessionId);
    return true;
  }

  /**
   * Run the booking flow (Steps 1-4)
   */
  private async runBookingFlow(sessionId: string, request: BookingStartRequest): Promise<void> {
    const session = this.sessions.get(sessionId);
    if (!session || !this.browser) {
      return;
    }

    session.state = 'navigating';
    session.updatedAt = new Date();

    try {
      // Create browser context and page
      session.context = await this.browser.newContext({
        userAgent: DEFAULT_USER_AGENT,
        viewport: { width: 1920, height: 1080 },
        locale: 'en-US',
        timezoneId: 'America/New_York',
      });

      session.page = await session.context.newPage();
      await this.applyStealthMeasures(session.page);

      logger.info(`BookingSession ${sessionId}: Navigating to ${request.bookingUrl}`);

      // Navigate to booking page — use 'domcontentloaded' instead of 'networkidle'
      // because Moxie's booking page has persistent connections that prevent
      // networkidle from ever resolving.
      const response = await session.page.goto(request.bookingUrl, {
        timeout: request.timeout,
        waitUntil: 'domcontentloaded',
      });

      if (!response || !response.ok()) {
        throw new NavigationError(`Failed to load page: ${response?.status() || 'unknown'}`);
      }

      // Wait for the page to be interactive
      await this.delay(3000);

      // Step 1: Select service
      logger.info(`BookingSession ${sessionId}: Step 1 - Selecting service`);
      await this.selectService(session.page, request.service);

      // Step 2: Select provider
      logger.info(`BookingSession ${sessionId}: Step 2 - Selecting provider`);
      await this.selectProvider(session.page, request.provider);

      // Intercept network requests to capture Moxie's internal API calls
      const moxieApiCalls: Array<{method: string, url: string, postData?: string, status?: number, responseBody?: string}> = [];
      session.page.on('request', (req) => {
        const url = req.url();
        if (url.includes('joinmoxie.com') && !url.includes('.js') && !url.includes('.css') && !url.includes('.png') && !url.includes('.svg') && !url.includes('.woff')) {
          const entry: any = { method: req.method(), url };
          if (req.method() === 'POST' || req.method() === 'PUT' || req.method() === 'PATCH') {
            entry.postData = req.postData()?.substring(0, 2000);
          }
          moxieApiCalls.push(entry);
        }
      });
      session.page.on('response', async (res) => {
        const url = res.url();
        const entry = moxieApiCalls.find(e => e.url === url && !e.status);
        if (entry) {
          entry.status = res.status();
          try {
            const body = await res.text();
            entry.responseBody = body.substring(0, 1000);
          } catch { /* ignore */ }
        }
      });

      // Step 3: Select date and time
      logger.info(`BookingSession ${sessionId}: Step 3 - Selecting date and time`);
      await this.selectDateTime(session.page, request.date, request.time);

      // Steps 4-6: Fill contact details and navigate through to payment
      // Moxie Step 3 = phone entry, Step 4 = name/email (new clients), Step 5 = review, Step 6 = payment
      // navigateToPaymentStep handles filling fields at each step and clicking through
      logger.info(`BookingSession ${sessionId}: Navigating through contact/review to payment`);
      await this.navigateToPaymentStep(session.page, request.lead);

      // Log all captured Moxie API calls for reverse engineering
      logger.info(`Captured ${moxieApiCalls.length} Moxie API calls during booking flow`);
      for (const call of moxieApiCalls) {
        if (call.method !== 'GET' || call.url.includes('/api/')) {
          logger.info(`Moxie API: ${call.method} ${call.url} → ${call.status || '?'}${call.postData ? ` | body: ${call.postData.substring(0, 500)}` : ''}${call.responseBody ? ` | resp: ${call.responseBody.substring(0, 500)}` : ''}`);
        }
      }

      // Capture handoff URL
      session.handoffUrl = session.page.url();
      session.handoffExpiresAt = new Date(Date.now() + this.config.monitoringTimeout);
      session.pageUrl = session.handoffUrl;
      session.state = 'ready_for_handoff';
      session.updatedAt = new Date();

      logger.info(`BookingSession ${sessionId}: Ready for handoff`, { url: session.handoffUrl });

    } catch (error) {
      logger.error(`BookingSession ${sessionId}: Failed during navigation`, {
        error: error instanceof Error ? error.message : 'Unknown error',
      });

      session.state = 'failed';
      session.outcome = 'error';
      session.errorMessage = error instanceof Error ? error.message : 'Unknown error';
      session.updatedAt = new Date();
      session.completedAt = new Date();

      // Cleanup browser resources
      await session.context?.close().catch(() => {});
      session.context = undefined;
      session.page = undefined;
    }
  }

  /**
   * Step 1: Select a service
   */
  private async selectService(page: Page, serviceName?: string): Promise<void> {
    // If specific service requested, try to find and click it
    if (serviceName) {
      try {
        // Try search box first
        const searchBox = page.locator('input[placeholder*="Search" i]').first();
        if (await searchBox.isVisible({ timeout: 2000 })) {
          await searchBox.fill(serviceName);
          await this.delay(1000);
        }

        // Click on service
        const serviceElement = page.locator(`div, button, a`).filter({
          hasText: new RegExp(serviceName, 'i'),
        }).first();

        if (await serviceElement.isVisible({ timeout: 3000 })) {
          await serviceElement.click();
          await this.delay(2000);
          logger.info(`Selected service: ${serviceName}`);
          return;
        }
      } catch (err) {
        logger.warn(`Service "${serviceName}" not found, falling back`);
      }
    }

    // Fallback: Click first service with duration indicator
    const durationElements = await page.$$('text=/\\d+\\s*(min|hour|hr)/i');
    if (durationElements.length > 0) {
      await durationElements[0].click();
      await this.delay(2000);
      logger.info('Selected first available service');
    }
  }

  /**
   * Step 2: Select a provider
   */
  private async selectProvider(page: Page, providerName?: string): Promise<void> {
    await this.delay(1000);

    // If specific provider requested, try to find them
    if (providerName) {
      try {
        const providerElement = page.locator(`text=/${providerName}/i`).first();
        if (await providerElement.isVisible({ timeout: 2000 })) {
          await providerElement.click();
          await this.delay(1000);
          logger.info(`Selected provider: ${providerName}`);
        }
      } catch {
        logger.warn(`Provider "${providerName}" not found, using first available`);
      }
    }

    // Select "No preference" or first radio button
    try {
      const noPreference = page.locator('text=/no preference|first available/i').first();
      if (await noPreference.isVisible({ timeout: 2000 })) {
        await noPreference.click({ force: true });
        await this.delay(500);
        logger.info('Selected "No preference" provider');
      }
    } catch {
      // Try first radio button
      const radioButtons = await page.$$('input[type="radio"]');
      if (radioButtons.length > 0) {
        await radioButtons[0].click({ force: true });
      }
    }

    // Click "Confirm selection"
    try {
      const confirmBtn = page.locator('text=/confirm selection/i').first();
      if (await confirmBtn.isVisible({ timeout: 2000 })) {
        await confirmBtn.click({ force: true });
        await this.delay(2000);
        logger.info('Clicked "Confirm selection"');
      }
    } catch {
      // Fallback to any confirm button
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

    // Click "Next step"
    try {
      const nextBtn = page.locator('text=/next step/i').first();
      if (await nextBtn.isVisible({ timeout: 2000 })) {
        await nextBtn.click({ force: true });
        await this.delay(2000);
        logger.info('Clicked "Next step"');
      }
    } catch {
      await page.evaluate(() => {
        const buttons = Array.from(document.querySelectorAll('button'));
        for (const btn of buttons) {
          if (btn.textContent?.toLowerCase().includes('next')) {
            (btn as HTMLElement).click();
            return;
          }
        }
      });
      await this.delay(2000);
    }
  }

  /**
   * Step 3: Select date and time
   */
  private async selectDateTime(page: Page, date: string, time: string): Promise<void> {
    // Parse date
    const [year, month, day] = date.split('-').map(Number);

    // Navigate calendar to correct month and click the day
    await this.navigateCalendar(page, year, month - 1, day);

    // Wait for time slots to load after clicking the day
    // Moxie renders time slots dynamically — can take 3-10s to appear
    logger.info('Waiting for time slots to load...');
    const maxWaitMs = 15000;
    const pollMs = 1000;
    let elapsed = 0;
    let slotsFound = false;
    while (elapsed < maxWaitMs) {
      await this.delay(pollMs);
      elapsed += pollMs;
      slotsFound = await page.evaluate(() => /\d{1,2}:\d{2}\s*(am|pm)/i.test(document.body.innerText));
      if (slotsFound) {
        logger.info(`Time slots detected after ${elapsed}ms`);
        break;
      }
    }
    if (!slotsFound) {
      logger.warn(`No time slots detected after ${maxWaitMs}ms — proceeding anyway`);
    }
    // Extra settle time for rendering
    await this.delay(500);

    // Click on the time slot
    await this.selectTimeSlot(page, time);

    // Click "Next step" to advance to contact details form
    await this.delay(1000);
    try {
      const nextBtn = page.locator('text=/next step/i').first();
      if (await nextBtn.isVisible({ timeout: 5000 })) {
        await nextBtn.click({ force: true });
        await this.delay(2000);
        logger.info('Clicked "Next step" after time selection');
      } else {
        // Fallback: try any button with "next" text
        await page.evaluate(() => {
          const buttons = Array.from(document.querySelectorAll('button'));
          for (const btn of buttons) {
            if (btn.textContent?.toLowerCase().includes('next')) {
              (btn as HTMLElement).click();
              return;
            }
          }
        });
        await this.delay(2000);
        logger.info('Clicked "Next" button (fallback) after time selection');
      }
    } catch (err) {
      logger.warn(`No "Next step" button found after time selection: ${(err as Error).message}`);
    }
  }

  /**
   * Navigate calendar to the target date
   * Uses the same proven approach as the availability scraper (gridcell + bounding box clicks)
   */
  private async navigateCalendar(page: Page, year: number, month: number, day: number): Promise<void> {
    logger.info(`Navigating calendar to ${year}-${month + 1}-${day}`);

    // Wait for calendar grid to be visible
    try {
      await page.getByRole('grid').first().waitFor({ timeout: 15000 });
      logger.info('Calendar grid detected');
    } catch {
      // Fallback: wait for any calendar-like element
      await page.waitForSelector('[class*="calendar"], [role="grid"]', { timeout: 10000 });
    }
    await this.delay(2000);

    // Navigate to correct month using next/prev buttons
    const monthNames = ['january', 'february', 'march', 'april', 'may', 'june',
      'july', 'august', 'september', 'october', 'november', 'december'];
    const targetMonthName = monthNames[month];

    let attempts = 0;
    while (attempts < 12) {
      // Get all text on page to find month header
      const pageText = await page.evaluate(() => document.body.innerText.toLowerCase());

      if (pageText.includes(targetMonthName) && pageText.includes(String(year))) {
        logger.info(`Found target month: ${targetMonthName} ${year}`);
        break;
      }

      // Click next month button
      try {
        const nextBtn = page.locator('button[aria-label*="next" i], button:has-text("›"), button:has-text(">")').first();
        if (await nextBtn.isVisible({ timeout: 1000 })) {
          await nextBtn.click();
          await this.delay(1000);
        }
      } catch {
        break;
      }
      attempts++;
    }

    // Click on the target day using the scraper's proven approach:
    // Strategy 1: gridcell role with bounding box click
    const dayStr = String(day);
    let clicked = false;

    try {
      const gridCells = page.getByRole('gridcell');
      const cellCount = await gridCells.count();
      logger.info(`Found ${cellCount} grid cells`);

      for (let i = 0; i < cellCount; i++) {
        const cell = gridCells.nth(i);
        const text = await cell.textContent();
        if (text?.trim() === dayStr) {
          const box = await cell.boundingBox();
          if (box) {
            await page.mouse.click(box.x + box.width / 2, box.y + box.height / 2);
            clicked = true;
            logger.info(`Clicked on day ${day} via gridcell bounding box`);
            await this.delay(2000);
            break;
          }
        }
      }
    } catch (err) {
      logger.warn(`Gridcell strategy failed: ${(err as Error).message}`);
    }

    // Strategy 2: button with exact text match
    if (!clicked) {
      try {
        const buttons = page.locator('button');
        const buttonCount = await buttons.count();
        for (let i = 0; i < buttonCount; i++) {
          const btn = buttons.nth(i);
          const text = await btn.textContent();
          if (text?.trim() === dayStr) {
            await btn.click({ force: true });
            clicked = true;
            logger.info(`Clicked on day ${day} via button`);
            await this.delay(2000);
            break;
          }
        }
      } catch (err) {
        logger.warn(`Button strategy failed: ${(err as Error).message}`);
      }
    }

    if (!clicked) {
      logger.error(`Could not click on day ${day}`);
    }

    // Verify time slots loaded
    const hasTimeSlots = await page.evaluate(() => /\d{1,2}:\d{2}\s*(am|pm)/i.test(document.body.innerText));
    logger.info(`Time slots visible after day click: ${hasTimeSlots}`);
  }

  /**
   * Select a specific time slot
   */
  private async selectTimeSlot(page: Page, time: string): Promise<void> {
    logger.info(`Selecting time slot: ${time} (v2 format-variant matching)`);

    // Normalize: strip spaces, lowercase for comparison
    // Input could be "7:45pm" or "7:45 PM" — normalize to "7:45pm"
    const normalizedTime = time.toLowerCase().replace(/\s+/g, '');

    // Generate multiple format variants for matching:
    // "7:45pm", "7:45 pm", "7:45 PM", "7:45PM"
    const match = normalizedTime.match(/^(\d{1,2}:\d{2})(am|pm)$/);
    const variants: string[] = [time];
    if (match) {
      const [, timePart, ampm] = match;
      variants.push(
        `${timePart}${ampm}`,           // 7:45pm
        `${timePart} ${ampm}`,           // 7:45 pm
        `${timePart} ${ampm.toUpperCase()}`, // 7:45 PM
        `${timePart}${ampm.toUpperCase()}`,  // 7:45PM
      );
    }

    // Moxie renders time slots as div/span elements, not buttons.
    // Use page.evaluate to find and click, matching the scraper's approach.
    const clicked = await page.evaluate((searchTime: string) => {
      const normalized = searchTime.toLowerCase().replace(/\s+/g, '');
      const allElements = Array.from(document.querySelectorAll('button, div, span'));

      for (const el of allElements) {
        const text = el.textContent?.trim() || '';
        const timeMatch = text.match(/^(\d{1,2}:\d{2}\s*(am|pm))$/i);
        if (!timeMatch) continue;

        const elTime = timeMatch[1].toLowerCase().replace(/\s+/g, '');
        if (elTime !== normalized) continue;

        // Verify it's a visible, positioned element (right side of page, like scraper checks)
        const rect = (el as HTMLElement).getBoundingClientRect();
        if (rect.width > 0 && rect.height > 0) {
          (el as HTMLElement).click();
          return { success: true, text: timeMatch[1], tag: el.tagName };
        }
      }

      // Collect what's actually on the page for debugging
      const found = allElements
        .map(el => ({ text: el.textContent?.trim() || '', tag: el.tagName }))
        .filter(e => /\d{1,2}:\d{2}\s*(am|pm)/i.test(e.text))
        .slice(0, 10);

      return { success: false, found };
    }, normalizedTime);

    if (clicked.success) {
      await this.delay(1000);
      logger.info(`Selected time: ${(clicked as any).text} (${(clicked as any).tag} element)`);
      return;
    }

    // Log what time elements were actually found
    logger.error(`Time slot "${time}" not found. Elements with time patterns: ${JSON.stringify((clicked as any).found)}`);

    throw new Error(`Time slot "${time}" not found or unavailable`);
  }

  /**
   * Step 4: Fill contact details
   * Moxie's "Your information" step has: first name, last name, email, phone
   */
  /**
   * Fill any visible contact fields on the current page.
   * Moxie shows fields progressively — phone first (Step 3 "Enter account details"),
   * then name/email after phone lookup (Step 4 for new clients).
   * This method fills whatever is visible and returns what was filled.
   */
  private async fillVisibleContactFields(page: Page, lead: LeadInfo): Promise<{phone: boolean, firstName: boolean, lastName: boolean, email: boolean}> {
    const filled = { phone: false, firstName: false, lastName: false, email: false };

    // Log visible inputs
    const inputInfo = await page.evaluate(() => {
      return Array.from(document.querySelectorAll('input, textarea')).map(inp => ({
        tag: inp.tagName,
        type: (inp as HTMLInputElement).type,
        name: (inp as HTMLInputElement).name,
        id: inp.id,
        placeholder: (inp as HTMLInputElement).placeholder,
        ariaLabel: inp.getAttribute('aria-label'),
        visible: (inp as HTMLElement).offsetParent !== null,
      })).filter(i => i.visible);
    });
    logger.info(`Visible inputs: ${JSON.stringify(inputInfo)}`);

    // Fill phone
    filled.phone = await this.fillField(page, lead.phone, [
      'input[type="tel"]',
      'input[name*="phone" i]',
      'input[placeholder*="phone" i]',
    ], 'phone');

    // Fill first name
    filled.firstName = await this.fillField(page, lead.firstName, [
      'input[name*="first" i]',
      'input[placeholder*="first" i]',
      'input[id*="first" i]',
      'input[aria-label*="first" i]',
    ], 'first name');

    // Fill last name
    filled.lastName = await this.fillField(page, lead.lastName, [
      'input[name*="last" i]',
      'input[placeholder*="last" i]',
      'input[id*="last" i]',
      'input[aria-label*="last" i]',
    ], 'last name');

    // If no named fields, try generic text inputs
    if (!filled.firstName && !filled.lastName) {
      const textInputs = page.locator('input[type="text"]:visible');
      const count = await textInputs.count();
      if (count >= 1) {
        await textInputs.nth(0).fill(lead.firstName);
        filled.firstName = true;
        logger.info(`Filled generic text input 1 with first name: ${lead.firstName}`);
      }
      if (count >= 2) {
        await textInputs.nth(1).fill(lead.lastName);
        filled.lastName = true;
        logger.info(`Filled generic text input 2 with last name: ${lead.lastName}`);
      }
    }

    // Fill email
    filled.email = await this.fillField(page, lead.email, [
      'input[type="email"]',
      'input[name*="email" i]',
      'input[placeholder*="email" i]',
      'input[id*="email" i]',
      'input[aria-label*="email" i]',
    ], 'email');

    return filled;
  }

  /**
   * Try multiple selectors to fill a form field. Returns true if filled.
   */
  private async fillField(page: Page, value: string, selectors: string[], fieldName: string): Promise<boolean> {
    for (const selector of selectors) {
      try {
        const el = page.locator(selector).first();
        if (await el.isVisible({ timeout: 1000 })) {
          await el.fill(value);
          logger.info(`Filled ${fieldName}: ${value} (via ${selector})`);
          return true;
        }
      } catch {
        // Try next selector
      }
    }
    logger.warn(`Could not find ${fieldName} field`);
    return false;
  }

  /**
   * Navigate from contact details step through to payment page.
   * 
   * Moxie's actual flow after time selection:
   *   Step 3: "Enter account details" — phone number only
   *   Step 4: "Your information" — first name, last name, email (for new clients)  
   *   Step 5: Review / Confirm
   *   Step 6: Payment (card details / deposit)
   *
   * At each step: fill any visible contact fields, then click "Next step".
   * Stop when we detect card input elements or hit max iterations.
   */
  private async navigateToPaymentStep(page: Page, lead: LeadInfo): Promise<void> {
    const maxClicks = 8; // Safety limit
    for (let i = 0; i < maxClicks; i++) {
      await this.delay(2000); // Wait for page to settle
      
      // Check if we're on the payment step via card input elements
      // Be very specific — avoid matching hidden iframes or unrelated elements
      const cardSelectors = [
        'input[name*="card" i]',
        'input[placeholder*="card" i]', 
        'input[autocomplete*="cc-" i]',
        'iframe[title*="card" i]',
        'iframe[name*="card" i]',
        'iframe[src*="stripe" i]',
        'iframe[src*="square" i]',
        '[data-testid*="card" i]',
        '[class*="CardElement" i]',
        '[class*="card-element" i]',
      ];
      let hasCardInput = false;
      let matchedCardSelector = '';
      for (const sel of cardSelectors) {
        const count = await page.locator(sel).count();
        if (count > 0) {
          // Verify it's actually visible
          const isVisible = await page.locator(sel).first().isVisible().catch(() => false);
          if (isVisible) {
            hasCardInput = true;
            matchedCardSelector = sel;
            logger.info(`Card element detected: ${sel} (count=${count}, visible=true)`);
            break;
          } else {
            logger.info(`Card element found but NOT visible: ${sel} (count=${count})`);
          }
        }
      }
      
      // Only use VISIBLE card input elements as payment detection — no text matching
      // Text like "complete booking" appears on review pages (not payment)
      const pageText = await page.evaluate(() => document.body.innerText.toLowerCase());
      
      if (hasCardInput) {
        const paymentPageText = pageText.substring(0, 300).replace(/\n+/g, ' ').trim();
        logger.info(`Reached payment step after ${i} iterations (hasCardInput=true, selector=${matchedCardSelector})`);
        logger.info(`Payment page text: "${paymentPageText}"`);
        await this.delay(2000);
        return;
      }

      // Extract current step info
      const stepMatch = pageText.match(/step\s*(\d+)/i);
      const stepNum = stepMatch ? parseInt(stepMatch[1]) : 0;
      const truncatedText = pageText.substring(0, 600).replace(/\n+/g, ' ').trim();
      logger.info(`Navigation iteration ${i}: Step ${stepNum || '?'}, text: "${truncatedText.substring(0, 200)}..."`);

      // Fill any visible contact fields at this step
      const hasInputs = await page.locator('input:visible').count();
      if (hasInputs > 0) {
        logger.info(`Found ${hasInputs} visible inputs — attempting to fill contact fields`);
        const filled = await this.fillVisibleContactFields(page, lead);
        logger.info(`Filled: phone=${filled.phone}, firstName=${filled.firstName}, lastName=${filled.lastName}, email=${filled.email}`);
        await this.delay(500);
      }

      // Click "Next step" or equivalent navigation button
      let clicked = false;
      
      // Primary: look for "Next step" text
      try {
        const nextBtn = page.locator('button:has-text("Next step"), a:has-text("Next step")').first();
        if (await nextBtn.isVisible({ timeout: 2000 })) {
          await nextBtn.click({ force: true });
          clicked = true;
          logger.info(`Clicked "Next step" (iteration ${i})`);
        }
      } catch { /* try fallback */ }

      // Fallback: "Book appointment", "Confirm", "Continue", "Next", submit button
      if (!clicked) {
        const fallbackSelectors = [
          'button:has-text("Book appointment")',
          'button:has-text("Confirm")',
          'button:has-text("Continue")',
          'button:has-text("Next")',
          'button:has-text("Proceed")',
          'button[type="submit"]',
        ];
        for (const sel of fallbackSelectors) {
          try {
            const btn = page.locator(sel).first();
            if (await btn.isVisible({ timeout: 1000 })) {
              await btn.click({ force: true });
              clicked = true;
              logger.info(`Clicked "${sel}" (iteration ${i})`);
              break;
            }
          } catch { /* try next */ }
        }
      }

      if (!clicked) {
        logger.warn(`No navigation button found at iteration ${i} — stopping`);
        break;
      }

      // Wait for page transition
      await this.delay(2000);
      try {
        await page.waitForLoadState('domcontentloaded', { timeout: 5000 });
      } catch { /* continue anyway */ }
    }

    logger.info('Finished navigation — capturing current page as handoff');
  }

  /**
   * Start monitoring for booking outcome
   */
  private startOutcomeMonitoring(sessionId: string): void {
    const session = this.sessions.get(sessionId);
    if (!session || !session.page) {
      return;
    }

    logger.info(`BookingSession ${sessionId}: Starting outcome monitoring`);

    const startTime = Date.now();

    session.monitoringTimerId = setInterval(async () => {
      // Check for timeout
      if (Date.now() - startTime > this.config.monitoringTimeout) {
        logger.info(`BookingSession ${sessionId}: Monitoring timeout reached`);
        session.state = 'abandoned';
        session.outcome = 'timeout';
        session.updatedAt = new Date();
        session.completedAt = new Date();
        clearInterval(session.monitoringTimerId);
        await this.sendCallback(session);
        return;
      }

      // Check for outcome
      try {
        const outcome = await this.checkForOutcome(session.page!);
        if (outcome) {
          session.state = outcome.success ? 'completed' : 'failed';
          session.outcome = outcome.outcome;
          session.confirmationDetails = outcome.confirmationDetails;
          session.updatedAt = new Date();
          session.completedAt = new Date();

          logger.info(`BookingSession ${sessionId}: Outcome detected`, {
            outcome: outcome.outcome,
            confirmation: outcome.confirmationDetails,
          });

          clearInterval(session.monitoringTimerId);
          await this.sendCallback(session);
        }
      } catch (error) {
        logger.warn(`BookingSession ${sessionId}: Error checking outcome`, {
          error: error instanceof Error ? error.message : 'Unknown',
        });
      }
    }, this.config.monitoringPollInterval);
  }

  /**
   * Check for booking outcome on the page
   */
  private async checkForOutcome(page: Page): Promise<{
    success: boolean;
    outcome: BookingOutcome;
    confirmationDetails?: ConfirmationDetails;
  } | null> {
    try {
      const currentUrl = page.url();

      // Check URL patterns for success
      for (const pattern of this.outcomeIndicators.successUrlPatterns) {
        if (pattern.test(currentUrl)) {
          const confirmationDetails = await this.extractConfirmationDetails(page);
          return { success: true, outcome: 'success', confirmationDetails };
        }
      }

      const pageText = await page.evaluate(() => document.body.innerText);

      // Check text patterns for success
      for (const pattern of this.outcomeIndicators.successTextPatterns) {
        if (pattern.test(pageText)) {
          const confirmationDetails = await this.extractConfirmationDetails(page);
          return { success: true, outcome: 'success', confirmationDetails };
        }
      }

      // Check selectors for success
      for (const selector of this.outcomeIndicators.successSelectors) {
        try {
          const element = await page.$(selector);
          if (element) {
            const confirmationDetails = await this.extractConfirmationDetails(page);
            return { success: true, outcome: 'success', confirmationDetails };
          }
        } catch {
          // Selector not found, continue
        }
      }

      // Check text patterns for failure
      for (const pattern of this.outcomeIndicators.failureTextPatterns) {
        if (pattern.test(pageText)) {
          if (/payment\s+failed|card\s+declined|transaction\s+failed/i.test(pageText)) {
            return { success: false, outcome: 'payment_failed' };
          }
          if (/slot.*available|time.*taken/i.test(pageText)) {
            return { success: false, outcome: 'slot_unavailable' };
          }
          return { success: false, outcome: 'error' };
        }
      }

      // Check selectors for failure
      for (const selector of this.outcomeIndicators.failureSelectors) {
        try {
          const element = await page.$(selector);
          if (element) {
            return { success: false, outcome: 'error' };
          }
        } catch {
          // Selector not found, continue
        }
      }

      return null; // No outcome detected yet
    } catch {
      return null;
    }
  }

  /**
   * Extract confirmation details from the page
   */
  private async extractConfirmationDetails(page: Page): Promise<ConfirmationDetails> {
    const details: ConfirmationDetails = {};

    try {
      // Try to extract confirmation number
      const confirmationElement = await page.$('.confirmation-number, [data-testid="confirmation-number"]');
      if (confirmationElement) {
        details.confirmationNumber = await confirmationElement.textContent() || undefined;
      }

      // Extract from page text using regex
      const pageText = await page.evaluate(() => document.body.innerText);

      const confirmMatch = pageText.match(/confirmation\s*(number|#|code)[:\s]*([A-Z0-9-]+)/i);
      if (confirmMatch) {
        details.confirmationNumber = confirmMatch[2];
      }

      const timeMatch = pageText.match(/(\d{1,2}:\d{2}\s*(am|pm))/i);
      if (timeMatch) {
        details.appointmentTime = timeMatch[1];
      }

      details.rawText = pageText.slice(0, 500); // First 500 chars for debugging
    } catch (error) {
      logger.warn('Failed to extract confirmation details', {
        error: error instanceof Error ? error.message : 'Unknown',
      });
    }

    return details;
  }

  /**
   * Send callback to platform when outcome is detected
   */
  private async sendCallback(session: InternalSession): Promise<void> {
    if (!session.callbackUrl) {
      return;
    }

    try {
      const response = await fetch(session.callbackUrl, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          sessionId: session.id,
          state: session.state,
          outcome: session.outcome,
          confirmationDetails: session.confirmationDetails,
          error: session.errorMessage,
        }),
      });

      if (!response.ok) {
        logger.warn(`BookingSession ${session.id}: Callback failed`, {
          status: response.status,
        });
      } else {
        logger.info(`BookingSession ${session.id}: Callback sent successfully`);
      }
    } catch (error) {
      logger.error(`BookingSession ${session.id}: Callback error`, {
        error: error instanceof Error ? error.message : 'Unknown',
      });
    }
  }

  /**
   * Cleanup a session
   */
  private async cleanupSession(sessionId: string): Promise<void> {
    const session = this.sessions.get(sessionId);
    if (!session) {
      return;
    }

    if (session.monitoringTimerId) {
      clearInterval(session.monitoringTimerId);
    }
    if (session.cleanupTimerId) {
      clearTimeout(session.cleanupTimerId);
    }

    await session.context?.close().catch(() => {});
    session.context = undefined;
    session.page = undefined;

    // Keep session in map for status queries, but mark for delayed removal
    setTimeout(() => {
      this.sessions.delete(sessionId);
      logger.info(`BookingSession ${sessionId}: Removed from cache`);
    }, 5 * 60 * 1000); // Keep for 5 more minutes

    logger.info(`BookingSession ${sessionId}: Cleaned up`);
  }

  /**
   * Apply stealth measures to avoid bot detection
   */
  private async applyStealthMeasures(page: Page): Promise<void> {
    await page.addInitScript(() => {
      Object.defineProperty(navigator, 'webdriver', { get: () => false });
      Object.defineProperty(navigator, 'plugins', { get: () => [1, 2, 3, 4, 5] });
      Object.defineProperty(navigator, 'languages', { get: () => ['en-US', 'en'] });
      Object.defineProperty(navigator, 'platform', { get: () => 'Win32' });
    });
  }

  /**
   * Delay helper
   */
  private delay(ms: number): Promise<void> {
    return new Promise(resolve => setTimeout(resolve, ms));
  }

  /**
   * Get active session count
   */
  getActiveSessionCount(): number {
    return Array.from(this.sessions.values()).filter(
      s => !['completed', 'failed', 'abandoned', 'cancelled'].includes(s.state)
    ).length;
  }

  /**
   * List all sessions (for debugging)
   */
  listSessions(): BookingStatusResponse[] {
    return Array.from(this.sessions.values()).map(session => ({
      success: true,
      sessionId: session.id,
      state: session.state,
      outcome: session.outcome,
      confirmationDetails: session.confirmationDetails,
      error: session.errorMessage,
      createdAt: session.createdAt.toISOString(),
      updatedAt: session.updatedAt.toISOString(),
    }));
  }
}

// Singleton instance
let sessionManager: BookingSessionManager | null = null;

export async function getSessionManager(): Promise<BookingSessionManager> {
  if (!sessionManager) {
    sessionManager = new BookingSessionManager({
      headless: process.env.HEADLESS !== 'false',
    });
    await sessionManager.initialize();
  }
  return sessionManager;
}

export async function closeSessionManager(): Promise<void> {
  if (sessionManager) {
    await sessionManager.close();
    sessionManager = null;
  }
}
