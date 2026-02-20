/**
 * Moxie GraphQL API Discovery Script
 * 
 * Walks through the full booking flow on Moxie's widget while capturing
 * every GraphQL request/response. This reveals the complete API surface
 * including input types, field names, and enum values.
 * 
 * Usage: npx ts-node capture-moxie-api.ts [slug]
 * Default slug: forever-22
 */

import { chromium, Page, Request, Response } from 'playwright';
import * as fs from 'fs';

const SLUG = process.argv[2] || 'forever-22';
const BOOKING_URL = `https://app.joinmoxie.com/booking/${SLUG}`;
const OUTPUT_FILE = `moxie-api-capture-${SLUG}-${new Date().toISOString().slice(0, 10)}.json`;

interface GraphQLCapture {
  timestamp: string;
  operationName: string;
  query: string;
  variables: any;
  response: any;
  url: string;
}

const captures: GraphQLCapture[] = [];

async function captureGraphQL(page: Page) {
  page.on('request', async (req: Request) => {
    if (!req.url().includes('graphql')) return;
    
    try {
      const postData = req.postDataJSON();
      if (!postData?.query) return;
      
      // Extract operation name from query
      const opMatch = postData.query.match(/(?:query|mutation)\s+(\w+)/);
      const opName = postData.operationName || opMatch?.[1] || 'unknown';
      
      console.log(`\n📡 GraphQL ${postData.query.startsWith('mutation') ? 'MUTATION' : 'QUERY'}: ${opName}`);
      console.log(`   Variables: ${JSON.stringify(postData.variables || {}).slice(0, 200)}`);
    } catch (e) {
      // ignore
    }
  });

  page.on('response', async (resp: Response) => {
    if (!resp.url().includes('graphql')) return;
    
    try {
      const req = resp.request();
      const postData = req.postDataJSON();
      if (!postData?.query) return;
      
      const body = await resp.json();
      const opMatch = postData.query.match(/(?:query|mutation)\s+(\w+)/);
      const opName = postData.operationName || opMatch?.[1] || 'unknown';
      
      const capture: GraphQLCapture = {
        timestamp: new Date().toISOString(),
        operationName: opName,
        query: postData.query,
        variables: postData.variables || {},
        response: body,
        url: resp.url(),
      };
      
      captures.push(capture);
      
      if (body.errors) {
        console.log(`   ❌ Errors: ${JSON.stringify(body.errors).slice(0, 300)}`);
      } else {
        const dataKeys = Object.keys(body.data || {});
        console.log(`   ✅ Response keys: ${dataKeys.join(', ')}`);
        
        // Print response structure (types/fields) for discovery
        for (const key of dataKeys) {
          const val = body.data[key];
          if (val && typeof val === 'object') {
            printStructure(val, `   📋 ${key}`, 2);
          }
        }
      }
    } catch (e) {
      // ignore parse errors
    }
  });
}

function printStructure(obj: any, prefix: string, depth: number) {
  if (depth <= 0 || !obj) return;
  
  if (Array.isArray(obj)) {
    console.log(`${prefix}: Array[${obj.length}]`);
    if (obj.length > 0 && typeof obj[0] === 'object') {
      printStructure(obj[0], `${prefix}[0]`, depth - 1);
    }
    return;
  }
  
  const keys = Object.keys(obj);
  const summary = keys.map(k => {
    const v = obj[k];
    if (v === null) return `${k}:null`;
    if (Array.isArray(v)) return `${k}:Array[${v.length}]`;
    if (typeof v === 'object') return `${k}:{...}`;
    return `${k}:${typeof v}=${JSON.stringify(v).slice(0, 50)}`;
  });
  console.log(`${prefix}: { ${summary.join(', ')} }`);
}

