const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const path = require('node:path');

const source = fs.readFileSync(path.join(__dirname, '..', 'i18n.js'), 'utf8');
function createHarness(initial = 'zh-CN') {
  const store = new Map([['satsstv.language.v1', initial]]);
  const listeners = {};
  const document = {
    title: '', documentElement: { lang: '' },
    querySelectorAll: () => [], getElementById: () => null,
    addEventListener: (name, callback) => { listeners[name] = callback; },
  };
  const window = { dispatchEvent() {} };
  const sandbox = {
    document, window, localStorage: {
      getItem: key => store.get(key) ?? null,
      setItem: (key, value) => store.set(key, value),
    },
    CustomEvent: class { constructor(type, init) { this.type = type; this.detail = init.detail; } },
    Node: { TEXT_NODE: 3 },
  };
  vm.runInNewContext(source, sandbox, { filename: 'i18n.js' });
  return { api: window.SATSSTVI18n, document, store };
}

test('i18n defaults to Chinese and has English/Japanese translations', () => {
  const h = createHarness();
  assert.equal(h.api.getLanguage(), 'zh-CN');
  assert.equal(h.api.t('settingsTitle'), '设备设置');
  assert.equal(h.api.t('rxLiveCapture'), '实时音频采集中');
  assert.equal(h.api.t('setAsBackground'), '设为底图');
  h.api.setLanguage('en');
  assert.equal(h.api.t('settingsTitle'), 'Device Settings');
  assert.equal(h.api.t('rxLiveCapture'), 'Capturing audio in real time');
  assert.equal(h.api.t('setAsBackground'), 'Set as Background');
  assert.equal(h.api.t('containsImageComponent'), 'Contains image layer');
  assert.equal(h.document.documentElement.lang, 'en');
  h.api.setLanguage('ja');
  assert.equal(h.api.t('settingsTitle'), 'デバイス設定');
  assert.equal(h.api.t('rxLiveCapture'), 'リアルタイム音声を受信中');
  assert.equal(h.api.t('setAsBackground'), '背景に設定');
  assert.equal(h.api.t('containsImageComponent'), '画像レイヤーを含む');
  assert.equal(h.document.documentElement.lang, 'ja-JP');
});

test('language selection persists and unknown language is rejected', () => {
  const h = createHarness();
  assert.equal(h.api.setLanguage('en'), true);
  assert.equal(h.store.get('satsstv.language.v1'), 'en');
  assert.equal(h.api.setLanguage('fr'), false);
  assert.equal(h.api.getLanguage(), 'en');
});

test('localized strings interpolate dynamic values', () => {
  const h = createHarness('en');
  assert.equal(h.api.t('pttTestRunning', { seconds: 7 }), 'PTT test active · auto-release in 7s');
});
