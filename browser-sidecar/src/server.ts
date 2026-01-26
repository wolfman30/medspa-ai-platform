import express, { Request, Response, NextFunction } from 'express';
import { getScraper, closeScraper } from './scraper';
import { AvailabilityRequestSchema, HealthResponse, AvailabilityResponse } from './types';
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

    if (dates.length > 7) {
      res.status(400).json({
        success: false,
        error: 'Maximum 7 dates per batch request',
      });
      return;
    }

    const scraper = await getScraper();
    const results: AvailabilityResponse[] = [];

    for (const date of dates) {
      const result = await scraper.scrapeAvailability({
        bookingUrl,
        date,
        serviceName,
        providerName,
        timeout: timeout || 30000,
      });
      results.push(result);

      // Small delay between requests to avoid rate limiting
      await new Promise(resolve => setTimeout(resolve, 500));
    }

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
  await closeScraper();
  process.exit(0);
}

process.on('SIGTERM', () => shutdown('SIGTERM'));
process.on('SIGINT', () => shutdown('SIGINT'));

export { app };
export default app;
