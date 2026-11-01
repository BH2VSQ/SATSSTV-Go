'use strict';

const $ = (id) => document.getElementById(id);
const app = () => window.go.main.App;
const SLOT_STORAGE = 'satsstv.modeSlots.v1';
const TX_SLOT_STORAGE = 'satsstv.txModeSlots.v1';
const TX_SLOT_ACTIVE_STORAGE = 'satsstv.txModeSlotActive.v1';
const TX_MODE_STORAGE = 'satsstv.txMode.v1'; // legacy single-mode TX selection
const RX_MODE_STORAGE = 'satsstv.rxMode.v1';
const VALUE_IDS = ['myCall', 'hisCall', 'qth', 'locator', 'report', 'message'];
const tokenDefaults = { MYCALL: 'N0CALL', HISCALL: 'N0TEST', QTH: 'EXAMPLE QTH', LOCATOR: 'AA00AA', REPORT: '595', MESSAGE: 'TEST MESSAGE', DATE: 'YYYY-MM-DD', TIME: 'HH:MM:SS' };

const defaultTemplateSlots = [
  { id: 'cq', name: 'CQ', objects: [
    { type: 'text', text: 'CQ SSTV', x: 6, y: 8, fontSize: 29, color: '#f5a9b8', bold: true },
    { type: 'text', text: 'DE {MYCALL}', x: 6, y: 24, fontSize: 19, color: '#ffffff', bold: true },
    { type: 'text', text: '{LOCATOR}', x: 6, y: 86, fontSize: 12, color: '#ffffff', bold: false },
  ]},
  { id: 'rst', name: 'RST', objects: [
    { type: 'text', text: '{HISCALL}', x: 6, y: 8, fontSize: 28, color: '#f5a9b8', bold: true },
    { type: 'text', text: 'RST {REPORT}', x: 6, y: 28, fontSize: 21, color: '#ffffff', bold: true },
    { type: 'text', text: 'FROM {MYCALL}', x: 6, y: 86, fontSize: 14, color: '#ffffff', bold: false },
  ]},
  { id: '73', name: '73', objects: [
    { type: 'text', text: '73', x: 7, y: 10, fontSize: 52, color: '#f5a9b8', bold: true },
    { type: 'text', text: 'BEST REGARDS', x: 7, y: 48, fontSize: 17, color: '#ffffff', bold: true },
    { type: 'text', text: '{MYCALL}', x: 7, y: 84, fontSize: 17, color: '#ffffff', bold: true },
  ]},
  { id: 'slot4', name: '空白模板', objects: [] },
];

let modes = [];
let slots = [];
let activeSlot = 0;
let autoSelected = true;
let rxEnabled = true;
let toneActive = false;
let peakSpectrum = [];
let txSlots = [];
let activeTXSlot = 0;
let selectedImagePath = '';
let sourceImage = null;
let components = [];
let selectedComponent = 0;
let albumEntries = [];
let pointerDrag = null;
let templateSlots = defaultTemplateSlots.map(t => ({ ...t, objects: t.objects.map(o => ({ ...o })) }));
let selectedTemplateSlot = 0;
const templateImageCache = new Map();
let contextTarget = null;
let stationSettings = { myCall: '', locator: '' };
let audioState = { connected: false, rxActive: false, txActive: false, sampleRate: 48000, pttMode: 'vox' };
let waterfallHistory = [];
let captureTelemetrySeen = false;
let latestRXImageData = '';
const MAX_WATERFALL_ROWS = 64;

function dateTokens() {
  const d = new Date(); const pad = (n) => String(n).padStart(2, '0');
  return { DATE: `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`, TIME: `${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}` };
}
function valuesForTokens() {
  const vals = { ...tokenDefaults, ...dateTokens() };
  for (const id of VALUE_IDS) vals[id === 'myCall' ? 'MYCALL' : id === 'hisCall' ? 'HISCALL' : id.toUpperCase()] = $(id)?.value.trim() || vals[id === 'myCall' ? 'MYCALL' : id === 'hisCall' ? 'HISCALL' : id.toUpperCase()];
  return vals;
}
function resolveTokens(text) {
  const vals = valuesForTokens();
  return String(text).replace(/\{([A-Z]+)\}/g, (whole, key) => vals[key] || whole);
}
function activeRXMode() { return modes.find((m) => m.id === slots[activeSlot]) || modes.find((m) => m.ready) || modes[0]; }
function activeTXMode() { return modes.find((m) => m.id === txSlots[activeTXSlot]) || modes.find((m) => m.id === 'robot36') || modes.find((m) => m.ready) || modes[0]; }
function setStatus(message, error = false) {
  $('txResult').textContent = message;
  $('txResult').classList.toggle('error', error);
  $('globalStatus').textContent = message;
}
function niceMode(id) { return modes.find((m) => m.id === id)?.name || id || '空'; }

const DEFAULT_MODE_SLOTS = ['robot36', 'robot72', 'scottie_s1', 'scottie_s2', 'martin_m1', 'martin_m2', 'sc2_180', 'pd120'];

