const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');

const html = fs.readFileSync(path.join(__dirname, '..', 'index.html'), 'utf8');
const app = fs.readFileSync(path.join(__dirname, '..', 'app.js'), 'utf8');
const backend = fs.readFileSync(path.join(__dirname, '..', '..', 'ptt_test_control.go'), 'utf8');

test('PTT test control is present and limited to non-VOX modes', () => {
  assert.match(html, /id="pttTestButton"/);
  assert.match(app, /show\(\$\('pttTestRow'\), mode !== 'vox'\)/);
  assert.match(app, /StartPTTTest\(pttTestConfigFromForm\(\)\)/);
});

test('PTT test has a backend-enforced 15 second watchdog and immediate stop method', () => {
  assert.match(backend, /const pttTestTimeout = 15 \* time\.Second/);
  assert.match(backend, /func \(a \*App\) StopPTTTest\(\) error/);
  assert.match(backend, /case <-timer\.C:/);
});

test('settings dialog cannot close through its backdrop and saves before closing', () => {
  assert.match(app, /dialog\.addEventListener\('cancel', e => e\.preventDefault\(\)\)/);
  assert.doesNotMatch(app, /dialog\.addEventListener\('click', e => \{ if \(e\.target === dialog\) dialog\.close\(\); \}\)/);
  assert.match(app, /await app\(\)\.ApplyAudioSettings\(cfg, currentRXModeID\(\)\)[\s\S]{0,180}settingsDialog'\)\.close\(\)/);
});

test('three interface languages are selectable in the top bar', () => {
  assert.match(html, /<select id="languageSelect"/);
  assert.match(html, /value="zh-CN"/);
  assert.match(html, /value="en"/);
  assert.match(html, /value="ja"/);
  assert.match(html, /<script src="i18n\.js"><\/script>/);
});
