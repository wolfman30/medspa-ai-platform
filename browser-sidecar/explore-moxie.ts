import { chromium } from 'playwright';

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

  // Step 1: Get all visible service categories/items
  console.log('\n=== STEP 1: SERVICE LIST ===');
  const services = await page.evaluate(() => {
    const items: string[] = [];
    // Get all text content that looks like services
    const elements = document.querySelectorAll('div, button, a, span, h1, h2, h3, h4, h5, p');
    for (const el of elements) {
      const text = (el as HTMLElement).innerText?.trim();
      if (text && text.length > 3 && text.length < 200 && !items.includes(text)) {
        // Only get direct text (not deeply nested)
        const directText = Array.from(el.childNodes)
          .filter(n => n.nodeType === 3)
          .map(n => n.textContent?.trim())
          .filter(Boolean)
          .join(' ');
        if (directText && directText.length > 3 && !items.includes(directText)) {
          items.push(directText);
        }
      }
    }
    return items;
  });
  console.log('Direct text items found:');
  services.forEach(s => console.log(`  - "${s}"`));

  // Get the full page text structure
  console.log('\n=== FULL PAGE TEXT ===');
  const fullText = await page.evaluate(() => document.body.innerText);
  console.log(fullText.substring(0, 3000));

  // Try searching for "Botox"
  console.log('\n=== STEP 2: SEARCH FOR "Botox" ===');
  const searchBox = page.locator('input[placeholder*="Search" i], input[placeholder*="search" i]').first();
  if (await searchBox.isVisible({ timeout: 3000 }).catch(() => false)) {
    console.log('Search box found! Typing "Botox"...');
    await searchBox.fill('Botox');
    await page.waitForTimeout(1500);
    const afterSearch = await page.evaluate(() => document.body.innerText);
    console.log('After search:');
    console.log(afterSearch.substring(0, 2000));
  } else {
    console.log('No search box found');
  }

  // Try searching for "Wrinkle"
  console.log('\n=== STEP 3: SEARCH FOR "Wrinkle" ===');
  if (await searchBox.isVisible({ timeout: 1000 }).catch(() => false)) {
    await searchBox.fill('');
    await page.waitForTimeout(500);
    await searchBox.fill('Wrinkle');
    await page.waitForTimeout(1500);
    const afterSearch = await page.evaluate(() => document.body.innerText);
    console.log('After search for "Wrinkle":');
    console.log(afterSearch.substring(0, 2000));
  }

  // Clear search and look at clickable items
  console.log('\n=== STEP 4: CLICKABLE SERVICE ITEMS ===');
  if (await searchBox.isVisible({ timeout: 1000 }).catch(() => false)) {
    await searchBox.fill('');
    await page.waitForTimeout(1000);
  }
  
  const clickables = await page.evaluate(() => {
    const results: Array<{text: string, tag: string, classes: string}> = [];
    const elements = document.querySelectorAll('button, [role="button"], a, div[class*="card"], div[class*="service"], div[class*="item"]');
    for (const el of elements) {
      const text = (el as HTMLElement).innerText?.trim();
      if (text && text.length > 3 && text.length < 300) {
        results.push({
          text: text.substring(0, 150),
          tag: el.tagName,
          classes: el.className?.toString().substring(0, 100) || '',
        });
      }
    }
    return results;
  });
  console.log('Clickable elements:');
  clickables.forEach(c => console.log(`  [${c.tag}] "${c.text}" (class: ${c.classes})`));

  await browser.close();
  console.log('\nDone!');
}

exploreMoxie().catch(console.error);