function loadSlots() {
  try {
    const parsed = JSON.parse(localStorage.getItem(SLOT_STORAGE) || 'null');
    slots = Array.isArray(parsed) && parsed.length === 8 ? parsed : DEFAULT_MODE_SLOTS.slice();
  } catch { slots = DEFAULT_MODE_SLOTS.slice(); }
  try {
    const savedRX = JSON.parse(localStorage.getItem(RX_MODE_STORAGE) || 'null');
    rxEnabled = savedRX?.enabled !== false;
    autoSelected = savedRX?.auto !== false;
    activeSlot = Math.max(0, Math.min(7, Number(savedRX?.slot) || 0));
    if (!rxEnabled) { autoSelected = false; activeSlot = -1; }
  } catch { autoSelected = true; rxEnabled = true; activeSlot = 0; }
}
function loadTXSlots() {
  try {
    const parsed = JSON.parse(localStorage.getItem(TX_SLOT_STORAGE) || 'null');
    txSlots = Array.isArray(parsed) && parsed.length === 8 ? parsed : DEFAULT_MODE_SLOTS.slice();
  } catch { txSlots = DEFAULT_MODE_SLOTS.slice(); }
  const savedActive = localStorage.getItem(TX_SLOT_ACTIVE_STORAGE);
  const activeNumber = Number(savedActive);
  if (savedActive !== null && Number.isInteger(activeNumber) && activeNumber >= 0 && activeNumber < 8) {
    activeTXSlot = activeNumber;
  } else {
    // Migrate the former single-mode TX selection into the slot set.
    const legacy = localStorage.getItem(TX_MODE_STORAGE);
    const idx = legacy ? txSlots.indexOf(legacy) : -1;
    activeTXSlot = idx >= 0 ? idx : 0;
  }
}
function saveTXSlots() { localStorage.setItem(TX_SLOT_STORAGE, JSON.stringify(txSlots)); }
function saveTXActiveSlot() { localStorage.setItem(TX_SLOT_ACTIVE_STORAGE, String(activeTXSlot)); }
function saveRXModeSelection() { localStorage.setItem(RX_MODE_STORAGE, JSON.stringify({ enabled: rxEnabled, auto: autoSelected, slot: activeSlot })); }
function saveSlots() { localStorage.setItem(SLOT_STORAGE, JSON.stringify(slots)); }
function currentRXModeID() { return !rxEnabled || autoSelected ? '' : (slots[activeSlot] || ''); }
async function applyRXSelection(message) {
  try {
    await app().SetRXEnabled(rxEnabled, currentRXModeID());
    updateAudioState(await app().GetAudioState());
    setStatus(message);
  } catch (e) {
    setStatus(`更新 RX 状态失败：${String(e)}`, true);
  }
}
function renderModeSlots() {
  const host = $('modeSlots'); host.innerHTML = '';
  const rows = [0, 1].map(() => {
    const row = document.createElement('div');
    row.className = 'mode-slot-row';
    host.appendChild(row);
    return row;
  });
  const auto = document.createElement('button');
  auto.className = `mode-slot auto-slot${rxEnabled && autoSelected ? ' active' : ''}`;
  auto.textContent = 'Auto';
  auto.title = rxEnabled && autoSelected ? '再次点击停止 RX' : '启用 Auto 自动识别接收';
  auto.addEventListener('click', async () => {
    const selectingAuto = !(rxEnabled && autoSelected);
    rxEnabled = selectingAuto;
    autoSelected = selectingAuto;
    activeSlot = -1;
    saveRXModeSelection();
    renderModeSlots();
    updateModeInfo();
    await applyRXSelection(selectingAuto ? 'RX 已启用 Auto 自动识别' : 'RX 已停止');
  });
  rows[0].appendChild(auto);
  slots.forEach((id, i) => {
    const selected = rxEnabled && !autoSelected && activeSlot === i;
    const button = document.createElement('button');
    button.className = `mode-slot${selected ? ' active' : ''}`;
    button.textContent = niceMode(id).replace('Wraase ', '').replace('Scottie ', 'Scottie');
    button.title = selected ? `槽位 ${i + 1}：再次点击停止 RX，右键更换模式` : `槽位 ${i + 1}：${niceMode(id)}，右键更换`;
    button.addEventListener('click', async () => {
      if (selected) {
        rxEnabled = false;
        autoSelected = false;
        activeSlot = -1;
      } else {
        rxEnabled = true;
        autoSelected = false;
        activeSlot = i;
      }
      saveRXModeSelection();
      renderModeSlots();
      updateModeInfo();
      await applyRXSelection(rxEnabled ? `RX 已切换为 ${niceMode(id)} 指定接收` : 'RX 已停止');
    });
    button.addEventListener('contextmenu', (e) => { e.preventDefault(); openModeMenu(e.clientX, e.clientY, i); });
    (i < 4 ? rows[0] : rows[1]).appendChild(button);
  });
  updateModeInfo();
}
function openModeMenu(x, y, slotIndex) {
  contextTarget = { type: 'mode', slotIndex };
  const menu = $('contextMenu'); menu.innerHTML = '';
  const title = document.createElement('div'); title.className = 'context-title'; title.textContent = `设置 RX 模式槽位 ${slotIndex + 1}`; menu.appendChild(title);
  modes.forEach((m) => {
    const item = document.createElement('button'); item.className = 'context-item';
    item.textContent = `${m.name}${m.ready ? '' : ' · 尚未完成编解码'}`;
    item.disabled = false;
    item.addEventListener('click', async () => {
      slots[slotIndex] = m.id; saveSlots(); activeSlot = slotIndex; autoSelected = false; rxEnabled = true; saveRXModeSelection(); hideContextMenu(); renderModeSlots(); updateModeInfo();
      await applyRXSelection(`RX 槽位 ${slotIndex + 1} 已设为 ${m.name}`);
    });
    menu.appendChild(item);
  });
  showContextMenu(x, y);
}
function showContextMenu(x, y) {
  const menu = $('contextMenu'); menu.hidden = false;
  const w = menu.offsetWidth || 220, h = menu.offsetHeight || 250;
  menu.style.left = `${Math.max(5, Math.min(x, innerWidth - w - 6))}px`;
  menu.style.top = `${Math.max(5, Math.min(y, innerHeight - h - 6))}px`;
}
function hideContextMenu() { $('contextMenu').hidden = true; contextTarget = null; }
function updateModeInfo() {
  const mode = activeRXMode();
  if (!rxEnabled) {
    $('modeInfo').textContent = 'RX 已停止';
    $('rxDetectedMode').textContent = '---';
    $('rxAutoState').textContent = 'RX 已停止';
  } else if (autoSelected) {
    $('modeInfo').textContent = 'Auto · 自动识别所有支持模式';
    $('rxDetectedMode').textContent = 'Auto';
    $('rxAutoState').textContent = 'Auto · 自动接收';
  } else {
    $('modeInfo').textContent = `${mode?.name || '未选择'} · 指定模式接收`;
    $('rxDetectedMode').textContent = mode?.name || '---';
    $('rxAutoState').textContent = `${mode?.name || ''} · 指定模式接收`;
  }
  renderCanvas();
}
function renderTXModeSlots() {
  const host = $('txModeSlots');
  host.innerHTML = '';
  const rows = [0, 1].map(() => {
    const row = document.createElement('div');
    row.className = 'mode-slot-row';
    host.appendChild(row);
    return row;
  });
  txSlots.forEach((id, i) => {
    const selected = activeTXSlot === i;
    const button = document.createElement('button');
    button.className = `mode-slot${selected ? ' active' : ''}`;
    button.textContent = niceMode(id).replace('Wraase ', '').replace('Scottie ', 'Scottie');
    button.title = selected ? `槽位 ${i + 1}：${niceMode(id)}，右键更换模式` : `槽位 ${i + 1}：${niceMode(id)}，右键更换`;
    button.addEventListener('click', () => {
      if (selected) return;
      activeTXSlot = i;
      saveTXActiveSlot();
      renderTXModeSlots();
      renderCanvas();
      setStatus(`TX 模式已切换为 ${niceMode(id)}`);
    });
    button.addEventListener('contextmenu', (e) => { e.preventDefault(); openTXModeMenu(e.clientX, e.clientY, i); });
    (i < 4 ? rows[0] : rows[1]).appendChild(button);
  });
  $('txModeInfo').textContent = activeTXMode()?.name || niceMode(txSlots[activeTXSlot]);
  $('encode').disabled = !activeTXMode()?.ready;
}
function openTXModeMenu(x, y, slotIndex) {
  contextTarget = { type: 'txmode', slotIndex };
  const menu = $('contextMenu'); menu.innerHTML = '';
  const title = document.createElement('div'); title.className = 'context-title'; title.textContent = `设置 TX 模式槽位 ${slotIndex + 1}`; menu.appendChild(title);
  modes.forEach((m) => {
    const item = document.createElement('button'); item.className = 'context-item';
    item.textContent = `${m.name}${m.ready ? '' : ' · 尚未完成编解码'}`;
    item.addEventListener('click', () => {
      txSlots[slotIndex] = m.id;
      saveTXSlots();
      activeTXSlot = slotIndex;
      saveTXActiveSlot();
      hideContextMenu();
      renderTXModeSlots();
      renderCanvas();
      setStatus(`TX 槽位 ${slotIndex + 1} 已设为 ${m.name}`);
    });
    menu.appendChild(item);
  });
  showContextMenu(x, y);
}
function withOutlineDefaults(component) {
  const width = Number(component?.outlineWidth);
  const color = /^#[0-9a-f]{6}$/i.test(String(component?.outlineColor || '')) ? component.outlineColor : '#17212b';
  return {
    ...component,
    outlineEnabled: component?.outlineEnabled !== false,
    outlineWidth: Number.isFinite(width) ? Math.max(0.5, Math.min(8, width)) : 1.5,
    outlineColor: color,
  };
}
function componentOutline(component) {
  const normalized = withOutlineDefaults(component || {});
  return { enabled: normalized.outlineEnabled, width: normalized.outlineWidth, color: normalized.outlineColor };
}
function drawCover(ctx, img, width, height) {
  const scale = Math.max(width / img.width, height / img.height);
  const sw = width / scale, sh = height / scale;
  ctx.drawImage(img, (img.width - sw) / 2, (img.height - sh) / 2, sw, sh, 0, 0, width, height);
}
function componentBounds(ctx, c, width, height) {
  const x = (Number(c.x) || 0) / 100 * width;
  const y = (Number(c.y) || 0) / 100 * height;
  if (isImageComponent(c)) {
    return { x, y, width: (Number(c.width) || 40) / 100 * width, height: (Number(c.height) || 40) / 100 * height, image: true };
  }
  const text = resolveTokens(c.text || '');
  const lines = text.split('\n');
  ctx.font = `${c.bold ? '700' : '400'} ${c.fontSize || 18}px "Segoe UI", "Microsoft YaHei", sans-serif`;
  const lineHeight = (c.fontSize || 18) * 1.2;
  const maxWidth = Math.max(1, ...lines.map((line) => ctx.measureText(line).width));
  return { text, x, y, width: maxWidth, height: lineHeight * lines.length, lines, lineHeight, image: false };
}
function isImageComponent(c) { return c?.type === 'image' || c?.type === 'last_rx'; }
function getTemplateImage(c) {
  const dataURL = c?.type === 'last_rx' ? latestRXImageData : c?.imageData;
  if (!dataURL) return null;
  let image = templateImageCache.get(dataURL);
  if (image) return image;
  image = new Image();
  image.onload = () => renderCanvas();
  image.src = dataURL;
  templateImageCache.set(dataURL, image);
  return image;
}
function drawLastRXPlaceholder(ctx, bounds) {
  ctx.save();
  ctx.fillStyle = 'rgba(229,238,245,.88)';
  ctx.fillRect(bounds.x, bounds.y, bounds.width, bounds.height);
  ctx.strokeStyle = 'rgba(91,132,156,.8)';
  ctx.lineWidth = 1.5;
  ctx.setLineDash([5, 4]);
  ctx.strokeRect(bounds.x + 1, bounds.y + 1, Math.max(0, bounds.width - 2), Math.max(0, bounds.height - 2));
  ctx.setLineDash([]);
  ctx.fillStyle = '#648397';
  ctx.textAlign = 'center';
  ctx.textBaseline = 'middle';
  ctx.font = `600 ${Math.max(8, Math.min(15, bounds.width * .09))}px "Segoe UI", "Microsoft YaHei", sans-serif`;
  ctx.fillText('上一次 RX 图像', bounds.x + bounds.width / 2, bounds.y + bounds.height / 2 - 5, Math.max(1, bounds.width - 8));
  ctx.font = `400 ${Math.max(6, Math.min(10, bounds.width * .055))}px "Segoe UI", "Microsoft YaHei", sans-serif`;
  ctx.fillText('等待接收图像', bounds.x + bounds.width / 2, bounds.y + bounds.height / 2 + 11, Math.max(1, bounds.width - 8));
  ctx.restore();
}
function renderToCanvas(canvas, selectionOutline = false) {
  const mode = activeTXMode();
  const width = Math.max(160, mode?.width || 320), height = Math.max(120, mode?.height || 240);
  canvas.width = width; canvas.height = height;
  const ctx = canvas.getContext('2d');
  if (sourceImage && sourceImage.complete && sourceImage.naturalWidth) drawCover(ctx, sourceImage, width, height);
  else {
    const grad = ctx.createLinearGradient(0, 0, width, height);
    grad.addColorStop(0, '#24394d'); grad.addColorStop(1, '#111b29');
    ctx.fillStyle = grad; ctx.fillRect(0, 0, width, height);
    ctx.fillStyle = 'rgba(255,255,255,.07)'; ctx.beginPath(); ctx.arc(width * .8, height * .25, height * .28, 0, Math.PI * 2); ctx.fill();
  }
  components.forEach((c, idx) => {
    const b = componentBounds(ctx, c, width, height);
    const outline = componentOutline(c);
    if (isImageComponent(c)) {
      const img = getTemplateImage(c);
      if (img?.complete && img.naturalWidth) {
        const scale = Math.min(b.width / img.naturalWidth, b.height / img.naturalHeight);
        const drawWidth = img.naturalWidth * scale;
        const drawHeight = img.naturalHeight * scale;
        ctx.drawImage(img, b.x + (b.width - drawWidth) / 2, b.y + (b.height - drawHeight) / 2, drawWidth, drawHeight);
      } else if (c.type === 'last_rx') {
        drawLastRXPlaceholder(ctx, b);
      }
      if (outline.enabled) {
        ctx.save();
        ctx.strokeStyle = outline.color;
        ctx.lineWidth = outline.width;
        ctx.strokeRect(b.x + outline.width / 2, b.y + outline.width / 2,
          Math.max(0, b.width - outline.width), Math.max(0, b.height - outline.width));
        ctx.restore();
      }
    } else {
      ctx.font = `${c.bold ? '700' : '400'} ${c.fontSize || 18}px "Segoe UI", "Microsoft YaHei", sans-serif`;
      ctx.textBaseline = 'top'; ctx.textAlign = 'left'; ctx.fillStyle = c.color || '#ffffff';
      if (outline.enabled) {
        ctx.save();
        ctx.strokeStyle = outline.color;
        ctx.lineWidth = outline.width;
        ctx.lineJoin = 'round';
        b.lines.forEach((line, lineNo) => ctx.strokeText(line, b.x, b.y + lineNo * b.lineHeight));
        ctx.restore();
      }
      b.lines.forEach((line, lineNo) => ctx.fillText(line, b.x, b.y + lineNo * b.lineHeight));
    }
    if (selectionOutline && idx === selectedComponent) {
      ctx.save(); ctx.strokeStyle = '#5bcefa'; ctx.lineWidth = Math.max(1, width / 320);
      ctx.setLineDash([4, 3]); ctx.strokeRect(b.x - 2, b.y - 2, b.width + 4, b.height + 4); ctx.restore();
    }
  });
  if (!sourceImage && components.length === 0) {
    ctx.save(); ctx.font = `600 ${Math.max(7, width * .025)}px "Segoe UI", sans-serif`;
    ctx.fillStyle = 'rgba(255,255,255,.34)'; ctx.textAlign = 'right'; ctx.fillText('SATSSTV-GO', width * .97, height * .04); ctx.restore();
  }
  if (canvas === $('txCanvas')) $('canvasDimensions').textContent = `${width} × ${height}`;
}
function renderCanvas() { renderToCanvas($('txCanvas'), true); }
function exportCanvasDataURL() { const canvas = document.createElement('canvas'); renderToCanvas(canvas, false); return canvas.toDataURL('image/png'); }

