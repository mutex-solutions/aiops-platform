// Usage: node shoot.mjs <puppeteer-core dir> <html file> <out dir> [viewport width] [selector]
// Screenshots every element matching the selector (default ".sheet .plate") as the browser renders it.
import { pathToFileURL } from 'node:url'

const [pup, file, out, width = '1400', selector = '.sheet'] = process.argv.slice(2)
const mod = await import(pathToFileURL(`${pup}/lib/esm/puppeteer/puppeteer-core.js`).href)
const puppeteer = mod.default
const browser = await puppeteer.launch({
  executablePath: '/usr/bin/google-chrome',
  headless: true,
  args: ['--no-sandbox', '--disable-setuid-sandbox'],
})
const page = await browser.newPage()
await page.setViewport({ width: Number(width), height: 1000, deviceScaleFactor: 1 })
await page.goto(pathToFileURL(file).href, { waitUntil: 'networkidle0', timeout: 120000 })
const sheets = await page.$$(selector)
let n = 0
for (const s of sheets) {
  const id = await s.evaluate((e) => e.id || '')
  const plate = (await s.$('.plate')) || s
  await plate.evaluate((e) => e.scrollIntoView())
  await plate.screenshot({ path: `${out}/${id || 'el-' + n}.png` })
  n++
}
console.log(JSON.stringify({ shots: n }))
await browser.close()
