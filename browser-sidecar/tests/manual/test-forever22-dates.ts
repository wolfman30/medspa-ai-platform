/**
 * Test script to get available dates from Forever 22 Med Spa
 *
 * Usage:
 *   npx ts-node tests/manual/test-forever22-dates.ts
 */

import { AvailabilityScraper } from '../../src/scraper';

const FOREVER_22_URL = 'https://app.joinmoxie.com/booking/forever-22';

async function testForever22Dates() {
  console.log('🧪 Testing Forever 22 Available Dates\n');

  const scraper = new AvailabilityScraper({
    headless: false,
    timeout: 60000,
    retries: 1,
  });

  try {
    console.log('⏳ Initializing browser...');
    await scraper.initialize();
    console.log('✅ Browser initialized\n');

    // Get current month and next month
    const now = new Date();
    const currentYear = now.getFullYear();
    const currentMonth = now.getMonth() + 1; // 1-indexed

    console.log(`📅 Getting available dates for ${currentYear}-${currentMonth.toString().padStart(2, '0')}`);
    console.log('⏳ Scanning calendar...\n');

    const result = await scraper.getAvailableDates(
      FOREVER_22_URL,
      currentYear,
      currentMonth,
      60000
    );

    console.log('\n📊 Results:');
    console.log('─'.repeat(50));
    console.log(`Success: ${result.success}`);
    console.log(`Total available dates: ${result.dates.length}`);

    if (result.error) {
      console.log(`\n❌ Error: ${result.error}`);
    }

    if (result.dates.length > 0) {
      console.log('\n📅 Available Dates:');
      console.log('─'.repeat(50));
      result.dates.forEach((date) => {
        const d = new Date(date);
        const dayName = d.toLocaleDateString('en-US', { weekday: 'long' });
        console.log(`  ${date} (${dayName})`);
      });

      // Now try to get slots for the first available date
      if (result.dates.length > 0) {
        const testDate = result.dates[0];
        console.log(`\n⏳ Testing time slots for ${testDate}...`);

        const slotsResult = await scraper.scrapeAvailability({
          bookingUrl: FOREVER_22_URL,
          date: testDate,
          timeout: 60000,
        });

        console.log(`\n⏰ Time Slots for ${testDate}:`);
        console.log('─'.repeat(50));
        console.log(`Total slots: ${slotsResult.slots.length}`);

        if (slotsResult.slots.length > 0) {
          const availableSlots = slotsResult.slots.filter((s) => s.available);
          console.log(`✅ Available: ${availableSlots.length}`);
          availableSlots.slice(0, 10).forEach((slot) => {
            console.log(`  - ${slot.time}`);
          });
        }
      }
    } else {
      console.log('\n⚠️  No available dates found for this month');
    }

    console.log('\n' + '─'.repeat(50));
    console.log('✅ Test complete - NO BOOKING WAS MADE');
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

testForever22Dates().catch(console.error);
