/**
 * Manual test script for Forever 22 Med Spa Moxie booking widget
 *
 * This script tests the scraper against the real Forever 22 Moxie booking page
 * to verify it can:
 * 1. Navigate to the booking page
 * 2. Handle Moxie's multi-step flow
 * 3. Extract available time slots
 * 4. NOT actually book an appointment (dry run only)
 *
 * Usage:
 *   npx ts-node tests/manual/test-forever22.ts
 */

import { AvailabilityScraper } from '../../src/scraper';

const FOREVER_22_URL = 'https://app.joinmoxie.com/booking/forever-22';

async function testForever22() {
  console.log('🧪 Testing Forever 22 Med Spa Moxie Widget\n');
  console.log(`URL: ${FOREVER_22_URL}\n`);

  const scraper = new AvailabilityScraper({
    headless: false, // Set to false to watch the browser
    timeout: 60000,
    retries: 1,
  });

  try {
    console.log('⏳ Initializing browser...');
    await scraper.initialize();
    console.log('✅ Browser initialized\n');

    // Test with a date 7 days from now
    const testDate = new Date();
    testDate.setDate(testDate.getDate() + 7);
    const targetDate = testDate.toISOString().split('T')[0];

    console.log(`📅 Testing availability for: ${targetDate}`);
    console.log('⏳ Scraping availability...\n');

    const result = await scraper.scrapeAvailability({
      bookingUrl: FOREVER_22_URL,
      date: targetDate,
      timeout: 60000,
    });

    console.log('\n📊 Results:');
    console.log('─'.repeat(50));
    console.log(`Success: ${result.success}`);
    console.log(`Date: ${result.date}`);
    console.log(`Scraped at: ${result.scrapedAt}`);
    console.log(`Total slots: ${result.slots.length}`);

    if (result.error) {
      console.log(`\n❌ Error: ${result.error}`);
    }

    if (result.slots.length > 0) {
      console.log('\n⏰ Available Time Slots:');
      console.log('─'.repeat(50));

      const availableSlots = result.slots.filter((s) => s.available);
      const unavailableSlots = result.slots.filter((s) => !s.available);

      console.log(`\n✅ Available (${availableSlots.length}):`);
      availableSlots.forEach((slot) => {
        console.log(`  - ${slot.time}`);
      });

      if (unavailableSlots.length > 0) {
        console.log(`\n❌ Unavailable/Booked (${unavailableSlots.length}):`);
        unavailableSlots.slice(0, 5).forEach((slot) => {
          console.log(`  - ${slot.time}`);
        });
        if (unavailableSlots.length > 5) {
          console.log(`  ... and ${unavailableSlots.length - 5} more`);
        }
      }
    } else {
      console.log('\n⚠️  No time slots found');
    }

    console.log('\n' + '─'.repeat(50));
    console.log('✅ Test complete - NO BOOKING WAS MADE');
    console.log('   (Scraper only checks availability, does not book)');
  } catch (error) {
    console.error('\n❌ Test failed:', error);
    if (error instanceof Error) {
      console.error('Stack:', error.stack);
    }
  } finally {
    console.log('\n⏳ Closing browser...');
    await scraper.close();
    console.log('✅ Browser closed');
  }
}

// Run the test
testForever22().catch(console.error);
