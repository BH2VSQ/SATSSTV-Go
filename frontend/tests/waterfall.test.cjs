const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');

const appSource = fs.readFileSync(path.join(__dirname, '..', 'app.js'), 'utf8');
const start = appSource.indexOf('function drawWaterfall(');
const end = appSource.indexOf('\nfunction drawIdleSpectrum', start);
assert.notEqual(start, -1, 'drawWaterfall() must exist in app.js');
assert.notEqual(end, -1, 'drawWaterfall() must be followed by drawIdleSpectrum()');
const drawWaterfallSource = appSource.slice(start, end);

function createHarness(nextRow) {
  const calls = [];
  const ctx = {
    save() {}, restore() {}, beginPath() {}, clip() {}, rect() {},
    drawImage(...args) { calls.push(args); },
    set imageSmoothingEnabled(_value) {},
  };
  const sandbox = {
    MAX_WATERFALL_ROWS: 64,
    waterfallOff: { width: 2048, height: 64 },
    waterfallNextRow: nextRow,
  };
  vm.createContext(sandbox);
  vm.runInContext(drawWaterfallSource, sandbox);
  sandbox.drawWaterfall(ctx, 400, 80, 120, 17);
  return calls;
}

test('waterfall ring buffer keeps horizontal origin fixed and stitches rows', () => {
  for (const nextRow of [0, 1, 17, 32, 63]) {
    const calls = createHarness(nextRow);
    assert.equal(calls.length, nextRow === 0 ? 1 : 2, `draw count at ${nextRow}`);
    for (const call of calls) {
      assert.equal(call[1], 0, `source X must remain zero at row ${nextRow}`);
      assert.equal(call[3], 2048, `source width at row ${nextRow}`);
      assert.equal(call[5], 17, `destination X must remain fixed at row ${nextRow}`);
      assert.equal(call[7], 400, `destination width must remain fixed at row ${nextRow}`);
    }
    if (nextRow > 0) {
      const [older, newer] = calls;
      assert.equal(older[2], nextRow, 'older segment begins at ring pointer');
      assert.equal(newer[2], 0, 'newer segment begins at ring origin');
      assert.equal(older[4] + newer[4], 64, 'segments cover every ring row exactly once');
      assert.equal(older[6], 80, 'older segment starts at waterfall top');
      assert.ok(Math.abs(older[6] + older[8] - newer[6]) < 1e-9,
        'segments must meet without a vertical gap or overlap');
      assert.ok(Math.abs(newer[6] + newer[8] - 200) < 1e-9,
        'newer segment must end at waterfall bottom');
    }
  }
});
