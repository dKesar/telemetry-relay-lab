// Optional, run on the exhibition computer after starting relaylab serve.
// Requires Playwright with Chromium. The Go application itself needs neither.
const fs=require('node:fs/promises'),path=require('node:path');
const {chromium}=process.env.CODEX_PRIMARY_RUNTIME_NODE_MODULES?require(process.env.CODEX_PRIMARY_RUNTIME_NODE_MODULES+'/playwright'):require('playwright');
(async()=>{
  const output=process.env.QA_SCREENSHOT_DIR||'results/proof-captures',base=process.env.DEMO_URL||'http://127.0.0.1:8080';await fs.mkdir(output,{recursive:true});
  const browser=await chromium.launch({headless:true,executablePath:process.env.CHROME_BIN||undefined});
  try{
    const page=await browser.newPage({viewport:{width:1512,height:1080},deviceScaleFactor:1,reducedMotion:'reduce'});
    const cases=[['01-shared','shared_failure','availability',18000],['02-learning','unknown_topology','risk_aware',60000],['03-deadline','deadline_pressure','risk_aware',45000],['04-overload','overload','risk_aware',45000],['05-counterexample','weak_alternative','risk_aware',45000]];
    for(const [file,preset,left,t] of cases){await page.goto(`${base}/?preset=${preset}&left=${left}&right=adaptive&t=${t}&proof=1`);await page.waitForFunction(name=>!document.querySelector('#play').disabled&&document.querySelector('#captureBanner').textContent.includes(name),preset);await page.screenshot({path:path.join(output,file+'.png'),fullPage:true});console.log(file)}
    await page.goto(`${base}/research.html`);await page.screenshot({path:path.join(output,'06-series.png'),fullPage:true});
  }finally{await browser.close()}
})().catch(e=>{console.error(e);process.exit(1)});