function renderComponentList() {
  const host = $('componentList'); host.innerHTML = '';
  components.forEach((c, i) => {
    const row = document.createElement('button'); row.className = `component-item${selectedComponent === i ? ' selected' : ''}`;
    const sample = document.createElement('span'); sample.className = 'component-sample';
    sample.textContent = isImageComponent(c) ? (c.type === 'last_rx' ? 'RX' : 'IMG') : (c.text || '').replace(/\{[A-Z]+\}/g, '…').slice(0, 1) || 'T';
    sample.style.color = c.color || '#5b8ba0';
    const name = document.createElement('span'); name.className = 'component-name';
    name.textContent = isImageComponent(c) ? (c.type === 'last_rx' ? '上一次 RX 图像' : (c.imageName || '模板图像')) : (c.text || '').replace(/\s+/g, ' ').slice(0, 28) || `文字 ${i + 1}`;
    const meta = document.createElement('small'); meta.textContent = isImageComponent(c) ? `${Math.round(c.width)}×${Math.round(c.height)}%` : `${c.fontSize}px`;
    row.append(sample, name, meta); row.addEventListener('click', () => selectComponent(i)); host.appendChild(row);
  });
  $('componentProperties').classList.toggle('disabled-properties', components.length === 0);
  updateComponentProperties();
}
function selectComponent(index) {
  if (!components.length) return;
  selectedComponent = Math.max(0, Math.min(index, components.length - 1));
  renderComponentList(); renderCanvas();
}
function updateComponentProperties() {
  const c = components[selectedComponent];
  const isImage = isImageComponent(c);
  ['componentText', 'fontSize', 'fontColor', 'fontBold'].forEach(id => { $(id).disabled = !c || isImage; });
  ['posX', 'posY', 'deleteText'].forEach(id => { $(id).disabled = !c; });
  ['imageWidth', 'imageHeight'].forEach(id => { $(id).disabled = !isImage; });
  $('imageProperties').hidden = !isImage;
  $('componentText').parentElement.hidden = !c || isImage;
  $('fontBold').parentElement.hidden = !c || isImage;
  $('outlineProperties').hidden = !c;
  ['outlineEnabled', 'outlineWidth', 'outlineColor'].forEach(id => { $(id).disabled = !c; });
  if (!c) return;
  const outline = componentOutline(c);
  $('outlineEnabled').checked = outline.enabled;
  $('outlineWidth').value = String(outline.width);
  $('outlineWidthValue').textContent = `${outline.width.toFixed(1)} px`;
  $('outlineColor').value = outline.color;
  $('posX').value = c.x; $('xValue').textContent = `${Math.round(c.x)}%`;
  $('posY').value = c.y; $('yValue').textContent = `${Math.round(c.y)}%`;
  if (isImage) {
    $('imageWidth').value = c.width; $('imageWidthValue').textContent = `${Math.round(c.width)}%`;
    $('imageHeight').value = c.height; $('imageHeightValue').textContent = `${Math.round(c.height)}%`;
  } else {
    $('componentText').value = c.text; $('fontSize').value = c.fontSize; $('fontSizeValue').textContent = `${c.fontSize} px`;
    $('fontColor').value = c.color || '#ffffff'; $('fontBold').checked = !!c.bold;
  }
}
function updateSelectedComponent() {
  const c = components[selectedComponent]; if (!c) return;
  c.x = Number($('posX').value); c.y = Number($('posY').value);
  if (isImageComponent(c)) {
    c.width = Number($('imageWidth').value); c.height = Number($('imageHeight').value);
    c.x = Math.min(c.x, Math.max(0, 100 - c.width));
    c.y = Math.min(c.y, Math.max(0, 100 - c.height));
    $('posX').value = c.x; $('posY').value = c.y;
    $('imageWidthValue').textContent = `${c.width}%`; $('imageHeightValue').textContent = `${c.height}%`;
  } else {
    c.text = $('componentText').value; c.fontSize = Number($('fontSize').value); c.color = $('fontColor').value; c.bold = $('fontBold').checked;
    $('fontSizeValue').textContent = `${c.fontSize} px`;
  }
  c.outlineEnabled = $('outlineEnabled').checked;
  c.outlineWidth = Number($('outlineWidth').value);
  c.outlineColor = $('outlineColor').value;
  $('outlineWidthValue').textContent = `${c.outlineWidth.toFixed(1)} px`;
  $('xValue').textContent = `${c.x}%`; $('yValue').textContent = `${c.y}%`;
  renderComponentList(); renderCanvas();
}
function addTextComponent() {
  components.push(withOutlineDefaults({ type: 'text', text: '自定义文字', x: 8, y: 15 + (components.length * 8 % 65), fontSize: 18, color: '#ffffff', bold: true }));
  selectComponent(components.length - 1);
}
async function addImageComponent(dataURL, imageName = '模板图像') {
  if (!dataURL) return;
  const image = new Image();
  image.onload = () => {
    components.push(withOutlineDefaults({ type: 'image', imageData: dataURL, imageName, x: 8, y: 18, width: 42, height: 42 }));
    templateImageCache.set(dataURL, image);
    selectComponent(components.length - 1);
  };
  image.onerror = () => setStatus('无法载入模板图像', true);
  image.src = dataURL;
}
async function chooseTemplateImage() {
  try {
    const path = await app().SelectImage(); if (!path) return;
    const url = await app().LoadImagePreview(path);
    await addImageComponent(url, path.split(/[\\/]/).pop());
  } catch (e) { setStatus(`添加模板图像失败：${String(e)}`, true); }
}
function addLatestRXImage() {
  components.push(withOutlineDefaults({ type: 'last_rx', imageName: '上一次 RX 图像', x: 8, y: 18, width: 42, height: 42 }));
  selectComponent(components.length - 1);
  setStatus(latestRXImageData ? '已添加上一次 RX 图像组件' : '已添加上一次 RX 图像占位组件，收到首张图像后会自动填入');
}
async function refreshLatestRXImage() {
  try {
    latestRXImageData = await app().GetLatestRXImage() || '';
  } catch {
    // Missing or not-yet-created RX archive is an ordinary empty state.
    latestRXImageData = '';
  }
  templateImageCache.clear();
  renderCanvas();
}
function deleteSelectedComponent() {
  if (!components.length) return;
  components.splice(selectedComponent, 1); selectedComponent = Math.max(0, Math.min(selectedComponent, components.length - 1));
  renderComponentList(); renderCanvas();
}