async function walkBookingFlow(page: Page) {
  console.log(`\n🌐 Loading ${BOOKING_URL}...\n`);
  await page.goto(BOOKING_URL, { waitUntil: 'networkidle' });
  await page.waitForTimeout(3000);
  
  // Step 1: Service selection page
  console.log('\n=== STEP 1: SERVICE SELECTION ===');
  await page.waitForTimeout(2000);
  
  // Look for service categories and items
  const services = await page.$$eval('[class*="service"], [class*="Service"], button, a', els => 
    els.map(el => ({ text: el.textContent?.trim().slice(0, 80), tag: el.tagName, classes: el.className?.toString().slice(0, 60) }))
      .filter(e => e.text && e.text.length > 2 && e.text.length < 80)
  );
  console.log(`Found ${services.length} clickable elements`);
  
  // Try to find and click "Lip Filler" or similar
  const lipFillerBtn = await page.$('text=Lip Filler') || await page.$('text=lip filler');
  if (lipFillerBtn) {
    console.log('Clicking "Lip Filler"...');
    await lipFillerBtn.click();
    await page.waitForTimeout(3000);
  } else {
    // Try clicking into injectables category first
    const injectables = await page.$('text=Injectables') || await page.$('text=Filler');
    if (injectables) {
      console.log('Clicking "Injectables" category...');
      await injectables.click();
      await page.waitForTimeout(2000);
      
      const lipFiller2 = await page.$('text=Lip Filler');
      if (lipFiller2) {
        console.log('Clicking "Lip Filler"...');
        await lipFiller2.click();
        await page.waitForTimeout(3000);
      }
    }
  }
  
  // Step 2: Provider selection
  console.log('\n=== STEP 2: PROVIDER SELECTION ===');
  await page.waitForTimeout(2000);
  
  // Look for provider options
  const providerEls = await page.$$('[class*="provider"], [class*="Provider"], [data-testid*="provider"]');
  console.log(`Found ${providerEls.length} provider elements`);
  
  // Try clicking on Gale
  const galeBtn = await page.$('text=Gale') || await page.$('text=Gale Tesar');
  if (galeBtn) {
    console.log('Clicking "Gale Tesar"...');
    await galeBtn.click();
    await page.waitForTimeout(3000);
  } else {
    // Try "No Preference"
    const noPref = await page.$('text=No Preference') || await page.$('text=no preference');
    if (noPref) {
      console.log('Clicking "No Preference"...');
      await noPref.click();
      await page.waitForTimeout(3000);
    }
  }
  
  // Step 3: Date/time selection
  console.log('\n=== STEP 3: DATE/TIME SELECTION ===');
  await page.waitForTimeout(3000);
  
  // Look for available date buttons
  const dateButtons = await page.$$('[class*="date"], [class*="Date"], [class*="calendar"], [class*="day"]');
  console.log(`Found ${dateButtons.length} date-related elements`);
  
  // Try clicking an available date
  const availableDate = await page.$('[class*="available"], [aria-label*="available"], button:not([disabled])');
  if (availableDate) {
    const dateText = await availableDate.textContent();
    console.log(`Clicking available date: ${dateText?.trim()}`);
    await availableDate.click();
    await page.waitForTimeout(3000);
  }
  
  // Look for time slots
  const timeSlots = await page.$$('[class*="time"], [class*="slot"], [class*="Slot"]');
  console.log(`Found ${timeSlots.length} time-related elements`);
  
  // Click first available time
  if (timeSlots.length > 0) {
    const timeText = await timeSlots[0].textContent();
    console.log(`Clicking time slot: ${timeText?.trim()}`);
    await timeSlots[0].click();
    await page.waitForTimeout(3000);
  }
  
  // Step 4: Contact info / checkout
  console.log('\n=== STEP 4: CONTACT/CHECKOUT PAGE ===');
  await page.waitForTimeout(2000);
  
  // Don't fill in real info, just capture what API calls were made
  
  // Take a screenshot for reference
  await page.screenshot({ path: `/tmp/moxie-booking-flow-${SLUG}.png`, fullPage: true });
  console.log(`\nScreenshot saved to /tmp/moxie-booking-flow-${SLUG}.png`);
}

async function main() {
  console.log('🔍 Moxie GraphQL API Discovery');
  console.log(`   Booking URL: ${BOOKING_URL}`);
  console.log(`   Output: ${OUTPUT_FILE}\n`);
  
  const browser = await chromium.launch({ 
    headless: true,
    args: ['--no-sandbox', '--disable-setuid-sandbox']
  });
  
  const context = await browser.newContext({
    userAgent: 'Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1',
    viewport: { width: 390, height: 844 },
  });
  
  const page = await context.newPage();
  
  // Set up GraphQL capture BEFORE navigation
  await captureGraphQL(page);
  
  try {
    await walkBookingFlow(page);
  } catch (e) {
    console.error(`\n⚠️ Flow interrupted: ${e}`);
  }
  
  // Save all captures
  const output = {
    slug: SLUG,
    url: BOOKING_URL,
    capturedAt: new Date().toISOString(),
    totalCaptures: captures.length,
    operations: captures.map(c => ({
      operation: c.operationName,
      query: c.query,
      variables: c.variables,
      responseStructure: summarizeResponse(c.response),
      fullResponse: c.response,
    })),
    // Extract unique operation signatures
    operationSummary: [...new Set(captures.map(c => c.operationName))].map(op => {
      const cap = captures.find(c => c.operationName === op)!;
      return {
        name: op,
        type: cap.query.trim().startsWith('mutation') ? 'mutation' : 'query',
        query: cap.query,
        variableKeys: Object.keys(cap.variables || {}),
      };
    }),
  };
  
  fs.writeFileSync(OUTPUT_FILE, JSON.stringify(output, null, 2));
  console.log(`\n💾 Saved ${captures.length} captures to ${OUTPUT_FILE}`);
  
  // Print summary
  console.log('\n=== API SURFACE SUMMARY ===');
  for (const op of output.operationSummary) {
    console.log(`\n${op.type.toUpperCase()}: ${op.name}`);
    console.log(`  Variables: ${op.variableKeys.join(', ')}`);
    console.log(`  Query:\n${op.query.split('\n').map(l => '    ' + l).join('\n')}`);
  }
  
  await browser.close();
}

function summarizeResponse(resp: any): any {
  if (!resp?.data) return { errors: resp?.errors };
  const summary: any = {};
  for (const [key, val] of Object.entries(resp.data)) {
    summary[key] = describeType(val, 3);
  }
  return summary;
}

function describeType(val: any, depth: number): any {
  if (depth <= 0) return '...';
  if (val === null || val === undefined) return 'null';
  if (typeof val !== 'object') return typeof val;
  if (Array.isArray(val)) {
    return val.length > 0 ? [`Array[${val.length}] of ${JSON.stringify(describeType(val[0], depth - 1))}`] : 'empty[]';
  }
  const obj: any = {};
  for (const [k, v] of Object.entries(val)) {
    obj[k] = describeType(v, depth - 1);
  }
  return obj;
}

main().catch(console.error);
