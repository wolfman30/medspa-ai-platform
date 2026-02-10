import express, { Request, Response, NextFunction } from 'express';
import { getScraper, closeScraper } from './scraper';
import { getSessionManager, closeSessionManager } from './booking-session';
import {
  AvailabilityRequestSchema,
  CalendarSlotsRequestSchema,
  BookingStartRequestSchema,
  HealthResponse,
  AvailabilityResponse,
} from './types';
import logger from './logger';
import { ZodError } from 'zod';

const app = express();
app.use(express.json());

const startTime = Date.now();
const VERSION = process.env.VERSION || '1.0.0';

// Request logging middleware
app.use((req: Request, res: Response, next: NextFunction) => {
  const start = Date.now();
  res.on('finish', () => {
    const duration = Date.now() - start;
    logger.info(`${req.method} ${req.path}`, {
      status: res.statusCode,
      duration: `${duration}ms`,
    });
  });
  next();
});

// Health check endpoint
app.get('/health', async (_req: Request, res: Response) => {
  try {
    const scraper = await getScraper();
    const response: HealthResponse = {
      status: scraper.isReady() ? 'ok' : 'degraded',
      version: VERSION,
      browserReady: scraper.isReady(),
      uptime: Math.floor((Date.now() - startTime) / 1000),
    };
    res.json(response);
  } catch (error) {
    res.status(503).json({
      status: 'error',
      version: VERSION,
      browserReady: false,
      uptime: Math.floor((Date.now() - startTime) / 1000),
      error: error instanceof Error ? error.message : 'Unknown error',
    });
  }
});

// Readiness probe
app.get('/ready', async (_req: Request, res: Response) => {
  try {
    const scraper = await getScraper();
    if (scraper.isReady()) {
      res.status(200).json({ ready: true });
    } else {
      res.status(503).json({ ready: false });
    }
  } catch {
    res.status(503).json({ ready: false });
  }
});

// Main availability endpoint
app.post('/api/v1/availability', async (req: Request, res: Response) => {
  try {
    // Validate request
    const validatedRequest = AvailabilityRequestSchema.parse(req.body);

    logger.info('Availability request received', {
      url: validatedRequest.bookingUrl,
      date: validatedRequest.date,
    });

    // Get scraper and fetch availability
    const scraper = await getScraper();
    const result: AvailabilityResponse = await scraper.scrapeAvailability(validatedRequest);

    if (result.success) {
      res.json(result);
    } else {
      res.status(502).json(result); // Bad Gateway for upstream scraping failures
    }

  } catch (error) {
    if (error instanceof ZodError) {
      logger.warn('Invalid request', { errors: error.errors });
      res.status(400).json({
        success: false,
        error: 'Invalid request',
        details: error.errors,
      });
      return;
    }

    logger.error('Availability request failed', {
      error: error instanceof Error ? error.message : 'Unknown error',
    });

    res.status(500).json({
      success: false,
      error: error instanceof Error ? error.message : 'Internal server error',
    });
  }
});

// Batch availability endpoint (for multiple dates)
// Uses session reuse: service selection happens once, then calendar navigation per date
app.post('/api/v1/availability/batch', async (req: Request, res: Response) => {
  try {
    const { bookingUrl, dates, serviceName, providerName, timeout } = req.body;

    if (!bookingUrl || !Array.isArray(dates) || dates.length === 0) {
      res.status(400).json({
        success: false,
        error: 'bookingUrl and dates array are required',
      });
      return;
    }

    if (dates.length > 31) {
      res.status(400).json({
        success: false,
        error: 'Maximum 31 dates per batch request',
      });
      return;
    }

    logger.info(`Batch availability request: ${dates.length} dates`, { bookingUrl, serviceName });

    const scraper = await getScraper();
    const resultMap = await scraper.scrapeMultipleDates(
      bookingUrl,
      dates,
      serviceName,
      providerName,
      timeout || 30000,
    );

    // Convert map to ordered array matching input dates
    const results: AvailabilityResponse[] = dates.map(d => resultMap.get(d)!);

    res.json({
      success: true,
      results,
    });

  } catch (error) {
    logger.error('Batch availability request failed', {
      error: error instanceof Error ? error.message : 'Unknown error',
    });

    res.status(500).json({
      success: false,
      error: error instanceof Error ? error.message : 'Internal server error',
    });
  }
});

// Smart calendar search: one session scans multiple months
app.post('/api/v1/availability/calendar-slots', async (req: Request, res: Response) => {
  try {
    const validatedRequest = CalendarSlotsRequestSchema.parse(req.body);

    logger.info('Calendar slots request received', {
      url: validatedRequest.bookingUrl,
      maxMonths: validatedRequest.maxMonths,
      maxSlots: validatedRequest.maxSlots,
    });

    const scraper = await getScraper();
    const result = await scraper.scrapeCalendarSlots(validatedRequest);

    if (result.success) {
      res.json(result);
    } else {
      res.status(502).json(result);
    }
  } catch (error) {
    if (error instanceof ZodError) {
      logger.warn('Invalid calendar-slots request', { errors: error.errors });
      res.status(400).json({
        success: false,
        error: 'Invalid request',
        details: error.errors,
      });
      return;
    }

    logger.error('Calendar slots request failed', {
      error: error instanceof Error ? error.message : 'Unknown error',
    });

    res.status(500).json({
      success: false,
      error: error instanceof Error ? error.message : 'Internal server error',
    });
  }
});