function templateData(template) { return (template.objects || []).map((o) => withOutlineDefaults(o)); }
function loadTemplateAt(index) {
  selectedTemplateSlot = Math.max(0, Math.min(3, index));
  const template = templateSlots[selectedTemplateSlot];
  components = templateData(template); selectedComponent = 0;
  $('templateName').value = template.name || '';
  renderTemplates(); renderComponentList(); renderCanvas();
  setStatus(`已载入模板槽位 ${selectedTemplateSlot + 1}：${template.name || '空白模板'}`);
}
function renderTemplates() {
  const host = $('templatePresets'); host.innerHTML = '';
  templateSlots.forEach((t, index) => {
    const b = document.createElement('button'); b.className = `template-card${index === selectedTemplateSlot ? ' selected-template' : ''}`;
    const preview = document.createElement('span'); preview.className = 'template-thumb';
    const textObjects = t.objects.filter(o => !isImageComponent(o));
    const mainText = document.createElement('b'); mainText.textContent = (textObjects[0]?.text || (t.objects.some(o => isImageComponent(o)) ? 'IMAGE' : (index === 3 ? '空白模板' : 'TEXT'))).replace(/\{[A-Z]+\}/g, '…').slice(0, 16);
    const secondary = document.createElement('i'); secondary.textContent = textObjects[1]?.text?.replace(/\{[A-Z]+\}/g, '…').slice(0, 18) || (t.objects.some(o => isImageComponent(o)) ? '包含图像组件' : '');
    preview.append(mainText, secondary);
    const label = document.createElement('span'); label.className = 'template-name'; label.textContent = `${index + 1}. ${t.name || '空白模板'}`;
    b.append(preview, label); b.addEventListener('click', () => loadTemplateAt(index)); host.appendChild(b);
  });
}
async function saveCurrentTemplate() {
  const name = $('templateName').value.trim() || templateSlots[selectedTemplateSlot]?.name || `模板 ${selectedTemplateSlot + 1}`;
  const next = templateSlots.map((slot, index) => index === selectedTemplateSlot
    ? { ...slot, name, objects: templateData({ objects: components }) }
    : { ...slot, objects: templateData(slot) });
  const button = $('saveTemplate'); button.disabled = true;
  try {
    templateSlots = await app().SaveTemplateSlots(next);
    $('templateName').value = templateSlots[selectedTemplateSlot].name;
    renderTemplates(); setStatus(`已保存到模板槽位 ${selectedTemplateSlot + 1}：${name}`);
  } catch (e) { setStatus(`保存模板失败：${String(e)}`, true); }
  finally { button.disabled = false; }
}
async function loadTemplateSlots() {
  try { templateSlots = await app().GetTemplateSlots(); }
  catch (e) { templateSlots = defaultTemplateSlots.map(t => ({ ...t, objects: t.objects.map(o => ({ ...o })) })); setStatus(`读取模板失败，使用内置模板：${String(e)}`, true); }
  if (!Array.isArray(templateSlots) || templateSlots.length !== 4) templateSlots = defaultTemplateSlots.map(t => ({ ...t, objects: t.objects.map(o => ({ ...o })) }));
  loadTemplateAt(0);
  await refreshLatestRXImage();
}

function findComponentAt(x, y) {
  const c = $('txCanvas'), ctx = c.getContext('2d');
  for (let i = components.length - 1; i >= 0; i--) {
    const b = componentBounds(ctx, components[i], c.width, c.height);
    if (x >= b.x - 10 && x <= b.x + b.width + 10 && y >= b.y - 10 && y <= b.y + b.height + 10) return i;
  }
  return -1;
}
function canvasPoint(event) {
  const canvas = $('txCanvas'), rect = canvas.getBoundingClientRect();
  return { x: (event.clientX - rect.left) * canvas.width / rect.width, y: (event.clientY - rect.top) * canvas.height / rect.height };
}
function setupCanvasDrag() {
  const canvas = $('txCanvas');
  canvas.addEventListener('pointerdown', (e) => {
    const p = canvasPoint(e), index = findComponentAt(p.x, p.y); if (index < 0) return;
    selectComponent(index); pointerDrag = { index, offsetX: p.x - components[index].x / 100 * canvas.width, offsetY: p.y - components[index].y / 100 * canvas.height };
    canvas.setPointerCapture(e.pointerId); e.preventDefault();
  });
  canvas.addEventListener('pointermove', (e) => {
    if (!pointerDrag) return; const p = canvasPoint(e), c = components[pointerDrag.index];
    const maxX = isImageComponent(c) ? Math.max(0, 100 - Number(c.width || 40)) : 100;
    const maxY = isImageComponent(c) ? Math.max(0, 100 - Number(c.height || 40)) : 100;
    c.x = Math.max(0, Math.min(maxX, (p.x - pointerDrag.offsetX) / canvas.width * 100));
    c.y = Math.max(0, Math.min(maxY, (p.y - pointerDrag.offsetY) / canvas.height * 100));
    updateComponentProperties(); renderComponentList(); renderCanvas();
  });
  const end = () => { pointerDrag = null; };
  canvas.addEventListener('pointerup', end); canvas.addEventListener('pointercancel', end);
}

