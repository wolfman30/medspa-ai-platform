const { chromium } = require('playwright');

async function exploreMoxie() {
  const browser = await chromium.launch({ headless: true, args: ['--no-sandbox'] });
  const context = await browser.newContext({
    userAgent: 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36',
    viewport: { width: 1920, height: 1080 },
  });
  const page = await context.newPage();

  console.log('Loading Forever 22 booking page...');
  await page.goto('https://app.joinmoxie.com/booking/forever-22', {
    waitUntil: 'networkidle',
    timeout: 30000,
  });
  await page.waitForTimeout(3000);

  console.log('\n=== FULL PAGE TEXT ===');
  const fullText = await page.evaluate(() => document.body.innerText);
  console.log(fullText.substring(0, 5000));

  // Try searching for "Botox"
  console.log('\n=== SEARCH FOR "Botox" ===');
  const searchVisible = await page.locator('input[placeholder*="Search" i]').first().isVisible({ timeout: 3000 }).catch(() => false);
  if (searchVisible) {
    const searchBox = page.locator('input[placeholder*="Search" i]').first();
    console.log('Search box found! Typing "Botox"...');
    await searchBox.fill('Botox');
    await page.waitForTimeout(1500);
    const afterSearch = await page.evaluate(() => document.body.innerText);
    console.log(afterSearch.substring(0, 3000));

    // Clear and try "Wrinkle"
    console.log('\n=== SEARCH FOR "Wrinkle Relaxer" ===');
    await searchBox.fill('');
    await page.waitForTimeout(500);
    await searchBox.fill('Wrinkle Relaxer');
    await page.waitForTimeout(1500);
    const afterSearch2 = await page.evaluate(() => document.body.innerText);
    console.log(afterSearch2.substring(0, 3000));
  } else {
    console.log('No search box found');
  }

  await browser.close();
  console.log('\nDone!');
}

exploreMoxie().catch(console.error);
