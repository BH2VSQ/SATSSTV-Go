const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');

const source = fs.readFileSync(new URL('../app.js', `file://${__filename}`), 'utf8');
const eventStart = source.indexOf("window.runtime.EventsOn('rx:callsign',");
assert.notEqual(eventStart, -1, 'rx:callsign subscription must exist');
const callbackStart = source.indexOf('(data) => {', eventStart);
const nextEvent = source.indexOf("window.runtime.EventsOn('rx:preview'", callbackStart);
assert.ok(callbackStart > eventStart && nextEvent > callbackStart, 'callsign callback boundaries must be present');
const callbackSource = source.slice(callbackStart, nextEvent).trim().replace(/\);\s*$/, '');

function makeHarness() {
  const makeElement = () => ({
    value: '',
    textContent: '',
    title: '',
    dataset: {},
    dispatched: 0,
    dispatchEvent() { this.dispatched += 1; },
  });
  const elements = {
    rxCallsign: makeElement(),
    hisCall: makeElement(),
  };
  let status = '';
  const context = {
    $: (id) => elements[id],
    setStatus: (message) => { status = message; },
    t: (key, vars = {}) => key === 'callsignFilled'
      ? `已通过 ${vars.source} 识别到呼号 ${vars.callsign}，已填入对方呼号`
      : key === 'callsignPreserved'
        ? `已通过 ${vars.source} 识别到呼号 ${vars.callsign}；保留了已手动填写的对方呼号`
        : key,
    Event: class { constructor(type, options) { this.type = type; this.options = options; } },
  };
  const handler = vm.runInNewContext(`(${callbackSource})`, context);
  return { handler, elements, getStatus: () => status };
}

test('FSKID callsign automatically fills an empty HisCall field', () => {
  const h = makeHarness();
  h.handler({ callsign: 'BH2VSQ/P', source: 'MMSSTV FSKID' });
  assert.equal(h.elements.rxCallsign.textContent, 'BH2VSQ/P');
  assert.equal(h.elements.hisCall.value, 'BH2VSQ/P');
  assert.equal(h.elements.hisCall.dataset.fskidAuto, 'true');
  assert.equal(h.elements.hisCall.dispatched, 1);
  assert.match(h.getStatus(), /已填入对方呼号/);
});

test('a later FSKID result refreshes a value previously auto-filled by FSKID', () => {
  const h = makeHarness();
  h.handler({ callsign: 'BH2VSQ', source: 'MMSSTV FSKID' });
  h.handler({ callsign: 'BH2VSQ/P', source: 'MMSSTV FSKID' });
  assert.equal(h.elements.hisCall.value, 'BH2VSQ/P');
  assert.equal(h.elements.hisCall.dispatched, 2);
});

test('a manually entered HisCall is never overwritten by FSKID', () => {
  const h = makeHarness();
  h.handler({ callsign: 'BH2VSQ', source: 'MMSSTV FSKID' });
  h.elements.hisCall.value = 'JA1XYZ';
  delete h.elements.hisCall.dataset.fskidAuto; // mirrors the trusted user-input listener
  h.handler({ callsign: 'BH2VSQ/P', source: 'MMSSTV FSKID' });
  assert.equal(h.elements.hisCall.value, 'JA1XYZ');
  assert.match(h.getStatus(), /保留了已手动填写/);
});

test('malformed FSKID values are ignored', () => {
  const h = makeHarness();
  h.handler({ callsign: 'INVALID CALL', source: 'MMSSTV FSKID' });
  assert.equal(h.elements.rxCallsign.textContent, '');
  assert.equal(h.elements.hisCall.value, '');
});