let waterfallImageData = null;
let waterfallOff = null;

function spectrumColorRGB(value) {
  const strength = Math.max(0, Math.min(1, (Number(value) - 0.16) / 0.84));
  const gray = Math.round(255 * (1 - Math.pow(strength, 0.82)));
  return [gray, gray, gray];
}
function spectrumColor(value) {
  const [gray] = spectrumColorRGB(value);
  return `rgb(${gray},${gray},${gray})`;
}
function drawWaterfall(ctx, w, waterfallY, waterfallH) {
  const rows = waterfallHistory.slice(-MAX_WATERFALL_ROWS);
  if (!rows.length) return;
  const bins = rows[0].length;
  const rowCount = MAX_WATERFALL_ROWS;
  if (!waterfallOff || waterfallOff.width !== bins || waterfallOff.height !== rowCount) {
    waterfallOff = document.createElement('canvas');
    waterfallOff.width = bins;
    waterfallOff.height = rowCount;
    waterfallImageData = waterfallOff.getContext('2d').createImageData(bins, rowCount);
  }
  const data = waterfallImageData.data;
  const emptyRows = rowCount - rows.length;
  // Clear the (still empty) leading rows so a partial waterfall never leaves
  // stale pixels after an RX restart.
  for (let r = 0; r < emptyRows; r++) {
    const base = r * bins * 4;
    for (let b = 0; b < bins; b++) {
      const p = base + b * 4;
      data[p] = 247; data[p + 1] = 247; data[p + 2] = 244; data[p + 3] = 255;
    }
  }
  for (let r = 0; r < rows.length; r++) {
    const row = rows[r];
    const base = (emptyRows + r) * bins * 4;
    for (let b = 0; b < bins; b++) {
      const [gray] = spectrumColorRGB(row[b]);
      const p = base + b * 4;
      data[p] = gray; data[p + 1] = gray; data[p + 2] = gray; data[p + 3] = 255;
    }
  }
  waterfallOff.getContext('2d').putImageData(waterfallImageData, 0, 0);
  const prevSmoothing = ctx.imageSmoothingEnabled;
  ctx.imageSmoothingEnabled = false;
  ctx.drawImage(waterfallOff, 0, 0, bins, rowCount, 0, waterfallY, w, waterfallH);
  ctx.imageSmoothingEnabled = prevSmoothing;
}
function drawIdleSpectrum() {
  const canvas = $('spectrum'), ctx = canvas.getContext('2d'), w = canvas.width, h = canvas.height;
  const plotH = Math.round(h * 0.58);
  const waterfallY = plotH + 2;
  const waterfallH = h - waterfallY;

  // MMSSTV-inspired orange spectrum pane and high-contrast monochrome waterfall.
  ctx.fillStyle = '#ff813f';
  ctx.fillRect(0, 0, w, plotH);
  ctx.strokeStyle = 'rgba(117,59,21,.18)';
  ctx.lineWidth = 1;
  for (let x = 0; x <= w; x += w / 12) {
    ctx.beginPath(); ctx.moveTo(x, 18); ctx.lineTo(x, plotH); ctx.stroke();
  }
  for (let y = 28; y < plotH; y += (plotH - 28) / 4) {
    ctx.beginPath(); ctx.moveTo(0, y); ctx.lineTo(w, y); ctx.stroke();
  }
  const markers = [
    { frequency: 1200, label: '1200' },
    { frequency: 1500, label: '1500' },
    { frequency: 1900, label: '1900' },
    { frequency: 2300, label: '2300' },
  ];
  ctx.font = '12px Segoe UI, sans-serif';
  ctx.textBaseline = 'top';
  ctx.textAlign = 'center';
  markers.forEach(marker => {
    const x = (marker.frequency - 900) / 1900 * w;
    ctx.strokeStyle = 'rgba(255,238,65,.9)';
    ctx.lineWidth = 1.2;
    ctx.beginPath(); ctx.moveTo(x, 18); ctx.lineTo(x, plotH); ctx.stroke();
    ctx.fillStyle = '#27313b';
    ctx.fillText(marker.label, x, 1);
  });
  ctx.strokeStyle = '#36414a';
  ctx.beginPath(); ctx.moveTo(0, plotH - 1); ctx.lineTo(w, plotH - 1); ctx.stroke();

  ctx.fillStyle = '#f7f7f4';
  ctx.fillRect(0, waterfallY, w, waterfallH);
  drawWaterfall(ctx, w, waterfallY, waterfallH);
  ctx.strokeStyle = '#65717b';
  ctx.beginPath(); ctx.moveTo(0, waterfallY); ctx.lineTo(w, waterfallY); ctx.stroke();
}
function renderLiveSpectrum(values) {
  const canvas = $('spectrum'), ctx = canvas.getContext('2d'), w = canvas.width, h = canvas.height;
  if (!Array.isArray(values) || !values.length) { drawIdleSpectrum(); return; }
  const normalized = values.map(v => Math.max(0, Math.min(1, Number(v) || 0)));
  waterfallHistory.push(normalized);
  if (waterfallHistory.length > MAX_WATERFALL_ROWS) waterfallHistory.shift();
  if (peakSpectrum.length !== normalized.length) peakSpectrum = normalized.slice();
  else peakSpectrum = normalized.map((value, index) => Math.max(value, peakSpectrum[index] * 0.965));

  drawIdleSpectrum();
  const plotH = Math.round(h * 0.58);
  const pointsFor = spectrum => spectrum.map((value, index) => [
    index * w / Math.max(1, spectrum.length - 1),
    plotH - 9 - value * (plotH - 34),
  ]);
  const peaks = pointsFor(peakSpectrum);
  ctx.beginPath(); ctx.moveTo(peaks[0][0], peaks[0][1]);
  for (let i = 1; i < peaks.length; i++) ctx.lineTo(peaks[i][0], peaks[i][1]);
  ctx.lineWidth = 1.3; ctx.strokeStyle = '#ffe64a'; ctx.stroke();

  const points = pointsFor(normalized);
  ctx.beginPath(); ctx.moveTo(points[0][0], points[0][1]);
  for (let i = 1; i < points.length; i++) ctx.lineTo(points[i][0], points[i][1]);
  ctx.lineWidth = 2.2; ctx.strokeStyle = '#063cff';
  ctx.shadowColor = '#063cff'; ctx.shadowBlur = 1.5; ctx.stroke(); ctx.shadowBlur = 0;
}
function updateAudioState(state) {
  const wasRXActive = !!audioState.rxActive;
  audioState = { ...audioState, ...(state || {}) };
  if (audioState.rxActive && !wasRXActive) {
    captureTelemetrySeen = false;
    waterfallHistory = [];
    peakSpectrum = [];
    drawIdleSpectrum();
  }
  if (!audioState.rxActive) captureTelemetrySeen = false;
  const connected = !!audioState.connected;
  const label = audioState.rxActive ? `RX 自动运行 · ${(audioState.sampleRate || 48000) / 1000} kHz` : (connected ? '音频已就绪' : '音频待初始化');
  document.querySelector('.device-state').lastChild.textContent = ` ${label}`;
  document.querySelector('.device-state i').style.background = audioState.rxActive ? '#5b9f77' : '#c39a55';
  $('rate').disabled = false;
  ['audioInput','audioOutput','pttMode','rigctlAddress','serialPort','pttActiveHigh','pttLead','pttTail'].forEach(id => { if ($(id)) $(id).disabled = false; });
  const txButton = $('txToggle');
  txButton.disabled = !connected || toneActive || (!audioState.txActive && !activeTXMode()?.ready);
  txButton.textContent = audioState.txActive && !toneActive ? '停止发射' : '开始发射';
  txButton.classList.toggle('tx-running', !!audioState.txActive && !toneActive);
  const toneButton = $('toneToggle');
  toneButton.disabled = !connected || (audioState.txActive && !toneActive);
  toneButton.textContent = toneActive ? '停止单音' : '1500 Hz 单音';
  $('audioSettingsStatus').textContent = connected ? `共享模式音频已就绪，${audioState.rxActive ? 'RX 持续接收中' : 'RX 正在恢复'}${audioState.txActive ? '，TX 运行中' : ''}` : '音频设备待初始化，工作台会自动启动 RX';
  $('spectrumState').textContent = !audioState.rxActive ? 'INPUT IDLE' : (captureTelemetrySeen ? 'INPUT ACTIVE · SHARED' : 'WAITING FOR INPUT');
  $('spectrumOverlay').textContent = !audioState.rxActive ? 'RX 尚未启动' : (captureTelemetrySeen ? '' : 'RX 已启动，正在等待音频设备输入采样…');
  $('spectrumOverlay').style.display = audioState.rxActive && captureTelemetrySeen ? 'none' : 'grid';
  $('rxHint').textContent = !audioState.rxActive ? (rxEnabled ? 'RX 音频流正在初始化…' : 'RX 已停止') : (captureTelemetrySeen ? '实时音频采集中' : 'RX 已启动，等待输入采样');
  $('rxStatus').textContent = audioState.rxActive ? (autoSelected ? 'Auto 自动接收中' : '指定模式接收中') : (rxEnabled ? '初始化中' : 'RX 已停止');
  $('txStatus').textContent = audioState.txActive ? (toneActive ? '1500 Hz 单音发射' : '正在发射') : '草稿';
}
function bindRuntimeEvents() {
  if (!window.runtime?.EventsOn) return;
  window.runtime.EventsOn('audio:state', updateAudioState);
  window.runtime.EventsOn('audio:telemetry', (data) => {
    captureTelemetrySeen = true;
    const peakHz = Number(data?.peakHz || 0);
    const crest = Number(data?.crest || 0);
    $('spectrumState').textContent = `峰值 ${peakHz.toFixed(0)} Hz · 峰度 ${crest.toFixed(1)}`;
    const dbfs = Number(data?.dbfs ?? -120);
    const fill = Math.max(0, Math.min(100, (dbfs + 60) / 60 * 100));
    $('inputLevel').style.width = `${fill}%`;
    $('inputLevelText').textContent = `${dbfs.toFixed(1)} dBFS`;
    const samplesReceived = Number(data?.samplesReceived) || 0;
    $('rxHint').textContent = data?.droppedBlocks ? `采集队列丢弃 ${data.droppedBlocks} 个音频块` : (dbfs < -90 ? `已收到 ${samplesReceived.toLocaleString()} 个采样，当前输入接近静音` : `实时音频采集中 · ${samplesReceived.toLocaleString()} samples`);
    renderLiveSpectrum(data?.spectrum);
    $('spectrumOverlay').style.display = 'none';
  });
  window.runtime.EventsOn('rx:progress', (data) => {
    const percent = Math.max(0, Math.min(100, Number(data?.percent) || 0));
    $('rxProgressFill').style.width = `${percent}%`;
    $('rxProgressText').textContent = `${Math.round(percent)}%`;
    if (data?.complete) {
      $('rxProgressLabel').textContent = `${data.mode || 'SSTV'} 解码完成`;
    } else if (data?.active) {
      const rows = Number(data.rows) || 0;
      const total = Number(data.totalRows) || 0;
      $('rxProgressLabel').textContent = `正在接收 ${data.mode || 'SSTV'} · ${rows}/${total} 行`;
    } else {
      $('rxProgressLabel').textContent = '等待 SSTV 信号';
      $('rxProgressFill').style.width = '0%';
      $('rxProgressText').textContent = '0%';
    }
  });
  window.runtime.EventsOn('rx:callsign', (data) => {
    const callsign = String(data?.callsign || '').trim();
    if (!callsign) return;
    $('rxCallsign').textContent = callsign;
    $('rxCallsign').title = `来源：${data?.source || 'FSKID'}`;
    setStatus(`已通过 ${data?.source || 'FSKID'} 识别到呼号 ${callsign}`);
  });
  window.runtime.EventsOn('rx:preview', (data) => {
    const frame = $('rxImageFrame');
    let img = frame.querySelector('#rxLiveImage');
    if (!img) {
      frame.innerHTML = '';
      img = document.createElement('img');
      img.id = 'rxLiveImage';
      img.alt = `${data.mode || 'SSTV'} 实时解码图像`;
      frame.appendChild(img);
    }
    img.src = data.image;
    img.alt = `${data.mode || 'SSTV'} 实时解码图像`;
    $('rxDetectedMode').textContent = data.mode || '---';
    $('rxResolution').textContent = `${data.width} × ${data.height}`;
    $('rxStatus').textContent = '实时图像解码中';
    $('rxProgressLabel').textContent = `实时显示 ${data.mode || 'SSTV'} · ${data.rows}/${data.totalRows} 行`;
  });
  window.runtime.EventsOn('rx:frame', async (data) => {
    latestRXImageData = data.image || latestRXImageData;
    templateImageCache.clear();
    renderCanvas();
    const frame = $('rxImageFrame'); frame.innerHTML = '';
    const img = document.createElement('img'); img.src = data.image; img.alt = `${data.mode} 接收图像`; frame.appendChild(img);
    $('rxDetectedMode').textContent = data.mode; $('rxResolution').textContent = `${data.width} × ${data.height}`; $('rxTimestamp').textContent = data.timestamp;
    $('rxStatus').textContent = '图像接收完成';
    $('rxProgressFill').style.width = '100%'; $('rxProgressText').textContent = '100%'; $('rxProgressLabel').textContent = '图像接收完成';
    setStatus(`${data.mode} 图像接收完成${data.albumSaved ? '，已加入相册' : ''}`);
    await refreshAlbum();
  });
  window.runtime.EventsOn('rx:error', message => setStatus(`RX 解码提示：${message}`, true));
  window.runtime.EventsOn('tx:state', data => {
    $('txStatus').textContent = data.state === 'playing' ? '发射中' : data.state === 'encoding' ? '编码中' : data.state === 'completed' ? '完成' : data.state === 'stopped' ? '已停止' : data.state === 'error' ? '错误' : '草稿';
    $('txProgressLabel').textContent = data.message || data.state;
    if (data.state === 'completed') { $('txProgressFill').style.width = '100%'; $('txProgressText').textContent = '100%'; }
    if (data.state === 'stopped' || data.state === 'error') $('txProgressLabel').textContent = data.message || data.state;
    if (['completed', 'stopped', 'error'].includes(data.state)) {
      toneActive = false;
      updateAudioState(audioState);
    }
    setStatus(data.message || data.state, data.state === 'error');
  });
  window.runtime.EventsOn('tx:progress', (data) => {
    const percent = Math.max(0, Math.min(100, Number(data?.percent) || 0));
    $('txProgressFill').style.width = `${percent}%`;
    $('txProgressText').textContent = `${Math.round(percent)}%`;
    $('txProgressLabel').textContent = data?.message || (data?.stage === 'playing' ? '正在发射 SSTV' : '正在编码 SSTV');
  });
  window.runtime.EventsOn('tx:complete', () => refreshAlbum());
}

