import assert from 'node:assert/strict';
import { chromium } from 'playwright';
const base = process.env.API_PAGES_URL || 'http://127.0.0.1:4174/api.html';
const browser = await chromium.launch({headless:true});
try{
 for(const width of [360,768,1440]){
  const page=await browser.newPage({viewport:{width,height:850},serviceWorkers:'block'});
  const errors=[];
  page.on('pageerror',err=>errors.push(err.message));
  await page.goto(base,{waitUntil:'networkidle'});
  await page.waitForSelector('.endpoint');
  const metrics=await page.locator('.metric').count();
  assert.equal(metrics,3,'missing catalogue summary');
  const names=await page.locator('#mcp-results .endpoint').count();
  assert.ok(names>=20,'MCP tool catalogue incomplete');
  assert.equal((await page.locator('#catalog-error').isVisible()),false);
  await page.locator('#filter-search').fill('ticker_24h');
  assert.equal(await page.locator('#provider-results .endpoint').count(),1);
  await page.locator('#filter-search').fill('');
  await page.locator('#filter-surface').selectOption('rest');
  assert.equal(await page.locator('#mcp').isVisible(),false);
  assert.equal(await page.locator('#rest').isVisible(),true);
  const widthInfo=await page.evaluate(()=>({doc:document.documentElement.scrollWidth,viewport:document.documentElement.clientWidth}));
  assert.ok(widthInfo.doc <= widthInfo.viewport+2,`horizontal overflow at ${width}px: ${JSON.stringify(widthInfo)}`);
  assert.deepEqual(errors,[],'browser console failures');
  console.log(`PASS Pages API help ${width}px`);
  await page.close();
 }
}finally{await browser.close();}
