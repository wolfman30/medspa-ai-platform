import app from './server';
import { getScraper } from './scraper';
import logger from './logger';

const PORT = parseInt(process.env.PORT || '3000', 10);
const HOST = process.env.HOST || '0.0.0.0';

async function main(): Promise<void> {
  try {
    // Pre-initialize the browser
    logger.info('Starting browser sidecar service...');
    await getScraper();

    // Start the HTTP server
    app.listen(PORT, HOST, () => {
      logger.info(`Browser sidecar running on http://${HOST}:${PORT}`);
      logger.info('Endpoints:');
      logger.info('  GET  /health              - Health check');
      logger.info('  GET  /ready               - Readiness probe');
      logger.info('  POST /api/v1/availability - Scrape booking availability');
      logger.info('  POST /api/v1/availability/batch - Batch scrape multiple dates');
    });

  } catch (error) {
    logger.error('Failed to start service', {
      error: error instanceof Error ? error.message : 'Unknown error',
    });
    process.exit(1);
  }
}

main();
