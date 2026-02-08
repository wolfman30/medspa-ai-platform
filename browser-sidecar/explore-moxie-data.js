const https = require('https');

function fetch(url) {
  return new Promise((resolve, reject) => {
    https.get(url, {
      headers: { 'User-Agent': 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36' }
    }, (res) => {
      let data = '';
      res.on('data', chunk => data += chunk);
      res.on('end', () => resolve(data));
    }).on('error', reject);
  });
}

async function main() {
  const html = await fetch('https://app.joinmoxie.com/booking/forever-22');
  
  // Extract __NEXT_DATA__
  const match = html.match(/<script id="__NEXT_DATA__"[^>]*>(.*?)<\/script>/s);
  if (!match) { console.log('No __NEXT_DATA__ found'); return; }
  
  const data = JSON.parse(match[1]);
  const props = data.props?.pageProps;
  
  if (!props) { console.log('No pageProps'); console.log(JSON.stringify(data).substring(0, 2000)); return; }

  // Print service categories and services
  console.log('=== MEDSPA INFO ===');
  console.log('Name:', props.medspa?.name || props.medspaName);
  
  // Look for services/categories in different locations
  const keys = Object.keys(props);
  console.log('\npageProps keys:', keys);
  
  // Print each key's type and preview
  for (const key of keys) {
    const val = props[key];
    const type = Array.isArray(val) ? `array[${val.length}]` : typeof val;
    const preview = JSON.stringify(val)?.substring(0, 200);
    console.log(`\n  ${key} (${type}): ${preview}`);
  }
  
  // Deep search for service/category arrays
  function findServices(obj, path = '') {
    if (!obj || typeof obj !== 'object') return;
    if (Array.isArray(obj)) {
      for (let i = 0; i < obj.length; i++) {
        const item = obj[i];
        if (item && typeof item === 'object' && (item.name || item.title || item.serviceName)) {
          console.log(`\n  [${path}[${i}]] name="${item.name || item.title || item.serviceName}" category="${item.category || item.categoryName || ''}" id="${item.id || ''}"`);
        }
        findServices(item, `${path}[${i}]`);
      }
    } else {
      for (const [k, v] of Object.entries(obj)) {
        if ((k.toLowerCase().includes('service') || k.toLowerCase().includes('categor') || k.toLowerCase().includes('package')) && Array.isArray(v)) {
          console.log(`\n=== Found array: ${path}.${k} (${v.length} items) ===`);
          for (const item of v.slice(0, 30)) {
            if (typeof item === 'object' && item) {
              console.log(`  - ${JSON.stringify(item).substring(0, 300)}`);
            }
          }
        }
        findServices(v, `${path}.${k}`);
      }
    }
  }
  
  console.log('\n\n=== DEEP SERVICE SEARCH ===');
  findServices(props);
}

main().catch(console.error);
