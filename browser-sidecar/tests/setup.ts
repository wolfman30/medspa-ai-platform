// Jest setup file
import { closeScraper } from '../src/scraper';

// Increase timeout for browser tests
jest.setTimeout(30000);

// Clean up after all tests
afterAll(async () => {
  await closeScraper();
});

// Mock console methods to reduce noise in tests
global.console = {
  ...console,
  log: jest.fn(),
  debug: jest.fn(),
  info: jest.fn(),
  warn: jest.fn(),
  // Keep error for debugging
};