async function startTX() {
  const mode = activeTXMode();
  if (!mode || !mode.ready) return setStatus('当前 TX 模式的编码器不可用', true);
  if (!audioState.connected) return setStatus('TX 音频输出尚未初始化，请在设备设置中选择输出设备并应用', true);
  try {
    // Use the current QSO editor identity for the FSKID payload, so the
    // transmitted station ID always matches the visible MYCALL template token.
    const savedStation = await app().SaveStationSettings({ myCall: $('myCall').value, locator: $('locator').value });
    stationSettings = { myCall: savedStation?.myCall || '', locator: savedStation?.locator || '' };
    syncStationSettingsForm();
    await app().StartTX(exportCanvasDataURL(), mode.id, Number($('rate').value));
    updateAudioState({ ...audioState, txActive: true });
    setStatus(`正在准备发送 ${mode.name}…`);
  } catch (e) { setStatus(`启动 TX 失败：${String(e)}`, true); }
}
async function toggleTone() {
  if (toneActive) {
    try { await app().StopTX(); setStatus('已请求停止 1500 Hz 单音'); }
    catch (e) { setStatus(`停止单音失败：${String(e)}`, true); }
    return;
  }
  if (audioState.txActive) {
    setStatus('当前已有 SSTV 发射任务正在运行', true);
    return;
  }
  toneActive = true;
  updateAudioState(audioState);
  try {
    await app().StartTone(Number($('rate').value));
    setStatus('正在发送 1500 Hz 单音，最长 10 秒');
  } catch (e) {
    toneActive = false;
    updateAudioState(audioState);
    setStatus(`启动 1500 Hz 单音失败：${String(e)}`, true);
  }
}
async function stopTX() {
  try { await app().StopTX(); setStatus('已请求停止 TX'); }
  catch (e) { setStatus(`停止 TX 失败：${String(e)}`, true); }
}
async function toggleTX() {
  if (audioState.txActive) return stopTX();
  return startTX();
}

