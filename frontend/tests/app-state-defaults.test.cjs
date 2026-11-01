const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const path = require('node:path');

const source = fs.readFileSync(path.join(__dirname, '..', 'app.js'), 'utf8');

function extractFunction(name, nextMarker) {
  const start = source.indexOf(`function ${name}(`);
  assert.notEqual(start, -1, `${name} must exist`);
  const end = source.indexOf(nextMarker, start);
  assert.ok(end > start, `end marker for ${name} must exist`);
  return source.slice(start, end).trim();
}

test('Auto RX selection can recover with one click when UI selection is stale but backend RX is inactive', () => {
  const helper = extractFunction('isAutoRXActuallyRunning', 'async function applyRXSelection');
  const context = { rxEnabled: true, autoSelected: true, audioState: { rxActive: false } };
  vm.runInNewContext(`${helper}\n${extractFunction('shouldEnableAutoRX', 'async function applyRXSelection')}\nthis.result = shouldEnableAutoRX();`, context);
  assert.equal(context.result, true);

  context.audioState.rxActive = true;
  vm.runInNewContext('this.result = shouldEnableAutoRX();', context);
  assert.equal(context.result, false, 'a genuinely active Auto receiver should toggle off');
});

test('template switching refreshes the canvas immediately and after the next animation frame', () => {
  const start = source.indexOf('function refreshTemplatePreview(');
  const end = source.indexOf('function renderTemplates(', start);
  assert.ok(start >= 0 && end > start, 'template refresh/switch functions must exist');
  const functions = source.slice(start, end);
  const frames = [];
  const calls = [];
  const elements = { templateName: { value: '' } };
  const context = {
    templateRenderRevision: 0,
    selectedTemplateSlot: 0,
    templateSlots: [
      { name: 'CQ', objects: [{ type: 'text', text: 'CQ' }] },
      { name: 'RST', objects: [{ type: 'text', text: '{HISCALL}' }] },
    ],
    components: [],
    selectedComponent: 0,
    persistentTXBackground: null,
    t: key => key,
    $: id => elements[id] || (elements[id] = { value: '' }),
    templateData: value => value.objects.map(object => ({ ...object })),
    isBackgroundComponent: object => object?.type === 'background',
    copyBackground: value => value ? { ...value } : null,
    persistTXBackgroundNow: () => Promise.resolve(),
    updateFontSizeLimits: () => calls.push('font'),
    renderTemplates: () => calls.push('templates'),
    renderComponentList: () => calls.push('components'),
    renderCanvas: () => calls.push('canvas'),
    requestAnimationFrame: callback => frames.push(callback),
    setStatus: () => {},
    templateDisplayName: name => name,
  };
  vm.runInNewContext(`${functions}\nthis.load = loadTemplateAt;`, context);
  context.load(1);
  assert.equal(context.selectedTemplateSlot, 1);
  assert.equal(context.components[0].text, '{HISCALL}');
  assert.deepEqual(calls.filter(call => call === 'canvas'), ['canvas']);
  assert.equal(frames.length, 1, 'a post-layout canvas refresh must be scheduled');
  frames[0]();
  assert.deepEqual(calls.filter(call => call === 'canvas'), ['canvas', 'canvas']);
});

test('station template tokens default to N0CALL, JJ00, and ToCall', () => {
  const defaultsMatch = source.match(/const tokenDefaults = (\{[^\n]+\});/);
  assert.ok(defaultsMatch, 'tokenDefaults must exist');
  const defaults = vm.runInNewContext(`(${defaultsMatch[1]})`);
  assert.equal(defaults.MYCALL, 'N0CALL');
  assert.equal(defaults.LOCATOR, 'JJ00');
  assert.equal(defaults.HISCALL, 'ToCall');
});

test('workbench and settings station fields expose the requested default values', () => {
  const html = fs.readFileSync(path.join(__dirname, '..', 'index.html'), 'utf8');
  assert.match(html, /id="myCall"[^>]*value="N0CALL"/);
  assert.match(html, /id="locator"[^>]*value="JJ00"/);
  assert.match(html, /id="settingsMyCall"[^>]*value="N0CALL"/);
  assert.match(html, /id="settingsLocator"[^>]*value="JJ00"/);
});

test('HISCALL token resolves to ToCall when the QSO HisCall field is empty', () => {
  const defaultsMatch = source.match(/const tokenDefaults = (\{[^\n]+\});/);
  const context = {
    tokenDefaults: vm.runInNewContext(`(${defaultsMatch[1]})`),
    VALUE_IDS: ['myCall', 'hisCall', 'qth', 'locator', 'report', 'message'],
    dateTokens: () => ({ DATE: '2026-11-02', TIME: '12:00:00' }),
    $: id => ({ value: id === 'myCall' ? 'N0CALL' : id === 'locator' ? 'JJ00' : '' }),
  };
  const valuesFn = source.slice(source.indexOf('function valuesForTokens('), source.indexOf('function activeRXMode('));
  vm.runInNewContext(`${valuesFn}\nthis.values = valuesForTokens();`, context);
  assert.equal(context.values.HISCALL, 'ToCall');
  assert.equal(context.values.MYCALL, 'N0CALL');
  assert.equal(context.values.LOCATOR, 'JJ00');
});

test('RX startup uses the explicit audio initializer rather than only a toggle operation', () => {
  const start = source.indexOf('async function initializeAudioRX()');
  const end = source.indexOf('// UI events.', start);
  assert.ok(start >= 0 && end > start);
  const init = source.slice(start, end);
  assert.match(init, /app\(\)\.InitializeAudio\(currentRXModeID\(\)\)/);
  assert.match(init, /if \(rxEnabled && !state\?\.rxActive\)/);
});


test('renderTemplates renders all four slots including the blank preset without throwing', () => {
  const start = source.indexOf('function renderTemplates()');
  const end = source.indexOf('async function saveCurrentTemplate()', start);
  assert.ok(start >= 0 && end > start);
  const fn = source.slice(start, end);
  const host = {
    children: [],
    innerHTML: 'stale',
    appendChild(child) { this.children.push(child); },
  };
  const makeNode = () => ({
    children: [], className: '', textContent: '', type: '',
    append(...children) { this.children.push(...children); },
    addEventListener() {},
  });
  const context = {
    templateSlots: [
      { name: 'CQ', objects: [{ type: 'text', text: 'CQ SSTV' }] },
      { name: 'RST', objects: [{ type: 'text', text: '{HISCALL}' }] },
      { name: '73', objects: [{ type: 'text', text: '73' }] },
      { name: 'Blank', objects: [] },
    ],
    selectedTemplateSlot: 0,
    $: () => host,
    document: { createElement: makeNode },
    isImageComponent: object => ['image', 'last_rx', 'background'].includes(object?.type),
    t: key => key,
    templateDisplayName: name => name,
    loadTemplateAt() {},
  };
  vm.runInNewContext(`${fn}\nrenderTemplates();`, context);
  assert.equal(host.children.length, 4);
  assert.equal(host.children[3].children[0].children[0].textContent, 'blankTemplate');
});
