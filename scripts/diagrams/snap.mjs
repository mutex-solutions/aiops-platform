// Usage: node snap.mjs <puppeteer-core dir> <archify.html> <out.png> [scale]
// Opens a delivered archify HTML in headless Chrome and screenshots the diagram panel
// exactly as the browser renders it. Viewer chrome (toolbar, guides, finders) is hidden,
// so the image holds the diagram and its legend only.
import { pathToFileURL } from 'node:url'

const [pup, file, out, scale = '2'] = process.argv.slice(2)
const mod = await import(pathToFileURL(`${pup}/lib/esm/puppeteer/puppeteer-core.js`).href)
const browser = await mod.default.launch({
  executablePath: process.env.CHROME || '/usr/bin/google-chrome',
  headless: true,
  args: ['--no-sandbox', '--disable-setuid-sandbox'],
})
const page = await browser.newPage()
await page.emulateMediaFeatures([
  { name: 'prefers-color-scheme', value: 'light' },
  { name: 'prefers-reduced-motion', value: 'reduce' },
])
await page.setViewport({ width: 1760, height: 1200, deviceScaleFactor: Number(scale) })
await page.goto(pathToFileURL(file).href, { waitUntil: 'networkidle0', timeout: 120000 })
await page.addStyleTag({
  content: `.toolbar, .no-print, .guided-views, .header .pulse-dot { display: none !important; }
            *, *::before, *::after { animation: none !important; transition: none !important; }`,
})
const target = (await page.$('.diagram-container')) || (await page.$('svg[role="img"]'))
if (!target) {
  console.error('no diagram container found')
  await browser.close()
  process.exit(2)
}
await target.evaluate((e) => e.scrollIntoView({ block: 'start' }))
const box = await target.boundingBox()
const overflow = await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth)
await target.screenshot({ path: out })
console.log(JSON.stringify({ out, width: Math.round(box.width), height: Math.round(box.height), pageOverflowX: overflow }))
await browser.close()
