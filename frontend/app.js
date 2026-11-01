'use strict';

const $ = (id) => document.getElementById(id);
const app = () => window.go.main.App;
const SLOT_STORAGE = 'satsstv.modeSlots.v1';
const TX_SLOT_STORAGE = 'satsstv.txModeSlots.v1';
const TX_SLOT_ACTIVE_STORAGE = 'satsstv.txModeSlotActive.v1';
const TX_MODE_STORAGE = 'satsstv.txMode.v1'; // legacy single-mode TX selection
const RX_MODE_STORAGE = 'satsstv.rxMode.v1';
const VALUE_IDS = ['myCall', 'hisCall', 'qth', 'locator', 'report', 'message'];
const tokenDefaults = { MYCALL: 'N0CALL', HISCALL: 'ToCall', QTH: 'EXAMPLE QTH', LOCATOR: 'JJ00', REPORT: '595', MESSAGE: 'TEST MESSAGE', DATE: 'YYYY-MM-DD', TIME: 'HH:MM:SS' };

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
let txSlots = [];
let activeTXSlot = 0;
let imageLibraryEntries = [];
let components = [];
let selectedComponent = 0;
let albumEntries = [];
let pointerDrag = null;
let templateSlots = defaultTemplateSlots.map(t => ({ ...t, objects: t.objects.map(o => ({ ...o })) }));
let selectedTemplateSlot = 0;
let persistentTXBackground = null;
let txBackgroundSaveTimer = null;
let txBackgroundPersistChain = Promise.resolve();
let txBackgroundPersistVersion = 0;
const templateImageCache = new Map();
let contextTarget = null;
let stationSettings = { myCall: 'N0CALL', locator: 'JJ00' };
let audioState = { connected: false, rxActive: false, txActive: false, sampleRate: 48000, pttMode: 'vox' };
let waterfallHistory = []; // retained for compatibility with older UI state; rendering uses a circular offscreen buffer
let captureTelemetrySeen = false;
let latestRXImageData = '';
let templateRenderRevision = 0;
let pttTestActive = false;
let pttTestDeadline = 0;
let pttTestTimer = null;
const MAX_WATERFALL_ROWS = 64;
const t = (key, vars = {}) => window.SATSSTVI18n?.t(key, vars) || key;

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
  const localized = window.SATSSTVI18n?.localize(message) || message;
  $('txResult').textContent = localized;
  $('txResult').classList.toggle('error', error);
  $('globalStatus').textContent = localized;
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
    const savedSlot = Number(savedRX?.slot);
    activeSlot = Number.isInteger(savedSlot) && savedSlot >= 0 && savedSlot < 8 ? savedSlot : 0;
    if (!rxEnabled || autoSelected) activeSlot = -1;
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
function isAutoRXActuallyRunning() { return rxEnabled && autoSelected && !!audioState.rxActive; }
function shouldEnableAutoRX() { return !isAutoRXActuallyRunning(); }
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
  auto.title = rxEnabled && autoSelected ? t('rxSlotStopTitle', { slot: 'Auto' }) : t('rxAutoReceive');
  auto.addEventListener('click', async () => {
    const selectingAuto = shouldEnableAutoRX();
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
    button.title = selected ? t('rxSlotStopTitle', { slot: i + 1 }) : t('rxSlotChangeTitle', { slot: i + 1, mode: niceMode(id) });
    button.addEventListener('click', async () => {
      if (selected && audioState.rxActive) {
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
  const title = document.createElement('div'); title.className = 'context-title'; title.textContent = t('modeSlotRXTitle', { slot: slotIndex + 1 }); menu.appendChild(title);
  modes.forEach((m) => {
    const item = document.createElement('button'); item.className = 'context-item';
    item.textContent = `${m.name}${m.ready ? '' : ` · ${t('modeNotReady')}`}`;
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
    $('modeInfo').textContent = t('rxStoppedLabel');
    $('rxDetectedMode').textContent = '---';
    $('rxAutoState').textContent = t('rxStoppedLabel');
  } else if (autoSelected) {
    $('modeInfo').textContent = t('rxAutoAllModes');
    $('rxDetectedMode').textContent = 'Auto';
    $('rxAutoState').textContent = t('rxAutoReceive');
  } else {
    $('modeInfo').textContent = `${mode?.name || t('empty')} · ${t('fixedModeReceive')}`;
    $('rxDetectedMode').textContent = mode?.name || '---';
    $('rxAutoState').textContent = `${mode?.name || ''} · ${t('fixedModeReceive')}`;
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
    button.title = t('txSlotTitle', { slot: i + 1, mode: niceMode(id) });
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
  updateFontSizeLimits();
}
function openTXModeMenu(x, y, slotIndex) {
  contextTarget = { type: 'txmode', slotIndex };
  const menu = $('contextMenu'); menu.innerHTML = '';
  const title = document.createElement('div'); title.className = 'context-title'; title.textContent = t('modeSlotTXTitle', { slot: slotIndex + 1 }); menu.appendChild(title);
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
function drawBackgroundImage(ctx, img, component, width, height) {
  const zoom = Math.max(100, Math.min(400, Number(component.width) || 100)) / 100;
  const scale = Math.max(width / img.naturalWidth, height / img.naturalHeight) * zoom;
  const drawWidth = img.naturalWidth * scale;
  const drawHeight = img.naturalHeight * scale;
  const x = (width - drawWidth) / 2 + (Number(component.x) || 0) / 100 * width;
  const y = (height - drawHeight) / 2 + (Number(component.y) || 0) / 100 * height;
  ctx.drawImage(img, x, y, drawWidth, drawHeight);
}
function imageAspectRatio(c) {
  if (c?.type === 'last_rx' && latestRXImageData) {
    const img = getTemplateImage(c);
    if (img?.complete && img.naturalWidth && img.naturalHeight) return img.naturalWidth / img.naturalHeight;
  } else if (c?.imageData) {
    const img = getTemplateImage(c);
    if (img?.complete && img.naturalWidth && img.naturalHeight) return img.naturalWidth / img.naturalHeight;
  }
  return 4 / 3;
}
function componentBounds(ctx, c, width, height) {
  if (isBackgroundComponent(c)) return { x: 0, y: 0, width, height, image: true, background: true };
  const x = (Number(c.x) || 0) / 100 * width;
  const y = (Number(c.y) || 0) / 100 * height;
  if (isImageComponent(c)) {
    const imageWidth = Math.max(1, (Number(c.width) || 42) / 100 * width);
    const aspect = Math.max(0.05, imageAspectRatio(c));
    return { x, y, width: imageWidth, height: imageWidth / aspect, image: true };
  }
  const maxFontSize = maxFontSizeForMode();
  const fontSize = Math.max(6, Math.min(maxFontSize, Number(c.fontSize) || 18));
  const text = resolveTokens(c.text || '');
  const lines = text.split('\n');
  ctx.font = `${c.bold ? '700' : '400'} ${fontSize}px "Segoe UI", "Microsoft YaHei", sans-serif`;
  const lineHeight = fontSize * 1.2;
  const maxWidth = Math.max(1, ...lines.map((line) => ctx.measureText(line).width));
  return { text, x, y, width: maxWidth, height: lineHeight * lines.length, lines, lineHeight, fontSize, image: false };
}
function isBackgroundComponent(c) { return c?.type === 'background'; }
function isImageComponent(c) { return c?.type === 'image' || c?.type === 'last_rx' || isBackgroundComponent(c); }
function getTemplateImage(c) {
  const dataURL = c?.type === 'last_rx' ? latestRXImageData : c?.imageData;
  if (!dataURL) return null;
  let image = templateImageCache.get(dataURL);
  if (image) return image;
  image = new Image();
  image.onload = () => renderCanvas();
  image.onerror = () => {
    templateImageCache.delete(dataURL);
    setStatus(`无法载入模板图片：${c?.imageName || '图像'}`, true);
  };
  image.src = dataURL;
  templateImageCache.set(dataURL, image);
  return image;
}
function maxFontSizeForMode(mode = activeTXMode()) {
  const dimension = Math.max(24, Math.min(Number(mode?.width) || 320, Number(mode?.height) || 240));
  return Math.max(12, Math.min(160, Math.floor(dimension * 0.32)));
}
function updateFontSizeLimits() {
  const limit = maxFontSizeForMode();
  $('fontSize').max = String(limit);
  $('fontSize').title = t('fontLimitTitle', { limit });
  components.forEach(c => {
    if (c.type === 'text' && Number(c.fontSize) > limit) c.fontSize = limit;
  });
  const selected = components[selectedComponent];
  if (selected?.type === 'text') {
    $('fontSize').value = String(Math.min(limit, Math.max(6, Number(selected.fontSize) || 18)));
    $('fontSizeValue').textContent = `${$('fontSize').value} / ${limit} px`;
  }
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
  ctx.fillText(t('lastRXImage'), bounds.x + bounds.width / 2, bounds.y + bounds.height / 2 - 5, Math.max(1, bounds.width - 8));
  ctx.font = `400 ${Math.max(6, Math.min(10, bounds.width * .055))}px "Segoe UI", "Microsoft YaHei", sans-serif`;
  ctx.fillText(t('waitingRXImage'), bounds.x + bounds.width / 2, bounds.y + bounds.height / 2 + 11, Math.max(1, bounds.width - 8));
  ctx.restore();
}
function renderToCanvas(canvas, selectionOutline = false) {
  const mode = activeTXMode();
  const width = Math.max(160, mode?.width || 320), height = Math.max(120, mode?.height || 240);
  canvas.width = width; canvas.height = height;
  const ctx = canvas.getContext('2d');
  const background = components.find(isBackgroundComponent);
  const backgroundImage = background ? getTemplateImage(background) : null;
  if (backgroundImage?.complete && backgroundImage.naturalWidth) {
    drawBackgroundImage(ctx, backgroundImage, background, width, height);
  } else {
    const grad = ctx.createLinearGradient(0, 0, width, height);
    grad.addColorStop(0, '#24394d'); grad.addColorStop(1, '#111b29');
    ctx.fillStyle = grad; ctx.fillRect(0, 0, width, height);
    ctx.fillStyle = 'rgba(255,255,255,.07)'; ctx.beginPath(); ctx.arc(width * .8, height * .25, height * .28, 0, Math.PI * 2); ctx.fill();
  }
  components.forEach((c, idx) => {
    if (isBackgroundComponent(c)) {
      if (selectionOutline && idx === selectedComponent) {
        ctx.save(); ctx.strokeStyle = '#5bcefa'; ctx.lineWidth = Math.max(1, width / 320);
        ctx.setLineDash([4, 3]); ctx.strokeRect(1, 1, width - 2, height - 2); ctx.restore();
      }
      return;
    }
    const b = componentBounds(ctx, c, width, height);
    const outline = componentOutline(c);
    if (isImageComponent(c)) {
      const img = getTemplateImage(c);
      if (img?.complete && img.naturalWidth) {
        ctx.drawImage(img, b.x, b.y, b.width, b.height);
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
      ctx.font = `${c.bold ? '700' : '400'} ${b.fontSize}px "Segoe UI", "Microsoft YaHei", sans-serif`;
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
  if (!background && components.length === 0) {
    ctx.save(); ctx.font = `600 ${Math.max(7, width * .025)}px "Segoe UI", sans-serif`;
    ctx.fillStyle = 'rgba(255,255,255,.34)'; ctx.textAlign = 'right'; ctx.fillText('SATSSTV-GO', width * .97, height * .04); ctx.restore();
  }
  if (canvas === $('txCanvas')) {
    $('canvasDimensions').textContent = `${width} × ${height}`;
    $('imageName').textContent = background ? `${t('backgroundPrefix')}${background.imageName || t('templateImage')}（${t('imageDragScaleHint')}）` : t('defaultBackgroundColor');
  }
}
function renderCanvas() { renderToCanvas($('txCanvas'), true); }
function exportCanvasDataURL() { const canvas = document.createElement('canvas'); renderToCanvas(canvas, false); return canvas.toDataURL('image/png'); }

function renderComponentList() {
  const host = $('componentList'); host.innerHTML = '';
  components.forEach((c, i) => {
    const row = document.createElement('button'); row.className = `component-item${selectedComponent === i ? ' selected' : ''}`;
    const sample = document.createElement('span'); sample.className = 'component-sample';
    sample.textContent = isBackgroundComponent(c) ? 'BG' : (isImageComponent(c) ? (c.type === 'last_rx' ? 'RX' : 'IMG') : (c.text || '').replace(/\{[A-Z]+\}/g, '…').slice(0, 1) || 'T');
    sample.style.color = c.color || '#5b8ba0';
    const name = document.createElement('span'); name.className = 'component-name';
    name.textContent = isBackgroundComponent(c) ? `${t('backgroundPrefix')}${c.imageName || t('templateImage')}` : (isImageComponent(c) ? (c.type === 'last_rx' ? t('lastRXImage') : (c.imageName || t('templateImage'))) : (c.text || '').replace(/\s+/g, ' ').slice(0, 28) || `${t('customText')} ${i + 1}`);
    const meta = document.createElement('small'); meta.textContent = isImageComponent(c) ? `${Math.round(c.width || 42)}%` : `${c.fontSize}px`;
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
  const isBackground = isBackgroundComponent(c);
  ['componentText', 'fontSize', 'fontColor', 'fontBold'].forEach(id => { $(id).disabled = !c || isImage; });
  ['posX', 'posY', 'deleteText'].forEach(id => { $(id).disabled = !c; });
  $('imageWidth').disabled = !isImage;
  $('imageProperties').hidden = !isImage;
  $('componentText').parentElement.hidden = !c || isImage;
  $('fontBold').parentElement.hidden = !c || isImage;
  $('outlineProperties').hidden = !c || isBackground;
  ['outlineEnabled', 'outlineWidth', 'outlineColor'].forEach(id => { $(id).disabled = !c || isBackground; });
  $('imageWidth').parentElement.firstChild.textContent = `${isBackground ? t('backgroundScale') : t('imageScaleLabel')} `;
  $('posX').parentElement.firstChild.textContent = `${isBackground ? t('horizontalOffset') : t('horizontalPositionLabel')} `;
  $('posY').parentElement.firstChild.textContent = `${isBackground ? t('verticalOffset') : t('verticalPositionLabel')} `;
  $('posX').min = isBackground ? '-150' : '0'; $('posX').max = isBackground ? '150' : '100';
  $('posY').min = isBackground ? '-150' : '0'; $('posY').max = isBackground ? '150' : '100';
  if (!c) return;
  const outline = componentOutline(c);
  $('outlineEnabled').checked = outline.enabled;
  $('outlineWidth').value = String(outline.width);
  $('outlineWidthValue').textContent = `${outline.width.toFixed(1)} px`;
  $('outlineColor').value = outline.color;
  $('posX').value = c.x || 0; $('xValue').textContent = `${Math.round(c.x || 0)}%`;
  $('posY').value = c.y || 0; $('yValue').textContent = `${Math.round(c.y || 0)}%`;
  if (isImage) {
    $('imageWidth').min = isBackground ? '100' : '5';
    $('imageWidth').max = isBackground ? '400' : '100';
    $('imageWidth').value = String(Math.max(Number($('imageWidth').min), Math.min(Number($('imageWidth').max), Number(c.width) || (isBackground ? 100 : 42))));
    $('imageWidthValue').textContent = `${Math.round(Number($('imageWidth').value))}%`;
  } else {
    $('componentText').value = c.text || '';
    $('fontSize').value = Math.min(maxFontSizeForMode(), c.fontSize || 18);
    $('fontSizeValue').textContent = `${$('fontSize').value} / ${maxFontSizeForMode()} px`;
    $('fontColor').value = c.color || '#ffffff'; $('fontBold').checked = !!c.bold;
  }
}
function updateSelectedComponent() {
  const c = components[selectedComponent]; if (!c) return;
  c.x = Number($('posX').value); c.y = Number($('posY').value);
  if (isImageComponent(c)) {
    c.width = Number($('imageWidth').value);
    if (isBackgroundComponent(c)) {
      c.width = Math.max(100, Math.min(400, c.width));
      c.x = Math.max(-150, Math.min(150, c.x)); c.y = Math.max(-150, Math.min(150, c.y));
    } else {
      const b = componentBounds($('txCanvas').getContext('2d'), c, $('txCanvas').width, $('txCanvas').height);
      c.x = Math.max(0, Math.min(Math.max(0, 100 - b.width / $('txCanvas').width * 100), c.x));
      c.y = Math.max(0, Math.min(Math.max(0, 100 - b.height / $('txCanvas').height * 100), c.y));
    }
    $('posX').value = c.x; $('posY').value = c.y;
    $('imageWidthValue').textContent = `${Math.round(c.width)}%`;
  } else {
    c.text = $('componentText').value; c.fontSize = Math.min(maxFontSizeForMode(), Number($('fontSize').value)); c.color = $('fontColor').value; c.bold = $('fontBold').checked;
    $('fontSizeValue').textContent = `${c.fontSize} / ${maxFontSizeForMode()} px`;
  }
  c.outlineEnabled = $('outlineEnabled').checked;
  c.outlineWidth = Number($('outlineWidth').value);
  c.outlineColor = $('outlineColor').value;
  $('outlineWidthValue').textContent = `${c.outlineWidth.toFixed(1)} px`;
  $('xValue').textContent = `${Math.round(c.x)}%`; $('yValue').textContent = `${Math.round(c.y)}%`;
  renderComponentList(); renderCanvas();
  if (isBackgroundComponent(c)) scheduleTXBackgroundPersist();
}
function addTextComponent() {
  components.push(withOutlineDefaults({ type: 'text', text: '自定义文字', x: 8, y: 15 + (components.length * 8 % 65), fontSize: Math.min(18, maxFontSizeForMode()), color: '#ffffff', bold: true }));
  selectComponent(components.length - 1);
}
async function addImageComponent(dataURL, imageName = '模板图像') {
  if (!dataURL) return;
  const image = new Image();
  image.onload = () => {
    components.push(withOutlineDefaults({ type: 'image', imageData: dataURL, imageName, x: 8, y: 18, width: 42 }));
    templateImageCache.set(dataURL, image);
    selectComponent(components.length - 1);
  };
  image.onerror = () => setStatus('无法载入模板图像', true);
  image.src = dataURL;
}
function setBackgroundImage(dataURL, imageName = '图库图片') {
  if (!dataURL) return;
  const image = new Image();
  image.onload = () => {
    templateImageCache.set(dataURL, image);
    const current = components.findIndex(isBackgroundComponent);
    const background = withOutlineDefaults({ type: 'background', imageData: dataURL, imageName, x: 0, y: 0, width: 100 });
    if (current >= 0) {
      const previous = components[current];
      background.x = previous.x || 0; background.y = previous.y || 0; background.width = previous.width || 100;
      components[current] = background;
      selectedComponent = current;
    } else {
      components.unshift(background);
      selectedComponent = 0;
    }
    persistentTXBackground = { ...background };
    renderComponentList(); renderCanvas();
    void persistTXBackgroundNow();
    setStatus(`已设置模板底图：${imageName}`);
  };
  image.onerror = () => setStatus('无法载入底图', true);
  image.src = dataURL;
}
async function openImageLibrary() {
  $('imageLibraryDialog').showModal();
  await refreshImageLibrary();
}
async function refreshImageLibrary() {
  const grid = $('imageLibraryGrid');
  grid.innerHTML = `<div class="image-library-empty">${t('libraryLoading')}</div>`;
  try {
    imageLibraryEntries = await app().ListImageLibrary();
    grid.innerHTML = '';
    if (!imageLibraryEntries.length) {
      grid.innerHTML = `<div class="image-library-empty">${t('libraryEmpty')}</div>`;
      return;
    }
    imageLibraryEntries.forEach(entry => {
      const card = document.createElement('article'); card.className = 'image-library-card';
      const preview = document.createElement('img'); preview.src = entry.thumbnail; preview.alt = entry.name;
      const details = document.createElement('div'); details.className = 'image-library-details';
      const title = document.createElement('b'); title.textContent = entry.name;
      const meta = document.createElement('small'); meta.textContent = `${entry.width} × ${entry.height} · ${(entry.size / 1024).toFixed(1)} KB`;
      details.append(title, meta);
      const actions = document.createElement('div'); actions.className = 'image-library-actions';
      const backgroundButton = document.createElement('button'); backgroundButton.className = 'small-btn'; backgroundButton.textContent = t('setAsBackground');
      backgroundButton.addEventListener('click', async () => {
        try { const dataURL = await app().LoadImageLibraryImage(entry.id); setBackgroundImage(dataURL, entry.name); $('imageLibraryDialog').close(); }
        catch (e) { setStatus(`载入底图失败：${String(e)}`, true); }
      });
      const componentButton = document.createElement('button'); componentButton.className = 'small-btn'; componentButton.textContent = t('addLayer');
      componentButton.addEventListener('click', async () => {
        try { const dataURL = await app().LoadImageLibraryImage(entry.id); await addImageComponent(dataURL, entry.name); $('imageLibraryDialog').close(); }
        catch (e) { setStatus(`添加图像失败：${String(e)}`, true); }
      });
      actions.append(backgroundButton, componentButton);
      card.append(preview, details, actions); grid.appendChild(card);
    });
  } catch (e) {
    grid.innerHTML = `<div class="image-library-empty">${t('libraryReadFailed', { error: String(e) })}</div>`;
  }
}
async function importImageToLibrary() {
  const button = $('importLibraryImage'); button.disabled = true;
  try {
    const entry = await app().ImportImageToLibrary();
    setStatus(`已导入图库：${entry.name}`);
    await refreshImageLibrary();
  } catch (e) {
    if (!String(e).includes('取消') && !String(e).includes('cancel')) setStatus(`导入图库失败：${String(e)}`, true);
  } finally { button.disabled = false; }
}
async function clearBackground() {
  if (txBackgroundSaveTimer !== null) {
    clearTimeout(txBackgroundSaveTimer);
    txBackgroundSaveTimer = null;
  }
  const index = components.findIndex(isBackgroundComponent);
  if (index >= 0) {
    components.splice(index, 1);
    selectedComponent = Math.max(0, Math.min(selectedComponent > index ? selectedComponent - 1 : selectedComponent, components.length - 1));
  }
  persistentTXBackground = null;
  txBackgroundPersistVersion++;
  // A deliberate clear must also remove legacy per-slot copies; otherwise
  // startup migration would restore an old template background next launch.
  const withoutBackgrounds = templateSlots.map((slot) => ({
    ...slot,
    objects: (slot.objects || []).filter((object) => !isBackgroundComponent(object)),
  }));
  renderComponentList(); renderCanvas();
  try {
    // Remove per-slot copies first; only then clear the global setting. If the
    // slot write fails, the previous persistent background remains recoverable.
    templateSlots = await app().SaveTemplateSlots(withoutBackgrounds);
    await queueTXBackgroundPersist(null);
    renderTemplates();
    setStatus('已清除共享底图');
  } catch (e) {
    setStatus(`清除共享底图失败：${String(e)}`, true);
  }
}
function addLatestRXImage() {
  components.push(withOutlineDefaults({ type: 'last_rx', imageName: '上一次 RX 图像', x: 8, y: 18, width: 42 }));
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
function templateDisplayName(name, index) { return !name || name === '空白模板' ? t('blankTemplate') : name; }
function copyBackground(background) { return background ? withOutlineDefaults({ ...background }) : null; }
function queueTXBackgroundPersist(background) {
  const snapshot = copyBackground(background);
  txBackgroundPersistChain = txBackgroundPersistChain.catch(() => {}).then(() =>
    snapshot ? app().SaveTXBackground(snapshot) : app().ClearTXBackground()
  );
  return txBackgroundPersistChain;
}
async function persistTXBackgroundNow() {
  if (txBackgroundSaveTimer !== null) {
    clearTimeout(txBackgroundSaveTimer);
    txBackgroundSaveTimer = null;
  }
  const background = components.find(isBackgroundComponent) || persistentTXBackground;
  persistentTXBackground = copyBackground(background);
  const version = ++txBackgroundPersistVersion;
  try {
    await queueTXBackgroundPersist(persistentTXBackground);
    if (version === txBackgroundPersistVersion) setStatus('共享底图已保存');
  } catch (e) {
    if (version === txBackgroundPersistVersion) setStatus(`保存共享底图失败：${String(e)}`, true);
  }
}
function scheduleTXBackgroundPersist() {
  if (txBackgroundSaveTimer !== null) clearTimeout(txBackgroundSaveTimer);
  txBackgroundSaveTimer = setTimeout(() => { void persistTXBackgroundNow(); }, 250);
}
function refreshTemplatePreview(revision = templateRenderRevision) {
  if (revision !== templateRenderRevision) return;
  updateFontSizeLimits();
  renderTemplates();
  renderComponentList(); // also synchronizes every property editor control
  renderCanvas();
}
function loadTemplateAt(index) {
  selectedTemplateSlot = Math.max(0, Math.min(3, index));
  const template = templateSlots[selectedTemplateSlot];
  if (!template) return;
  const slotObjects = templateData(template);
  const slotBackground = slotObjects.find(isBackgroundComponent) || null;
  const currentBackground = components.find(isBackgroundComponent) || null;
  const sharedBackground = copyBackground(persistentTXBackground || currentBackground || slotBackground);
  components = slotObjects.filter((object) => !isBackgroundComponent(object));
  if (sharedBackground) {
    components.unshift(sharedBackground);
    if (!persistentTXBackground) {
      persistentTXBackground = copyBackground(sharedBackground);
      void persistTXBackgroundNow();
    }
  }
  selectedComponent = components.length ? 0 : -1;
  $('templateName').value = template.name || t('blankTemplate');
  const revision = ++templateRenderRevision;
  // Paint immediately, then repaint after the WebView has committed the updated
  // template DOM/layout. This prevents the canvas appearing stale until another
  // unrelated mode change causes a redraw.
  refreshTemplatePreview(revision);
  requestAnimationFrame(() => refreshTemplatePreview(revision));
  setStatus(t('templateLoaded', { slot: selectedTemplateSlot + 1, name: templateDisplayName(template.name, selectedTemplateSlot), background: sharedBackground ? `（${t('backgroundKept') || '保留共享底图'}）` : '' }));
}
function renderTemplates() {
  const host = $('templatePresets'); host.innerHTML = '';
  templateSlots.forEach((templateSlot, index) => {
    const objects = Array.isArray(templateSlot.objects) ? templateSlot.objects : [];
    const b = document.createElement('button'); b.type = 'button'; b.className = `template-card${index === selectedTemplateSlot ? ' selected-template' : ''}`;
    const preview = document.createElement('span'); preview.className = 'template-thumb';
    const textObjects = objects.filter(o => !isImageComponent(o));
    const mainText = document.createElement('b'); mainText.textContent = (textObjects[0]?.text || (objects.some(o => isImageComponent(o)) ? 'IMAGE' : (index === 3 ? t('blankTemplate') : 'TEXT'))).replace(/\{[A-Z]+\}/g, '…').slice(0, 16);
    const secondary = document.createElement('i'); secondary.textContent = textObjects[1]?.text?.replace(/\{[A-Z]+\}/g, '…').slice(0, 18) || (objects.some(o => isImageComponent(o)) ? t('containsImageComponent') : '');
    preview.append(mainText, secondary);
    const label = document.createElement('span'); label.className = 'template-name'; label.textContent = `${index + 1}. ${templateDisplayName(templateSlot.name, index)}`;
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
  try {
    persistentTXBackground = copyBackground(await app().GetTXBackground());
  } catch (e) {
    persistentTXBackground = null;
    setStatus(`读取共享底图失败：${String(e)}`, true);
  }
  // Migrate backgrounds saved inside legacy template slots to the new global
  // setting. Pick the first saved one so old projects keep their last artwork.
  if (!persistentTXBackground) {
    for (const slot of templateSlots) {
      const legacy = (slot.objects || []).find(isBackgroundComponent);
      if (legacy) {
        persistentTXBackground = copyBackground(legacy);
        try { await queueTXBackgroundPersist(persistentTXBackground); }
        catch (e) { setStatus(`迁移旧模板底图失败：${String(e)}`, true); }
        break;
      }
    }
  }
  loadTemplateAt(0);
  await refreshLatestRXImage();
}

function findComponentAt(x, y) {
  const canvas = $('txCanvas'), ctx = canvas.getContext('2d');
  // Foreground layers always take hit priority over the full-canvas background.
  for (let i = components.length - 1; i >= 0; i--) {
    if (isBackgroundComponent(components[i])) continue;
    const b = componentBounds(ctx, components[i], canvas.width, canvas.height);
    if (x >= b.x - 10 && x <= b.x + b.width + 10 && y >= b.y - 10 && y <= b.y + b.height + 10) return i;
  }
  const backgroundIndex = components.findIndex(isBackgroundComponent);
  return backgroundIndex;
}
function canvasPoint(event) {
  const canvas = $('txCanvas'), rect = canvas.getBoundingClientRect();
  return { x: (event.clientX - rect.left) * canvas.width / rect.width, y: (event.clientY - rect.top) * canvas.height / rect.height };
}
function setupCanvasDrag() {
  const canvas = $('txCanvas');
  canvas.addEventListener('pointerdown', (e) => {
    const p = canvasPoint(e), index = findComponentAt(p.x, p.y); if (index < 0) return;
    selectComponent(index);
    const c = components[index];
    pointerDrag = isBackgroundComponent(c)
      ? { index, startX: p.x, startY: p.y, originX: Number(c.x) || 0, originY: Number(c.y) || 0, background: true }
      : { index, offsetX: p.x - c.x / 100 * canvas.width, offsetY: p.y - c.y / 100 * canvas.height, background: false };
    canvas.setPointerCapture(e.pointerId); e.preventDefault();
  });
  canvas.addEventListener('pointermove', (e) => {
    if (!pointerDrag) return;
    const p = canvasPoint(e), c = components[pointerDrag.index];
    if (pointerDrag.background) {
      c.x = Math.max(-150, Math.min(150, pointerDrag.originX + (p.x - pointerDrag.startX) / canvas.width * 100));
      c.y = Math.max(-150, Math.min(150, pointerDrag.originY + (p.y - pointerDrag.startY) / canvas.height * 100));
    } else {
      const b = componentBounds(canvas.getContext('2d'), c, canvas.width, canvas.height);
      const maxX = Math.max(0, 100 - b.width / canvas.width * 100);
      const maxY = Math.max(0, 100 - b.height / canvas.height * 100);
      c.x = Math.max(0, Math.min(maxX, (p.x - pointerDrag.offsetX) / canvas.width * 100));
      c.y = Math.max(0, Math.min(maxY, (p.y - pointerDrag.offsetY) / canvas.height * 100));
    }
    updateComponentProperties(); renderComponentList(); renderCanvas();
    if (pointerDrag.background) scheduleTXBackgroundPersist();
  });
  const end = () => {
    const wasBackground = pointerDrag?.background;
    pointerDrag = null;
    if (wasBackground) void persistTXBackgroundNow();
  };
  canvas.addEventListener('pointerup', end); canvas.addEventListener('pointercancel', end);
}

let waterfallOff = null;
let waterfallNextRow = 0;
let waterfallRowImage = null;
let currentInputDBFS = -120;
function resetWaterfall() {
  waterfallOff = null;
  waterfallRowImage = null;
  waterfallNextRow = 0;
  waterfallHistory = [];
}
function appendWaterfallRow(values) {
  if (!Array.isArray(values) || !values.length) return;
  const bins = values.length;
  if (!waterfallOff || waterfallOff.width !== bins) {
    waterfallOff = document.createElement('canvas');
    waterfallOff.width = bins; waterfallOff.height = MAX_WATERFALL_ROWS;
    const offCtx = waterfallOff.getContext('2d');
    offCtx.fillStyle = 'rgb(5,10,22)'; offCtx.fillRect(0, 0, bins, MAX_WATERFALL_ROWS);
    waterfallRowImage = offCtx.createImageData(bins, 1);
    waterfallNextRow = 0;
  }
  const data = waterfallRowImage.data;
  for (let i = 0; i < bins; i++) {
    const value = Math.max(0, Math.min(1, (Number(values[i]) - 0.08) / 0.76));
    const position = Math.pow(value, 0.78);
    let gray;
    if (position <= 0.28) gray = 5 + (12 - 5) * (position / 0.28);
    else if (position <= 0.52) gray = 12 + (0 - 12) * ((position - 0.28) / 0.24);
    else if (position <= 0.76) gray = 0 + (238 - 0) * ((position - 0.52) / 0.24);
    else gray = 238 + (255 - 238) * ((position - 0.76) / 0.24);
    const p = i * 4; data[p] = gray; data[p + 1] = gray; data[p + 2] = gray; data[p + 3] = 255;
  }
  waterfallOff.getContext('2d').putImageData(waterfallRowImage, 0, waterfallNextRow);
  waterfallNextRow = (waterfallNextRow + 1) % MAX_WATERFALL_ROWS;
}
function drawWaterfall(ctx, w, waterfallY, waterfallH, xOffset = 0) {
  if (!waterfallOff || w <= 0 || waterfallH <= 0) return;

  // The circular buffer is stored as rows. Once it wraps, the oldest row is
  // waterfallNextRow, so stitch [nextRow..end] followed by [0..nextRow].
  // IMPORTANT: drawImage source coordinates are (sx, sy, sw, sh). The row
  // pointer belongs in sy, never sx; using it as sx made the image drift left
  // as the ring advanced and exposed duplicated/cropped-looking bands.
  const splitRow = Math.max(0, Math.min(MAX_WATERFALL_ROWS,
    Math.trunc(waterfallNextRow)));
  const olderRows = MAX_WATERFALL_ROWS - splitRow;
  const splitHeight = waterfallH * olderRows / MAX_WATERFALL_ROWS;

  ctx.save();
  ctx.beginPath();
  ctx.rect(xOffset, waterfallY, w, waterfallH);
  ctx.clip();
  ctx.imageSmoothingEnabled = false;

  if (olderRows > 0) {
    ctx.drawImage(waterfallOff,
      0, splitRow, waterfallOff.width, olderRows,
      xOffset, waterfallY, w, splitHeight);
  }
  if (splitRow > 0) {
    ctx.drawImage(waterfallOff,
      0, 0, waterfallOff.width, splitRow,
      xOffset, waterfallY + splitHeight, w, waterfallH - splitHeight);
  }
  ctx.restore();
}
function drawIdleSpectrum() {
  const canvas = $('spectrum'), ctx = canvas.getContext('2d'), w = canvas.width, h = canvas.height;
  const meterW = 42;
  const plotX = meterW + 2;
  const plotW = w - plotX;
  const plotH = Math.round(h * 0.58);
  const waterfallY = plotH + 2;
  const waterfallH = h - waterfallY;

  // MMSSTV-style independent receiver level rail at the left of both panes.
  ctx.fillStyle = '#f1f1ed';
  ctx.fillRect(0, 0, meterW, h);
  ctx.strokeStyle = '#7d858b';
  ctx.strokeRect(0.5, 0.5, meterW - 1, h - 1);
  const meterTop = 22, meterBottom = h - 9, meterH = meterBottom - meterTop;
  ctx.fillStyle = '#fff000';
  ctx.fillRect(3, meterTop, meterW - 7, meterH);
  const level = Math.max(0, Math.min(1, (currentInputDBFS + 60) / 60));
  const fillH = meterH * level;
  ctx.fillStyle = '#d9a900';
  ctx.fillRect(3, meterBottom - fillH, meterW - 7, fillH);
  ctx.strokeStyle = '#a5a500';
  ctx.strokeRect(3.5, meterTop + .5, meterW - 8, meterH - 1);
  ctx.fillStyle = '#26313a';
  ctx.font = '10px Segoe UI, sans-serif';
  ctx.textAlign = 'center';
  ctx.textBaseline = 'top';
  ctx.fillText('RX', meterW / 2, 4);
  ctx.textBaseline = 'bottom';
  ctx.fillText(`${Math.round(currentInputDBFS)}dB`, meterW / 2, h - 1);
  ctx.textAlign = 'left';
  ctx.textBaseline = 'alphabetic';

  // MMSSTV-inspired orange spectrum pane and high-contrast monochrome waterfall.
  ctx.fillStyle = '#ff813f';
  ctx.fillRect(plotX, 0, plotW, plotH);
  ctx.strokeStyle = 'rgba(117,59,21,.18)';
  ctx.lineWidth = 1;
  for (let x = 0; x <= plotW; x += plotW / 12) {
    ctx.beginPath(); ctx.moveTo(plotX + x, 18); ctx.lineTo(plotX + x, plotH); ctx.stroke();
  }
  for (let y = 28; y < plotH; y += (plotH - 28) / 4) {
    ctx.beginPath(); ctx.moveTo(plotX, y); ctx.lineTo(w, y); ctx.stroke();
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
    const x = plotX + (marker.frequency - 700) / 2000 * plotW;
    ctx.strokeStyle = 'rgba(255,238,65,.9)';
    ctx.lineWidth = 1.2;
    ctx.beginPath(); ctx.moveTo(x, 18); ctx.lineTo(x, plotH); ctx.stroke();
    ctx.fillStyle = '#27313b';
    ctx.fillText(marker.label, x, 1);
  });
  ctx.strokeStyle = '#36414a';
  ctx.beginPath(); ctx.moveTo(plotX, plotH - 1); ctx.lineTo(w, plotH - 1); ctx.stroke();

  ctx.fillStyle = '#050a16';
  ctx.fillRect(plotX, waterfallY, plotW, waterfallH);
  drawWaterfall(ctx, plotW, waterfallY, waterfallH, plotX);
  ctx.strokeStyle = '#31465c';
  ctx.beginPath(); ctx.moveTo(plotX, waterfallY); ctx.lineTo(w, waterfallY); ctx.stroke();
}
function renderLiveSpectrum(values) {
  const canvas = $('spectrum'), ctx = canvas.getContext('2d'), w = canvas.width, h = canvas.height;
  if (!Array.isArray(values) || !values.length) { drawIdleSpectrum(); return; }
  const normalized = values.map(v => Math.max(0, Math.min(1, Number(v) || 0)));
  appendWaterfallRow(normalized);

  drawIdleSpectrum();
  const plotX = 44;
  const plotW = w - plotX;
  const plotH = Math.round(h * 0.58);
  const pointsFor = spectrum => spectrum.map((value, index) => [
    plotX + index * plotW / Math.max(1, spectrum.length - 1),
    plotH - 9 - value * (plotH - 34),
  ]);
  const points = pointsFor(normalized);
  ctx.beginPath(); ctx.moveTo(points[0][0], points[0][1]);
  for (let i = 1; i < points.length; i++) ctx.lineTo(points[i][0], points[i][1]);
  // MMSSTV's default FQC graph is a single live trace. A second peak-hold
  // envelope made this view look like two superimposed spectrum layers.
  ctx.lineWidth = 1.8; ctx.strokeStyle = '#063cff';
  ctx.shadowColor = '#063cff'; ctx.shadowBlur = 0.8; ctx.stroke(); ctx.shadowBlur = 0;
}

function updateAudioState(state) {
  const wasRXActive = !!audioState.rxActive;
  audioState = { ...audioState, ...(state || {}) };
  if (audioState.rxActive && !wasRXActive) {
    captureTelemetrySeen = false;
    resetWaterfall();
    drawIdleSpectrum();
  }
  if (!audioState.rxActive) captureTelemetrySeen = false;
  const connected = !!audioState.connected;
  const label = audioState.rxActive ? t('rxAutoRunning', { rate: (audioState.sampleRate || 48000) / 1000 }) : (connected ? t('audioReady') : t('audioInitializing'));
  $('deviceStateLabel').textContent = label;
  document.querySelector('.device-state i').style.background = audioState.rxActive ? '#5b9f77' : '#c39a55';
  $('rate').disabled = false;
  ['audioInput','audioOutput','pttMode','rigctlAddress','serialPort','pttActiveHigh','pttLead','pttTail'].forEach(id => { if ($(id)) $(id).disabled = false; });
  const txButton = $('txToggle');
  txButton.disabled = !connected || toneActive || (!audioState.txActive && !activeTXMode()?.ready);
  txButton.textContent = audioState.txActive && !toneActive ? t('txStop') : t('txStart');
  txButton.classList.toggle('tx-running', !!audioState.txActive && !toneActive);
  const toneButton = $('toneToggle');
  toneButton.disabled = !connected || (audioState.txActive && !toneActive);
  toneButton.textContent = toneActive ? t('toneStop') : t('toneStart');
  $('audioSettingsStatus').textContent = connected ? `${t('audioReadyStatus')}，${audioState.rxActive ? t('rxContinuous') : t('rxRestoring')}${audioState.txActive ? `，${t('txRunningLabel')}` : ''}` : t('audioPendingStatus');
  $('spectrumState').textContent = !audioState.rxActive ? 'INPUT IDLE' : (captureTelemetrySeen ? 'INPUT ACTIVE · SHARED' : 'WAITING FOR INPUT');
  $('spectrumOverlay').textContent = !audioState.rxActive ? t('audioInputIdle') : (captureTelemetrySeen ? '' : t('rxWaitingSamples'));
  $('spectrumOverlay').style.display = audioState.rxActive && captureTelemetrySeen ? 'none' : 'grid';
  $('rxHint').textContent = !audioState.rxActive ? (rxEnabled ? t('rxInitializingHint') : t('rxStoppedHint')) : (captureTelemetrySeen ? t('rxLiveCapture') : t('audioWaitingSamples'));
  $('rxStatus').textContent = audioState.rxActive ? (autoSelected ? t('rxAutoReceiving') : t('rxModeReceiving')) : (rxEnabled ? t('rxInitializing') : t('rxStopped'));
  $('txStatus').textContent = audioState.txActive ? (toneActive ? t('txTone') : t('txTransmitting')) : t('txDraftState');
}
function bindRuntimeEvents() {
  if (!window.runtime?.EventsOn) return;
  window.runtime.EventsOn('audio:state', updateAudioState);
  window.runtime.EventsOn('ptt:test', data => {
    if (data?.active) renderPTTTestState(true, Date.now() + 15000);
    else renderPTTTestState(false, 0, data?.error || '');
  });
  window.runtime.EventsOn('audio:warning', (message) => {
    const warning = String(message || '音频设备提示');
    $('rxHint').textContent = warning;
    setStatus(warning);
  });
  window.runtime.EventsOn('audio:telemetry', (data) => {
    captureTelemetrySeen = true;
    const peakHz = Number(data?.peakHz || 0);
    const crest = Number(data?.crest || 0);
    $('spectrumState').textContent = t('spectrumPeak', { peak: peakHz.toFixed(0), crest: crest.toFixed(1) });
    const dbfs = Number(data?.dbfs ?? -120);
    currentInputDBFS = dbfs;
    const fill = Math.max(0, Math.min(100, (dbfs + 60) / 60 * 100));
    $('inputLevel').style.width = `${fill}%`;
    $('inputLevelText').textContent = `${dbfs.toFixed(1)} dBFS`;
    const samplesReceived = Number(data?.samplesReceived) || 0;
    $('rxHint').textContent = data?.droppedBlocks ? t('droppedAudioBlocks', { count: data.droppedBlocks }) : (dbfs < -90 ? t('inputNearSilence', { count: samplesReceived.toLocaleString() }) : t('samplesCount', { count: samplesReceived.toLocaleString() }));
    renderLiveSpectrum(data?.spectrum);
    $('spectrumOverlay').style.display = 'none';
  });
  window.runtime.EventsOn('rx:progress', (data) => {
    const percent = Math.max(0, Math.min(100, Number(data?.percent) || 0));
    $('rxProgressFill').style.width = `${percent}%`;
    $('rxProgressText').textContent = `${Math.round(percent)}%`;
    if (data?.complete) {
      $('rxProgressLabel').textContent = t('decodeComplete', { mode: data.mode || 'SSTV' });
    } else if (data?.active) {
      const rows = Number(data.rows) || 0;
      const total = Number(data.totalRows) || 0;
      $('rxProgressLabel').textContent = t('receivingRows', { mode: data.mode || 'SSTV', rows, total });
    } else {
      $('rxProgressLabel').textContent = t('waitingSignal');
      $('rxProgressFill').style.width = '0%';
      $('rxProgressText').textContent = '0%';
    }
  });
  window.runtime.EventsOn('rx:callsign', (data) => {
    const callsign = String(data?.callsign || '').trim().toUpperCase();
    // Reject malformed/empty identities before they can affect the QSO form.
    if (!/^[A-Z0-9/\-]{1,16}$/.test(callsign)) return;
    $('rxCallsign').textContent = callsign;
    $('rxCallsign').title = t('callsignSource', { source: data?.source || 'FSKID' });

    // Auto-fill HisCall when it is empty, or refresh a value that was itself
    // previously filled by FSKID. A manually edited QSO callsign is preserved.
    const hisCall = $('hisCall');
    let filledHisCall = false;
    if (hisCall && (!hisCall.value.trim() || hisCall.dataset.fskidAuto === 'true')) {
      if (hisCall.value.trim().toUpperCase() !== callsign) {
        hisCall.value = callsign;
        hisCall.dataset.fskidAuto = 'true';
        hisCall.dispatchEvent(new Event('input', { bubbles: true }));
      }
      filledHisCall = true;
    }
    setStatus(filledHisCall
      ? t('callsignFilled', { source: data?.source || 'FSKID', callsign })
      : t('callsignPreserved', { source: data?.source || 'FSKID', callsign }));
  });
  window.runtime.EventsOn('rx:preview', (data) => {
    const frame = $('rxImageFrame');
    let img = frame.querySelector('#rxLiveImage');
    if (!img) {
      frame.innerHTML = '';
      img = document.createElement('img');
      img.id = 'rxLiveImage';
      img.alt = t('liveImageAlt', { mode: data.mode || 'SSTV' });
      frame.appendChild(img);
    }
    img.src = data.image;
    img.alt = `${data.mode || 'SSTV'} 实时解码图像`;
    $('rxDetectedMode').textContent = data.mode || '---';
    $('rxResolution').textContent = `${data.width} × ${data.height}`;
    $('rxStatus').textContent = t('rxLiveDecoding');
    $('rxProgressLabel').textContent = t('liveRows', { mode: data.mode || 'SSTV', rows: data.rows, total: data.totalRows });
  });
  window.runtime.EventsOn('rx:frame', async (data) => {
    latestRXImageData = data.image || latestRXImageData;
    templateImageCache.clear();
    renderCanvas();
    const frame = $('rxImageFrame'); frame.innerHTML = '';
    const img = document.createElement('img'); img.src = data.image; img.alt = t('receivingImageAlt', { mode: data.mode }); frame.appendChild(img);
    $('rxDetectedMode').textContent = data.mode; $('rxResolution').textContent = `${data.width} × ${data.height}`; $('rxTimestamp').textContent = data.timestamp;
    $('rxStatus').textContent = t('imageReceivedStatus');
    $('rxProgressFill').style.width = '100%'; $('rxProgressText').textContent = '100%'; $('rxProgressLabel').textContent = t('imageReceivedStatus');
    setStatus(`${data.mode} ${t('imageReceivedStatus')}${data.albumSaved ? t('imageAddedAlbum') : ''}`);
    await refreshAlbum();
  });
  window.runtime.EventsOn('rx:error', message => setStatus(`RX 解码提示：${message}`, true));
  window.runtime.EventsOn('tx:state', data => {
    $('txStatus').textContent = data.state === 'playing' ? t('txStatePlaying') : data.state === 'encoding' ? t('txStateEncoding') : data.state === 'completed' ? t('txStateCompleted') : data.state === 'stopped' ? t('txStateStopped') : data.state === 'error' ? t('txStateError') : t('txStateDraft');
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
    $('txProgressLabel').textContent = data?.message || (data?.stage === 'playing' ? t('txPlaying') : t('txEncoding'));
  });
  window.runtime.EventsOn('tx:complete', () => refreshAlbum());
}

async function startTX() {
  const mode = activeTXMode();
  if (!mode || !mode.ready) return setStatus(t('txEncoderUnavailable'), true);
  if (!audioState.connected) return setStatus(t('txOutputNotReady'), true);
  try {
    // Use the current QSO editor identity for the FSKID payload, so the
    // transmitted station ID always matches the visible MYCALL template token.
    const savedStation = await app().SaveStationSettings({ myCall: $('myCall').value, locator: $('locator').value });
    stationSettings = { myCall: savedStation?.myCall || 'N0CALL', locator: savedStation?.locator || 'JJ00' };
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
    fill(input, lists.inputs, t('systemDefaultInput')); fill(output, lists.outputs, t('systemDefaultOutput'));
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
    empty.textContent = ports.length ? t('chooseSerial') : t('serialNotFound');
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
    empty.textContent = t('serialEnumFailed');
    select.appendChild(empty);
  }
}
function updatePTTFields() {
  const mode = $('pttMode').value;
  const show = (el, visible) => { el.style.display = visible ? '' : 'none'; };
  show($('rigctlField'), mode === 'hamlib');
  show($('serialField'), mode === 'rts' || mode === 'dtr');
  show($('activeHighField'), mode === 'rts' || mode === 'dtr');
  show($('pttTestRow'), mode !== 'vox');
  if ($('pttTestButton')) $('pttTestButton').disabled = mode === 'vox';
  if (mode === 'vox' && pttTestActive) app().StopPTTTest().catch(() => {});
}
function pttTestConfigFromForm() {
  return {
    sampleRate: Number($('rate').value) || 48000,
    pttMode: $('pttMode').value,
    rigctlAddress: $('rigctlAddress').value.trim(),
    serialPort: $('serialPort').value.trim(),
    activeHigh: $('pttActiveHigh').checked,
    pttLeadMs: Number($('pttLead').value) || 0,
    pttTailMs: Number($('pttTail').value) || 0,
  };
}
function renderPTTTestState(active, deadline = 0, error = '') {
  pttTestActive = !!active;
  pttTestDeadline = deadline;
  const button = $('pttTestButton');
  const status = $('pttTestStatus');
  if (pttTestTimer) clearInterval(pttTestTimer);
  pttTestTimer = null;
  if (!button || !status) return;
  button.disabled = $('pttMode').value === 'vox';
  if (!active) {
    button.textContent = t('pttTest');
    status.textContent = error ? t('pttTestFailed', { error }) : '';
    return;
  }
  button.textContent = t('pttTestStop');
  const update = () => {
    const seconds = Math.max(0, Math.ceil((pttTestDeadline - Date.now()) / 1000));
    status.textContent = t('pttTestRunning', { seconds });
    if (seconds <= 0 && pttTestTimer) {
      clearInterval(pttTestTimer);
      pttTestTimer = null;
    }
  };
  update();
  pttTestTimer = setInterval(update, 200);
}
async function togglePTTTest() {
  const button = $('pttTestButton');
  const status = $('pttTestStatus');
  if (pttTestActive) {
    button.disabled = true;
    try {
      await app().StopPTTTest();
      renderPTTTestState(false);
    } catch (e) {
      renderPTTTestState(false, 0, String(e));
    } finally { button.disabled = false; }
    return;
  }
  if ($('pttMode').value === 'vox') {
    status.textContent = t('pttVoxUnavailable');
    return;
  }
  button.disabled = true;
  status.textContent = t('pttTestStarting');
  try {
    await app().StartPTTTest(pttTestConfigFromForm());
    renderPTTTestState(true, Date.now() + 15000);
  } catch (e) {
    renderPTTTestState(false, 0, String(e));
  } finally { button.disabled = false; }
}

async function saveAllSettings() {
  const button = $('saveAllSettings');
  const status = $('settingsStatus');
  const closeButton = $('closeSettings');
  button.disabled = true;
  closeButton.disabled = true;
  if ($('pttTestButton')) $('pttTestButton').disabled = true;
  status.textContent = t('saving');
  status.classList.remove('error');
  try {
    if (pttTestActive) {
      await app().StopPTTTest();
      renderPTTTestState(false);
    }
    const saved = await app().SaveStationSettings({ myCall: $('settingsMyCall').value, locator: $('settingsLocator').value });
    stationSettings = { myCall: saved?.myCall || 'N0CALL', locator: saved?.locator || 'JJ00' };
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
    status.textContent = t('settingsSaved');
    setStatus(t('settingsSaved'));
    $('settingsDialog').close();
  } catch (e) {
    status.textContent = t('settingsFailed', { error: String(e) });
    status.classList.add('error');
    setStatus(`保存设置失败：${String(e)}`, true);
  } finally {
    button.disabled = false;
    closeButton.disabled = false;
    if ($('pttTestButton')) $('pttTestButton').disabled = $('pttMode').value === 'vox' || pttTestActive;
  }
}


async function encodeWAV() {
  const mode = activeTXMode(); if (!mode || !mode.ready) return setStatus('当前 TX 模式尚无可用编码器', true);
  const button = $('encode'); button.disabled = true; setStatus('正在合成画面并编码 WAV…');
  try {
    const dataURL = exportCanvasDataURL();
    const background = components.find(isBackgroundComponent); const name = background?.imageName || 'SATSSTV-Go';
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
    stationSettings = { myCall: saved?.myCall || 'N0CALL', locator: saved?.locator || 'JJ00' };
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
  $('closeSettings').addEventListener('click', async () => {
    if (pttTestActive) { try { await app().StopPTTTest(); } catch {} }
    dialog.close();
  });
  $('saveAllSettings').addEventListener('click', saveAllSettings);
  $('refreshAudioDevices').addEventListener('click', refreshAudioDevices);
  $('refreshSerialPorts').addEventListener('click', refreshSerialPorts);
  $('pttMode').addEventListener('change', updatePTTFields);
  $('pttTestButton').addEventListener('click', togglePTTTest);
  // Do not close on backdrop clicks or Escape: this prevents losing unsaved edits.
  dialog.addEventListener('cancel', e => e.preventDefault());
  $('languageSelect').addEventListener('change', e => window.SATSSTVI18n?.setLanguage(e.target.value));
  window.addEventListener('satsstv-language-change', () => {
    updatePTTFields();
    updateAudioState(audioState);
    updateModeInfo();
    if (pttTestActive) renderPTTTestState(true, pttTestDeadline);
    renderModeSlots(); renderTXModeSlots(); renderTemplates(); renderComponentList();
  });
  $('closePreview').addEventListener('click', () => $('albumPreviewDialog').close());
}
async function initializeAudioRX() {
  try {
    $('rxStatus').textContent = t('rxInitializing'); $('spectrumOverlay').textContent = t('audioInputOpening');
    if (rxEnabled) await app().InitializeAudio(currentRXModeID());
    else await app().SetRXEnabled(false, '');
    let state = await app().GetAudioState();
    // Wails/audio backends can report the pre-start state if the native device
    // emits its first state event while the window is still initializing.
    // Retry initialization once when RX was requested but is not actually live.
    if (rxEnabled && !state?.rxActive) {
      await new Promise(resolve => setTimeout(resolve, 180));
      state = await app().GetAudioState();
      if (!state?.rxActive) {
        await app().InitializeAudio(currentRXModeID());
        state = await app().GetAudioState();
      }
    }
    updateAudioState(state);
    renderModeSlots();
    setStatus(!rxEnabled ? '音频设备已就绪，RX 当前已停止' : (state?.rxActive ? (autoSelected ? 'RX 已启动，正在自动识别 SSTV 模式' : `RX 已启动，指定模式：${activeRXMode()?.name || ''}`) : '音频设备已连接，RX 正在等待启动'));
  } catch (e) {
    $('rxStatus').textContent = t('rxInitFailedShort'); $('rxHint').textContent = String(e); $('spectrumOverlay').textContent = t('rxInitFailed', { error: String(e) });
    setStatus(`自动启动 RX 失败：${String(e)}`, true);
  }
}


// UI events. RX starts automatically on application launch and remains active.
$('clearBackground').addEventListener('click', clearBackground);
$('addImage').addEventListener('click', openImageLibrary);
$('closeImageLibrary').addEventListener('click', () => $('imageLibraryDialog').close());
$('importLibraryImage').addEventListener('click', importImageToLibrary);
$('encode').addEventListener('click', encodeWAV);
$('txToggle').addEventListener('click', toggleTX);
$('toneToggle').addEventListener('click', toggleTone);
$('addText').addEventListener('click', addTextComponent);
$('addLastRX').addEventListener('click', addLatestRXImage);
$('deleteText').addEventListener('click', deleteSelectedComponent);
$('saveTemplate').addEventListener('click', saveCurrentTemplate);
$('componentText').addEventListener('input', updateSelectedComponent);
['fontSize', 'fontColor', 'posX', 'posY', 'fontBold', 'imageWidth', 'outlineEnabled', 'outlineWidth', 'outlineColor'].forEach(id => $(id).addEventListener('input', updateSelectedComponent));
VALUE_IDS.forEach(id => $(id).addEventListener('input', (event) => {
  if (id === 'myCall' || id === 'locator') syncStationSettingsForm();
  if (id === 'hisCall' && event.isTrusted) delete $('hisCall').dataset.fskidAuto;
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
  window.SATSSTVI18n?.applyTranslations();
  setupSettings();
  drawIdleSpectrum();
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
