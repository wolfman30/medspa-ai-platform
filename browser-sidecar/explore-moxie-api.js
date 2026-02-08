// Try to discover Moxie's internal API by fetching the page and looking for API calls
const https = require('https');

function fetch(url, options = {}) {
  return new Promise((resolve, reject) => {
    const req = https.get(url, {
      headers: {
        'User-Agent': 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36',
        'Accept': 'text/html,application/json,*/*',
        ...options.headers,
      }
    }, (res) => {
      let data = '';
      res.on('data', chunk => data += chunk);
      res.on('end', () => resolve({ status: res.statusCode, data, headers: res.headers }));
    });
    req.on('error', reject);
  });
}

async function main() {
  // Try the booking page HTML for embedded data/config
  console.log('=== Fetching booking page HTML ===');
  const page = await fetch('https://app.joinmoxie.com/booking/forever-22');
  console.log('Status:', page.status);
  
  // Look for API endpoints, JSON data, or config in the HTML
  const html = page.data;
  
  // Find any API URLs
  const apiMatches = html.match(/api[^"'\s]*/gi) || [];
  console.log('\nAPI references found:', [...new Set(apiMatches)].slice(0, 20));
  
  // Find any JSON data embedded
  const jsonMatches = html.match(/__NEXT_DATA__|__APP_DATA__|window\.__[A-Z]+/g) || [];
  console.log('\nEmbedded data vars:', jsonMatches);
  
  // Find script sources
  const scriptSrcs = html.match(/src="([^"]*\.js[^"]*)"/g) || [];
  console.log('\nScript sources:', scriptSrcs.slice(0, 10));

  // Look for service/category data
  const serviceMatches = html.match(/service|category|wrinkle|botox|relaxer/gi) || [];
  console.log('\nService-related text matches:', [...new Set(serviceMatches)]);

  // Try common Moxie API patterns
  console.log('\n=== Trying Moxie API endpoints ===');
  
  const apiUrls = [
    'https://app.joinmoxie.com/api/booking/forever-22/services',
    'https://app.joinmoxie.com/api/v1/booking/forever-22/services',
    'https://api.joinmoxie.com/booking/forever-22/services',
    'https://app.joinmoxie.com/api/booking/forever-22',
  ];
  
  for (const url of apiUrls) {
    try {
      const res = await fetch(url);
      console.log(`${url} → ${res.status}`);
      if (res.status === 200 && res.data.length < 5000) {
        console.log('  Response:', res.data.substring(0, 500));
      }
    } catch (e) {
      console.log(`${url} → ERROR: ${e.message}`);
    }
  }
}

main().catch(console.error);