// ============================================================================
// BOOKING SESSION ENDPOINTS
// ============================================================================

// Start a new booking session
app.post('/api/v1/booking/start', async (req: Request, res: Response) => {
  try {
    const validatedRequest = BookingStartRequestSchema.parse(req.body);

    logger.info('Booking session start request', {
      url: validatedRequest.bookingUrl,
      date: validatedRequest.date,
      time: validatedRequest.time,
    });

    const sessionManager = await getSessionManager();
    const result = await sessionManager.startSession(validatedRequest);

    if (result.success) {
      res.status(201).json(result);
    } else {
      res.status(400).json(result);
    }

  } catch (error) {
    if (error instanceof ZodError) {
      logger.warn('Invalid booking request', { errors: error.errors });
      res.status(400).json({
        success: false,
        error: 'Invalid request',
        details: error.errors,
      });
      return;
    }

    logger.error('Booking start failed', {
      error: error instanceof Error ? error.message : 'Unknown error',
    });

    res.status(500).json({
      success: false,
      error: error instanceof Error ? error.message : 'Internal server error',
    });
  }
});

// Get handoff URL for a booking session
app.get('/api/v1/booking/:sessionId/handoff-url', async (req: Request, res: Response) => {
  try {
    const { sessionId } = req.params;

    const sessionManager = await getSessionManager();
    const result = await sessionManager.getHandoffUrl(sessionId);

    if (result.success) {
      res.json(result);
    } else if (result.error === 'Session not found') {
      res.status(404).json(result);
    } else {
      res.status(400).json(result);
    }

  } catch (error) {
    logger.error('Get handoff URL failed', {
      error: error instanceof Error ? error.message : 'Unknown error',
    });

    res.status(500).json({
      success: false,
      error: error instanceof Error ? error.message : 'Internal server error',
    });
  }
});

// Get booking session status
app.get('/api/v1/booking/:sessionId/status', async (req: Request, res: Response) => {
  try {
    const { sessionId } = req.params;

    const sessionManager = await getSessionManager();
    const result = sessionManager.getSessionStatus(sessionId);

    if (result.success) {
      res.json(result);
    } else {
      res.status(404).json(result);
    }

  } catch (error) {
    logger.error('Get session status failed', {
      error: error instanceof Error ? error.message : 'Unknown error',
    });

    res.status(500).json({
      success: false,
      error: error instanceof Error ? error.message : 'Internal server error',
    });
  }
});

// Cancel a booking session
app.delete('/api/v1/booking/:sessionId', async (req: Request, res: Response) => {
  try {
    const { sessionId } = req.params;

    const sessionManager = await getSessionManager();
    const cancelled = await sessionManager.cancelSession(sessionId);

    if (cancelled) {
      res.json({ success: true, sessionId, message: 'Session cancelled' });
    } else {
      res.status(404).json({ success: false, error: 'Session not found' });
    }

  } catch (error) {
    logger.error('Cancel session failed', {
      error: error instanceof Error ? error.message : 'Unknown error',
    });

    res.status(500).json({
      success: false,
      error: error instanceof Error ? error.message : 'Internal server error',
    });
  }
});

// List all booking sessions (for debugging)
app.get('/api/v1/booking/sessions', async (_req: Request, res: Response) => {
  try {
    const sessionManager = await getSessionManager();
    const sessions = sessionManager.listSessions();

    res.json({
      success: true,
      activeCount: sessionManager.getActiveSessionCount(),
      sessions,
    });

  } catch (error) {
    logger.error('List sessions failed', {
      error: error instanceof Error ? error.message : 'Unknown error',
    });

    res.status(500).json({
      success: false,
      error: error instanceof Error ? error.message : 'Internal server error',
    });
  }
});

// Error handling middleware
app.use((err: Error, _req: Request, res: Response, _next: NextFunction) => {
  logger.error('Unhandled error', { error: err.message, stack: err.stack });
  res.status(500).json({
    success: false,
    error: 'Internal server error',
  });
});

// Graceful shutdown handler
async function shutdown(signal: string): Promise<void> {
  logger.info(`Received ${signal}, shutting down gracefully...`);
  await Promise.all([
    closeScraper(),
    closeSessionManager(),
  ]);
  process.exit(0);
}

process.on('SIGTERM', () => shutdown('SIGTERM'));
process.on('SIGINT', () => shutdown('SIGINT'));

export { app };
export default app;
