/**
 * Specific scenario test: Book "Ablative Erbium Laser Resurfacing" on Mon/Thu after 4pm
 *
 * Customer preferences:
 * - Days: Mondays OR Thursdays only
 * - Time: After 4pm (16:00)
 * - Service: Ablative Erbium Laser Resurfacing (2 providers)
 * - Provider: No preference (first available)
 * - Mode: DRY RUN (no actual booking)
 */

import { AvailabilityScraper } from '../../src/scraper';
import { filterSlots } from '../../src/utils/slot-filter';

const FOREVER_22_URL = 'https://app.joinmoxie.com/booking/forever-22';

// Customer preferences
const CUSTOMER_PREFS = {
  service: 'Ablative Erbium Laser Resurfacing',
  daysOfWeek: [1, 4], // Monday=1, Thursday=4
  afterTime: '16:00', // 4pm
};

async function testSpecificScenario() {
  console.log('🎯 SPECIFIC BOOKING SCENARIO TEST\n');
  console.log('═'.repeat(60));
  console.log('📋 Customer Requirements:');
  console.log(`   Service: ${CUSTOMER_PREFS.service}`);
  console.log(`   Days: Mondays OR Thursdays only`);
  console.log(`   Time: After 4:00 PM`);
  console.log(`   Provider: No preference (first available)`);
  console.log(`   Mode: DRY RUN (availability check only - NO BOOKING)`);
  console.log('═'.repeat(60));
  console.log();

  const scraper = new AvailabilityScraper({
    headless: false, // Show browser so you can see what's happening
    timeout: 60000,
    retries: 1,
  });

  try {
    // Step 1: Initialize browser
    console.log('⏳ Step 1: Initializing browser...');
    await scraper.initialize();
    console.log('✅ Browser ready\n');

    // Step 2: Get available dates for current month
    const now = new Date();
    const currentYear = now.getFullYear();
    const currentMonth = now.getMonth() + 1;

    console.log(`⏳ Step 2: Scanning calendar for ${currentYear}-${currentMonth.toString().padStart(2, '0')}...`);
    console.log(`   (This will select "${CUSTOMER_PREFS.service}")`);

    const datesResult = await scraper.getAvailableDates(
      FOREVER_22_URL,
      currentYear,
      currentMonth,
      60000,
      CUSTOMER_PREFS.service // Pass the service name
    );

    if (!datesResult.success || datesResult.dates.length === 0) {
      console.log('❌ No available dates found for this month');
      return;
    }

    console.log(`✅ Found ${datesResult.dates.length} available dates\n`);

    // Step 3: Filter dates by customer day preference (Mon/Thu only)
    console.log('⏳ Step 3: Filtering dates by customer preference (Mon/Thu only)...');
    const filteredDates = datesResult.dates.filter((date) => {
      const parts = date.split('-');
      const year = parseInt(parts[0], 10);
      const month = parseInt(parts[1], 10) - 1;
      const day = parseInt(parts[2], 10);
      const dateObj = new Date(Date.UTC(year, month, day));
      const dayOfWeek = dateObj.getUTCDay();
      return CUSTOMER_PREFS.daysOfWeek.includes(dayOfWeek);
    });

    console.log(`✅ ${filteredDates.length} dates match (Mondays or Thursdays):\n`);
    filteredDates.forEach((date) => {
      const d = new Date(date + 'T12:00:00');
      const dayName = d.toLocaleDateString('en-US', { weekday: 'long' });
      console.log(`   📅 ${date} (${dayName})`);
    });
    console.log();

    if (filteredDates.length === 0) {
      console.log('❌ No Mondays or Thursdays available this month');
      return;
    }

    // Step 4: Check time slots for each qualifying date
    console.log('⏳ Step 4: Checking time slots for each qualifying date...\n');
    console.log('═'.repeat(60));

    let totalQualifyingSlots = 0;
    const qualifyingDates: Array<{ date: string; dayName: string; slots: string[] }> = [];

    for (const date of filteredDates) {
      const d = new Date(date + 'T12:00:00');
      const dayName = d.toLocaleDateString('en-US', { weekday: 'long' });

      console.log(`\n📅 ${date} (${dayName})`);
      console.log('─'.repeat(60));

      const slotsResult = await scraper.scrapeAvailability({
        bookingUrl: FOREVER_22_URL,
        date: date,
        serviceName: CUSTOMER_PREFS.service, // Pass the service name
        timeout: 60000,
      });

      if (!slotsResult.success) {
        console.log(`   ❌ Error: ${slotsResult.error}`);
        continue;
      }

      console.log(`   📊 Total slots found: ${slotsResult.slots.length}`);

      // Filter by time preference (after 4pm)
      const timeFilteredSlots = filterSlots(date, slotsResult.slots, {
        afterTime: CUSTOMER_PREFS.afterTime,
      });

      const availableAfter4pm = timeFilteredSlots.filter((s) => s.available);

      console.log(`   ⏰ Available slots after 4pm: ${availableAfter4pm.length}`);

      if (availableAfter4pm.length > 0) {
        console.log(`   ✅ QUALIFYING SLOTS:`);
        availableAfter4pm.forEach((slot) => {
          console.log(`      • ${slot.time}`);
        });
        totalQualifyingSlots += availableAfter4pm.length;
        qualifyingDates.push({
          date,
          dayName,
          slots: availableAfter4pm.map((s) => s.time),
        });
      } else {
        console.log(`   ❌ No slots available after 4pm`);
      }
    }

    // Step 5: Summary
    console.log('\n');
    console.log('═'.repeat(60));
    console.log('📊 FINAL RESULTS - CUSTOMER OPTIONS');
    console.log('═'.repeat(60));
    console.log();

    if (qualifyingDates.length === 0) {
      console.log('❌ NO QUALIFYING APPOINTMENTS FOUND');
      console.log();
      console.log('The service "Ablative Erbium Laser Resurfacing" has no');
      console.log('available slots on Mondays or Thursdays after 4pm this month.');
      console.log();
      console.log('💡 Recommendation: Suggest alternative days or times to customer');
    } else {
      console.log(`✅ FOUND ${totalQualifyingSlots} QUALIFYING TIME SLOTS`);
      console.log(`   Across ${qualifyingDates.length} dates that match customer preferences\n`);

      qualifyingDates.forEach(({ date, dayName, slots }, index) => {
        console.log(`${index + 1}. ${date} (${dayName})`);
        slots.forEach((slot) => {
          console.log(`   • ${slot}`);
        });
        console.log();
      });

      console.log('💬 SMS Message to Customer:');
      console.log('─'.repeat(60));
      console.log(`Great news! I found ${totalQualifyingSlots} available appointment${totalQualifyingSlots > 1 ? 's' : ''}`);
      console.log(`for Ablative Erbium Laser Resurfacing on your preferred days:`);
      console.log();
      qualifyingDates.slice(0, 3).forEach(({ date, dayName, slots }) => {
        const formattedDate = new Date(date + 'T12:00:00').toLocaleDateString('en-US', {
          month: 'short',
          day: 'numeric',
        });
        console.log(`${dayName}, ${formattedDate}: ${slots.join(', ')}`);
      });
      if (qualifyingDates.length > 3) {
        console.log(`...and ${qualifyingDates.length - 3} more date${qualifyingDates.length - 3 > 1 ? 's' : ''}`);
      }
      console.log();
      console.log('Which time works best for you?');
    }

    console.log();
    console.log('═'.repeat(60));
    console.log('🛡️  DRY RUN COMPLETE - NO BOOKING WAS MADE');
    console.log('   This was an availability check only.');
    console.log('   In production, we would:');
    console.log('   1. Wait for customer to select a time');
    console.log('   2. Collect deposit via Square');
    console.log('   3. Continue monitoring availability during payment');
    console.log('   4. Complete booking once deposit is received');
    console.log('═'.repeat(60));

  } catch (error) {
    console.error('\n❌ Test failed:', error);
    if (error instanceof Error) {
      console.error('Stack:', error.stack);
    }
  } finally {
    console.log('\n⏳ Closing browser...');
    await scraper.close();
    console.log('✅ Browser closed\n');
  }
}

// Run the test
testSpecificScenario().catch(console.error);