async function refreshAudioDevices() {
  const input = $('audioInput'), output = $('audioOutput');
  try {
    const lists = await app().GetAudioDevices();
    const fill = (select, items, defaultLabel) => {
      const previous = select.value; select.innerHTML = '';
      const base = document.createElement('option'); base.value = ''; base.textContent = defaultLabel; select.appendChild(base);
      (items || []).forEach(device => { const option = document.createElement('option'); option.value = device.id; option.textContent = `${device.name}${device.isDefault ? ' · 默认' : ''}`; select.appendChild(option); });
      if ([...select.options].some(o => o.value === previous)) select.value = previous;
    };
    fill(input, lists.inputs, '系统默认输入设备'); fill(output, lists.outputs, '系统默认输出设备');
    $('audioSettingsStatus').textContent = `发现 ${lists.inputs.length} 个输入设备和 ${lists.outputs.length} 个输出设备`;
  } catch (e) { $('audioSettingsStatus').textContent = `设备枚举失败：${String(e)}`; }
}
async function loadAudioSettings() {
  try {
    const cfg = await app().GetAudioSettings();
    $('rate').value = String(cfg.sampleRate || 48000);
    $('audioInput').value = cfg.captureDeviceId || ''; $('audioOutput').value = cfg.playbackDeviceId || '';
    $('pttMode').value = cfg.pttMode || 'vox'; $('rigctlAddress').value = cfg.rigctlAddress || '';
    const portSelect = $('serialPort');
    if (cfg.serialPort && ![...portSelect.options].some(o => o.value === cfg.serialPort)) {
      const option = document.createElement('option');
      option.value = cfg.serialPort;
      option.textContent = `${cfg.serialPort}（已断开）`;
      portSelect.appendChild(option);
    }
    portSelect.value = cfg.serialPort || '';
    $('pttActiveHigh').checked = cfg.activeHigh !== false;
    $('pttLead').value = cfg.pttLeadMs ?? 300; $('pttTail').value = cfg.pttTailMs ?? 250;
    updatePTTFields();
  } catch (e) { $('settingsStatus').textContent = `读取音频设置失败：${String(e)}`; $('settingsStatus').classList.add('error'); }
}
async function refreshSerialPorts() {
  const select = $('serialPort');
  const previous = select.value;
  try {
    const ports = await app().ListSerialPorts();
    select.innerHTML = '';
    const empty = document.createElement('option');
    empty.value = '';
    empty.textContent = ports.length ? '选择串口' : '未发现串口';
    select.appendChild(empty);
    (ports || []).forEach(port => {
      const option = document.createElement('option');
      option.value = port;
      option.textContent = port;
      select.appendChild(option);
    });
    if (previous && [...select.options].some(o => o.value === previous)) select.value = previous;
  } catch (e) {
    select.innerHTML = '';
    const empty = document.createElement('option');
    empty.value = '';
    empty.textContent = '串口枚举失败';
    select.appendChild(empty);
  }
}
function updatePTTFields() {
  const mode = $('pttMode').value;
  const show = (el, visible) => { el.style.display = visible ? '' : 'none'; };
  show($('rigctlField'), mode === 'hamlib');
  show($('serialField'), mode === 'rts' || mode === 'dtr');
  show($('activeHighField'), mode === 'rts' || mode === 'dtr');
}
async function saveAllSettings() {
  const button = $('saveAllSettings');
  const status = $('settingsStatus');
  button.disabled = true;
  status.textContent = '正在保存…';
  status.classList.remove('error');
  try {
    const saved = await app().SaveStationSettings({ myCall: $('settingsMyCall').value, locator: $('settingsLocator').value });
    stationSettings = { myCall: saved?.myCall || '', locator: saved?.locator || '' };
    $('myCall').value = stationSettings.myCall;
    $('locator').value = stationSettings.locator;
    syncStationSettingsForm();
    renderCanvas();
    const cfg = {
      sampleRate: Number($('rate').value), captureDeviceId: $('audioInput').value, playbackDeviceId: $('audioOutput').value,
      pttMode: $('pttMode').value, rigctlAddress: $('rigctlAddress').value.trim(), serialPort: $('serialPort').value.trim(),
      activeHigh: $('pttActiveHigh').checked, pttLeadMs: Number($('pttLead').value), pttTailMs: Number($('pttTail').value),
    };
    await app().ApplyAudioSettings(cfg, currentRXModeID());
    updateAudioState(await app().GetAudioState());
    status.textContent = '设置已保存并应用';
    setStatus('音频、PTT 与本站信息已保存并应用');
  } catch (e) {
    status.textContent = `保存失败：${String(e)}`;
    status.classList.add('error');
    setStatus(`保存设置失败：${String(e)}`, true);
  } finally { button.disabled = false; }
}

async function chooseImage() {
  try {
    const path = await app().SelectImage(); if (!path) return;
    const url = await app().LoadImagePreview(path);
    const img = new Image();
    img.onload = () => { sourceImage = img; selectedImagePath = path; $('imageName').textContent = path.split(/[\\/]/).pop(); renderCanvas(); setStatus('背景图片已载入'); };
    img.onerror = () => setStatus('无法载入所选图片', true); img.src = url;
  } catch (e) { setStatus(`载入图片失败：${String(e)}`, true); }
}
function clearBackground() { sourceImage = null; selectedImagePath = ''; $('imageName').textContent = '纯色背景，尚未载入图片'; renderCanvas(); }
async function encodeWAV() {
  const mode = activeTXMode(); if (!mode || !mode.ready) return setStatus('当前 TX 模式尚无可用编码器', true);
  const button = $('encode'); button.disabled = true; setStatus('正在合成画面并编码 WAV…');
  try {
    const dataURL = exportCanvasDataURL();
    const name = selectedImagePath ? selectedImagePath.split(/[\\/]/).pop() : 'SATSSTV-Go';
    const path = await app().EncodeImageData(dataURL, name, mode.id, Number($('rate').value));
    $('txStatus').textContent = 'WAV 已生成';
    let albumNote = '';
    try {
      await app().SaveAlbumImage(dataURL, 'tx', mode.id, 'WAV 已生成，等待实时发射');
      albumNote = '，已归档到 TX 相册';
      await refreshAlbum();
    } catch (albumError) {
      albumNote = `，相册归档失败：${String(albumError)}`;
    }
    setStatus(`WAV 已保存：${path}${albumNote}`);
  } catch (e) { setStatus(`编码失败：${String(e)}`, true); }
  finally { button.disabled = !activeTXMode()?.ready; }
}

