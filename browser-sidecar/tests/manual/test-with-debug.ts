/**
 * Debug test to see which service is actually being selected
 */

import { AvailabilityScraper } from '../../src/scraper';
import * as fs from 'fs';
import * as path from 'path';

const FOREVER_22_URL = 'https://app.joinmoxie.com/booking/forever-22';

// Enable debug mode
process.env.DEBUG_SCRAPER = 'true';

async function debugServiceSelection() {
  console.log('🔍 DEBUG TEST - Service Selection Verification\n');
  console.log('This test will:');
  console.log('1. Take screenshots of each step');
  console.log('2. Show us which service is actually selected');
  console.log('3. Verify the service name\n');

  const scraper = new AvailabilityScraper({
    headless: false, // Show browser
    timeout: 60000,
    retries: 0,
  });

  try {
    await scraper.initialize();
    console.log('✅ Browser initialized\n');

    // Just get available dates (this will select the service)
    console.log('⏳ Navigating to booking page and selecting service...');
    console.log('   Watch the browser to see which service is clicked!\n');

    const result = await scraper.getAvailableDates(
      FOREVER_22_URL,
      2026,
      2,
      60000
    );

    console.log(`\n✅ Process complete`);
    console.log(`   Found ${result.dates.length} available dates\n`);

    // Check if screenshots were created
    const debugDir = path.join(process.cwd(), 'debug-screenshots');
    if (fs.existsSync(debugDir)) {
      const screenshots = fs.readdirSync(debugDir).filter(f => f.endsWith('.png'));
      console.log('📸 Screenshots saved:');
      screenshots.forEach(file => {
        console.log(`   ${file}`);
      });
      console.log(`\n📁 Location: ${debugDir}\n`);
      console.log('🔍 Check these screenshots to see:');
      console.log('   - 01-initial-page.png = The service list');
      console.log('   - 02-after-service-click.png = Which service was clicked');
      console.log('   - 03-provider-panel.png = Provider selection screen\n');
    }

  } catch (error) {
    console.error('❌ Error:', error);
  } finally {
    await scraper.close();
    console.log('✅ Browser closed');
  }
}

debugServiceSelection().catch(console.error);