async function refreshAlbum() {
  try { albumEntries = await app().ListAlbum(); renderAlbum(); }
  catch (e) { $('albumGrid').innerHTML = `<div class="album-empty">读取相册失败：${escapeHTML(String(e))}</div>`; }
}
function escapeHTML(s) { return String(s).replace(/[&<>"']/g, c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c])); }
function renderAlbum() {
  const filter = $('albumFilter').value, items = albumEntries.filter(e => filter === 'all' || e.kind === filter);
  $('albumCount').textContent = albumEntries.length;
  const host = $('albumGrid'); host.innerHTML = '';
  if (!items.length) { host.innerHTML = '<div class="album-empty">当前分类暂无图像。完成接收后会自动归档，也可以从文件导入历史图像。</div>'; return; }
  items.forEach((entry) => {
    const card = document.createElement('article'); card.className = 'album-card'; card.dataset.id = entry.id;
    const image = document.createElement('div'); image.className = 'album-thumb';
    const img = document.createElement('img'); img.alt = entry.filename; img.loading = 'lazy'; image.appendChild(img);
    const info = document.createElement('div'); info.className = 'album-info';
    const title = document.createElement('b'); title.textContent = `${entry.kind.toUpperCase()} · ${entry.mode || '图像'}`;
    const timestamp = document.createElement('time'); timestamp.textContent = entry.displayTime;
    const status = document.createElement('small'); status.textContent = entry.status || '已归档';
    info.append(title, timestamp, status); card.append(image, info);
    card.addEventListener('click', () => previewAlbumEntry(entry));
    card.addEventListener('contextmenu', (e) => { e.preventDefault(); openAlbumMenu(e.clientX, e.clientY, entry); });
    host.appendChild(card);
    app().LoadAlbumPreview(entry.id).then(url => { img.src = url; }).catch(() => { image.textContent = '无法读取图像'; });
  });
}
function openAlbumMenu(x, y, entry) {
  const menu = $('contextMenu'); menu.innerHTML = ''; const title = document.createElement('div'); title.className = 'context-title'; title.textContent = `${entry.kind.toUpperCase()} · ${entry.displayTime}`; menu.appendChild(title);
  const exportBtn = document.createElement('button'); exportBtn.className = 'context-item'; exportBtn.textContent = '导出带时间戳的 PNG';
  exportBtn.addEventListener('click', async () => { hideContextMenu(); try { const path = await app().ExportAlbumImage(entry.id); setStatus(`图像已导出：${path}`); } catch (e) { if (!String(e).includes('取消')) setStatus(`导出失败：${String(e)}`, true); } });
  menu.appendChild(exportBtn);
  const deleteBtn = document.createElement('button'); deleteBtn.className = 'context-item danger-context-item'; deleteBtn.textContent = '删除图像';
  deleteBtn.addEventListener('click', async () => {
    hideContextMenu();
    try { await app().DeleteAlbumImage(entry.id); await refreshAlbum(); await refreshLatestRXImage(); setStatus(`已删除相册图像：${entry.filename}`); }
    catch (e) { setStatus(`删除图像失败：${String(e)}`, true); }
  });
  menu.appendChild(deleteBtn); showContextMenu(x, y);
}
async function previewAlbumEntry(entry) {
  try { $('albumPreviewImage').src = await app().LoadAlbumPreview(entry.id); $('albumPreviewCaption').textContent = `${entry.kind.toUpperCase()} · ${entry.displayTime} · ${entry.mode || '图像'}`; $('albumPreviewDialog').showModal(); }
  catch (e) { setStatus(`预览失败：${String(e)}`, true); }
}
async function importAlbum(kind) {
  try { const entry = await app().ImportAlbumImage(kind); if (entry) { setStatus(`${kind.toUpperCase()} 图像已加入相册`); await refreshAlbum(); } }
  catch (e) { if (!String(e).includes('取消')) setStatus(`导入失败：${String(e)}`, true); }
}

function showPage(name) {
  document.querySelectorAll('.nav-btn').forEach(b => b.classList.toggle('active', b.dataset.page === name));
  $('workbenchPage').classList.toggle('active-page', name === 'workbench'); $('albumPage').classList.toggle('active-page', name === 'album'); $('appShell')?.classList.toggle('album-active', name === 'album');
  if (name === 'album') refreshAlbum();
}
function syncStationSettingsForm() {
  $('settingsMyCall').value = $('myCall').value;
  $('settingsLocator').value = $('locator').value;
}

async function loadStationSettings() {
  try {
    const saved = await app().GetStationSettings();
    stationSettings = { myCall: saved?.myCall || '', locator: saved?.locator || '' };
    $('myCall').value = stationSettings.myCall;
    $('locator').value = stationSettings.locator;
    syncStationSettingsForm();
    renderCanvas();
  } catch (e) {
    $('settingsStatus').textContent = `读取本站信息失败：${String(e)}`;
    $('settingsStatus').classList.add('error');
  }
}

function setupSettings() {
  const dialog = $('settingsDialog');
  $('openSettings').addEventListener('click', async () => {
    syncStationSettingsForm();
    await refreshAudioDevices();
    await refreshSerialPorts();
    await loadAudioSettings();
    updatePTTFields();
    dialog.showModal();
  });
  $('closeSettings').addEventListener('click', () => dialog.close());
  $('saveAllSettings').addEventListener('click', saveAllSettings);
  $('refreshAudioDevices').addEventListener('click', refreshAudioDevices);
  $('refreshSerialPorts').addEventListener('click', refreshSerialPorts);
  $('pttMode').addEventListener('change', updatePTTFields);
  dialog.addEventListener('click', e => { if (e.target === dialog) dialog.close(); });
  $('closePreview').addEventListener('click', () => $('albumPreviewDialog').close());
}
async function initializeAudioRX() {
  try {
    $('rxStatus').textContent = '正在初始化'; $('spectrumOverlay').textContent = '正在打开共享模式音频输入设备…';
    await app().SetRXEnabled(rxEnabled, currentRXModeID());
    updateAudioState(await app().GetAudioState());
    setStatus(!rxEnabled ? '音频设备已就绪，RX 当前已停止' : (autoSelected ? 'RX 已启动，正在自动识别 SSTV 模式' : `RX 已启动，指定模式：${activeRXMode()?.name || ''}`));
  } catch (e) {
    $('rxStatus').textContent = '音频初始化失败'; $('rxHint').textContent = String(e); $('spectrumOverlay').textContent = `音频初始化失败：${String(e)}。请检查设备设置。`;
    setStatus(`自动启动 RX 失败：${String(e)}`, true);
  }
}


// UI events. RX starts automatically on application launch and remains active.
$('chooseImage').addEventListener('click', chooseImage);
$('clearBackground').addEventListener('click', clearBackground);
$('encode').addEventListener('click', encodeWAV);
$('txToggle').addEventListener('click', toggleTX);
$('toneToggle').addEventListener('click', toggleTone);
$('addText').addEventListener('click', addTextComponent);
$('addImage').addEventListener('click', chooseTemplateImage);
$('addLastRX').addEventListener('click', addLatestRXImage);
$('deleteText').addEventListener('click', deleteSelectedComponent);
$('saveTemplate').addEventListener('click', saveCurrentTemplate);
$('componentText').addEventListener('input', updateSelectedComponent);
['fontSize', 'fontColor', 'posX', 'posY', 'fontBold', 'imageWidth', 'imageHeight', 'outlineEnabled', 'outlineWidth', 'outlineColor'].forEach(id => $(id).addEventListener('input', updateSelectedComponent));
VALUE_IDS.forEach(id => $(id).addEventListener('input', () => {
  if (id === 'myCall' || id === 'locator') syncStationSettingsForm();
  renderCanvas();
}));
$('importAlbumTX').addEventListener('click', () => importAlbum('tx'));
$('saveCapture').addEventListener('click', async () => {
  try {
    const path = await app().SaveCapturedAudio();
    setStatus(`采集音频已保存：${path}`);
  } catch (e) {
    setStatus(`保存采集失败：${String(e)}`, true);
  }
});
$('albumFilter').addEventListener('change', renderAlbum);
document.querySelectorAll('.nav-btn').forEach(b => b.addEventListener('click', () => showPage(b.dataset.page)));
document.addEventListener('pointerdown', e => { if (!$('contextMenu').contains(e.target)) hideContextMenu(); });
document.addEventListener('keydown', e => { if (e.key === 'Escape') hideContextMenu(); });

window.addEventListener('DOMContentLoaded', async () => {
  drawIdleSpectrum();
  setupSettings();
  setupCanvasDrag();
  bindRuntimeEvents();

  try {
    modes = await app().GetModes();
    loadSlots();
    loadTXSlots();
    slots = slots.map(id => modes.some(m => m.id === id) ? id : 'robot36');
    txSlots = txSlots.map(id => modes.some(m => m.id === id) ? id : 'robot36');
    activeTXSlot = Math.max(0, Math.min(7, activeTXSlot));
    renderModeSlots();
    renderTXModeSlots();
    $('modeInfo').textContent = rxEnabled ? `${modes.length} 种 RX 模式` : 'RX 已停止';
  } catch (e) {
    $('modeInfo').textContent = `模式读取失败：${String(e)}`;
  }

  await loadTemplateSlots();
  await loadStationSettings();
  await loadAudioSettings();
  try { updateAudioState(await app().GetAudioState()); } catch {}
  await initializeAudioRX();
  renderCanvas();
  await refreshAlbum();
});
