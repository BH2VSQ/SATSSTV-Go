'use strict';

(() => {
  const STORAGE_KEY = 'satsstv.language.v1';
  const dictionaries = {
    'zh-CN': {
      appTitle: 'SATSSTV-Go', audioRate: '音频采样率', toneStop: '停止单音', txStop: '停止发射', rxLevel: '接收电平见频谱左侧', ready: '就绪', librarySubtitle: '图片保存在本机用户配置目录，模板会嵌入自己的副本', navWorkbench: '工作台', navAlbum: '相册', audioDisconnected: '音频设备未连接',
      rxTitle: '接收监视', rxIdle: '待机', rxAuto: 'Auto · 自动接收', rxAutoRunning: 'RX 自动运行 · {rate} kHz', audioReady: '音频已就绪', audioInitializing: '音频待初始化', rxAutoReceiving: 'Auto 自动接收中', rxModeReceiving: '指定模式接收中', rxInitializing: '初始化中', rxStopped: 'RX 已停止', txTransmitting: '正在发射', txTone: '1500 Hz 单音发射', txDraftState: '草稿', rxSlotHint: '右键更换槽位', rxWaitMode: '载入模式中…',
      rxWaitImage: '等待接收图像', rxWaitImageHint: '解码完成的图像会显示在这里', rxMode: '模式', rxSize: '尺寸', rxCallsign: '呼号', rxWaitFSKID: '等待 FSKID', rxTime: '接收时间',
      rxWaitSignal: '等待 SSTV 信号', saveCapture: '保存采集', rxSpectrumHint: '实时显示 900 至 2800 Hz SSTV 音频频谱与瀑布图', rxAutoStart: 'RX 将随工作台自动启动',
      txTitle: '发送编辑器', txDraft: '草稿', encodeWav: '生成 WAV', toneStart: '1500 Hz 单音', txStart: '开始发射', txWait: '发射就绪',
      canvasDragHint: '拖动文字或图像可调整位置', clear: '清除', defaultBackground: '默认背景，尚未载入图片', txReady: '编辑画面就绪',
      templateComponents: '模板组件', addText: '＋ 文字', addImage: '＋ 图像', addLastRX: '上次 RX 图像组件', componentProps: '组件属性', delete: '删除', textContent: '文字内容',
      fontSize: '字号', color: '颜色', horizontalPosition: '水平位置', verticalPosition: '垂直位置', bold: '粗体', showOutline: '显示描边', outlineWidth: '描边宽度', outlineColor: '描边颜色',
      imageScale: '图像缩放（保持原比例）', editorHelp: '支持文字、图像图层和可拖动缩放的持久底图。图像按原图比例缩放；字号上限随 TX 模式分辨率变化。',
      txTemplates: '发送模板', saveSlot: '保存当前槽位', presetHint: '前三个预设可修改，第四个为空白模板',
      myCallPlaceholder: '本台呼号', hisCallPlaceholder: '对方呼号', qthPlaceholder: 'QTH', locatorPlaceholder: '网格定位', reportPlaceholder: 'RST / 报告', messagePlaceholder: '自定义消息',
      albumTitle: '图像相册', albumSubtitle: '历史接收与发送图像，自动记录时间戳', albumAll: '全部图像', albumRX: '仅接收 RX', albumTX: '仅发送 TX', albumImportTX: '导入 TX 图像',
      albumEmpty: '相册暂无图像。完成接收或发射后会自动归档。', albumFootnote: '点击缩略图可查看大图；右键可导出带时间戳的 PNG 或删除记录。图像保存在本机用户配置目录。',
      settingsTitle: '设备设置', language: '界面语言', languageChinese: '简体中文', languageEnglish: 'English', languageJapanese: '日本語',
      audioDevices: '音频设备', rxInput: 'RX 输入设备', txOutput: 'TX 输出设备', systemDefaultInput: '系统默认输入设备', systemDefaultOutput: '系统默认输出设备', refreshDevices: '刷新设备',
      audioSharedHint: '输入与输出均采用共享模式，不独占系统音频设备。保存设置时会短暂重建音频流并自动恢复 RX。', audioAutoHint: '启动工作台时自动接收。',
      pttTitle: 'PTT 发射控制', pttMode: '控制方式', pttVOX: 'VOX（电台自动发射）', pttRTS: '串口 RTS', pttDTR: '串口 DTR', pttHamlib: 'Hamlib Rigctld', rigctlAddress: 'Rigctl 地址', hostPort: '主机名:端口', serialPort: '串口名称', chooseSerial: '选择串口', refreshSerial: '刷新',
      pttLead: 'PTT 前置延时 (ms)', pttTail: 'PTT 释放延时 (ms)', pttActiveHigh: 'RTS/DTR 高电平发射', pttHint: '默认使用 VOX，由电台根据 SSTV 音频自动切换发射。也可选择串口 RTS/DTR 或 Hamlib Rigctld 控制电台 PTT。',
      pttTest: '测试 PTT（最长 15 秒）', pttTestStop: '停止 PTT 测试', pttTestStarting: '正在触发 PTT…', pttTestRunning: 'PTT 测试中 · {seconds}s 后自动停止', pttTestStarted: 'PTT 已触发，15 秒后自动释放', pttTestStopped: 'PTT 测试已停止', pttTestFailed: 'PTT 测试失败：{error}', pttVoxUnavailable: 'VOX 模式不需要软件 PTT 测试',
      stationIdentity: '本站身份信息', myCallLabel: '本台呼号', locatorLabel: '本站网格', callsignExample: '例如 N0CALL', locatorExample: '例如 JJ00', stationHint: '保存后会自动填入工作台。对方呼号、QTH、报告和消息仍可按每次 QSO 单独填写。',
      settingsUnsaved: '尚未保存', saveSettings: '保存设置', saving: '正在保存…', settingsSaved: '设置已保存并应用', settingsFailed: '保存失败：{error}',
      close: '关闭', refresh: '刷新', imageLibrary: '模板图片图库', importImage: '导入图片', readingLibrary: '正在读取图库…',
      save: '保存',
      blankTemplate: '空白模板', containsImageComponent: '包含图像组件', empty: '空', rxStoppedLabel: 'RX 已停止', rxAutoAllModes: 'Auto · 自动识别所有支持模式', rxAutoReceive: 'Auto · 自动接收', fixedModeReceive: '指定模式接收',
      rxSlotStopTitle: '槽位 {slot}：再次点击停止 RX，右键更换模式', rxSlotChangeTitle: '槽位 {slot}：{mode}，右键更换', txSlotTitle: '槽位 {slot}：{mode}，右键更换',
      modeSlotRXTitle: '设置 RX 模式槽位 {slot}', modeSlotTXTitle: '设置 TX 模式槽位 {slot}', modeNotReady: '尚未完成编解码',
      fontLimitTitle: '当前 TX 模式字号上限：{limit}px', lastRXImage: '上一次 RX 图像', waitingRXImage: '等待接收图像',
      backgroundPrefix: '底图：', defaultBackgroundColor: '纯色背景', backgroundScale: '底图缩放', imageScaleLabel: '图像缩放（保持原比例）',
      horizontalOffset: '水平偏移', horizontalPositionLabel: '水平位置', verticalOffset: '垂直偏移', verticalPositionLabel: '垂直位置',
      customText: '自定义文字', templateImage: '模板图像', templateImageLoadFailed: '无法载入模板图像',
      libraryLoading: '正在读取图库…', libraryEmpty: '图库为空。点击右上角“导入图片”添加本机图片。', setAsBackground: '设为底图', addLayer: '添加图层', libraryReadFailed: '图库读取失败：{error}',
      imageImported: '已导入图库：{name}', imageImportFailed: '导入图库失败：{error}', backgroundCleared: '已清除共享底图',
      latestRXAdded: '已添加上一次 RX 图像组件', latestRXPlaceholderAdded: '已添加上一次 RX 图像占位组件，收到首张图像后会自动填入',
      backgroundSaved: '共享底图已保存', backgroundKept: '保留共享底图', templateLoaded: '已载入模板槽位 {slot}：{name}{background}', templateSaved: '已保存到模板槽位 {slot}：{name}',
      rxInitializingHint: 'RX 音频流正在初始化…', rxStoppedHint: 'RX 已停止', rxLiveCapture: '实时音频采集中', rxWaitingSamples: 'RX 已启动，正在等待音频设备输入采样…',
      rxLiveDecoding: '实时图像解码中', rxImageCompleted: '图像接收完成', txEncoding: '正在编码 SSTV', txPlaying: '正在发射 SSTV',
      audioInputIdle: 'RX 尚未启动', audioWaitingSamples: 'RX 已启动，等待输入采样', audioReadyStatus: '共享模式音频已就绪', audioPendingStatus: '音频设备待初始化，工作台会自动启动 RX',
      imageDragScaleHint: '可拖动 / 缩放', rxContinuous: 'RX 持续接收中', rxRestoring: 'RX 正在恢复', txRunningLabel: 'TX 运行中',
      droppedAudioBlocks: '采集队列丢弃 {count} 个音频块', inputNearSilence: '已收到 {count} 个采样，当前输入接近静音', samplesCount: '实时音频采集中 · {count} 个采样',
      decodeComplete: '{mode} 解码完成', receivingRows: '正在接收 {mode} · {rows}/{total} 行', waitingSignal: '等待 SSTV 信号',
      liveImageAlt: '{mode} 实时解码图像', receivingImageAlt: '{mode} 接收图像', liveRows: '实时显示 {mode} · {rows}/{total} 行',
      imageReceivedStatus: '图像接收完成', imageAddedAlbum: '，已加入相册', txStatePlaying: '发射中', txStateEncoding: '编码中', txStateCompleted: '完成', txStateStopped: '已停止', txStateError: '错误', txStateDraft: '草稿',
      audioDeviceCount: '发现 {inputs} 个输入设备和 {outputs} 个输出设备', audioDeviceEnumFailed: '设备枚举失败：{error}', serialDisconnected: '（已断开）', serialNotFound: '未发现串口', serialEnumFailed: '串口枚举失败', audioInputOpening: '正在打开共享模式音频输入设备…', rxInitFailedShort: '音频初始化失败',
      rxInitFailed: '音频初始化失败：{error}。请检查设备设置。', rxAutoStartFailed: '自动启动 RX 失败：{error}', txOutputNotReady: 'TX 音频输出尚未初始化，请在设备设置中选择输出设备并应用', txEncoderUnavailable: '当前 TX 模式的编码器不可用',
      spectrumPeak: '峰值 {peak} Hz · 峰度 {crest}', callsignSource: '来源：{source}', callsignFilled: '已通过 {source} 识别到呼号 {callsign}，已填入对方呼号', callsignPreserved: '已通过 {source} 识别到呼号 {callsign}；保留了已手动填写的对方呼号',
    },
    'en': {
      appTitle: 'SATSSTV-Go', audioRate: 'Audio Sample Rate', toneStop: 'Stop Tone', txStop: 'Stop TX', rxLevel: 'RX level is shown on the left of the spectrum', ready: 'Ready', librarySubtitle: 'Images are stored in your user data directory; templates embed their own copies.', navWorkbench: 'Workbench', navAlbum: 'Album', audioDisconnected: 'Audio device disconnected',
      rxTitle: 'RX Monitor', rxIdle: 'Idle', rxAuto: 'Auto · Receive', rxAutoRunning: 'RX active · {rate} kHz', audioReady: 'Audio ready', audioInitializing: 'Audio initializing', rxAutoReceiving: 'Auto receiving', rxModeReceiving: 'Fixed-mode receiving', rxInitializing: 'Initializing', rxStopped: 'RX stopped', txTransmitting: 'Transmitting', txTone: '1500 Hz tone transmitting', txDraftState: 'Draft', rxSlotHint: 'Right-click to change slot', rxWaitMode: 'Loading modes…',
      rxWaitImage: 'Waiting for image', rxWaitImageHint: 'Decoded images will appear here', rxMode: 'Mode', rxSize: 'Size', rxCallsign: 'Callsign', rxWaitFSKID: 'Waiting for FSKID', rxTime: 'Received',
      rxWaitSignal: 'Waiting for SSTV signal', saveCapture: 'Save Capture', rxSpectrumHint: 'Live SSTV audio spectrum and waterfall, 900–2800 Hz', rxAutoStart: 'RX starts automatically with the workbench',
      txTitle: 'TX Editor', txDraft: 'Draft', encodeWav: 'Create WAV', toneStart: '1500 Hz Tone', txStart: 'Start TX', txWait: 'Ready to transmit',
      canvasDragHint: 'Drag text or images to reposition', clear: 'Clear', defaultBackground: 'Default background; no image loaded', txReady: 'Canvas ready',
      templateComponents: 'Template Components', addText: '＋ Text', addImage: '＋ Image', addLastRX: 'Last RX Image', componentProps: 'Component Properties', delete: 'Delete', textContent: 'Text',
      fontSize: 'Font Size', color: 'Color', horizontalPosition: 'Horizontal Position', verticalPosition: 'Vertical Position', bold: 'Bold', showOutline: 'Show Outline', outlineWidth: 'Outline Width', outlineColor: 'Outline Color',
      imageScale: 'Image Scale (Keep Aspect Ratio)', editorHelp: 'Text, image layers, and a persistent draggable/scalable background are supported. Image proportions are preserved; font size limits follow TX mode resolution.',
      txTemplates: 'TX Templates', saveSlot: 'Save Slot', presetHint: 'The first three presets are editable; the fourth is blank.',
      myCallPlaceholder: 'My Callsign', hisCallPlaceholder: 'His Callsign', qthPlaceholder: 'QTH', locatorPlaceholder: 'Grid Locator', reportPlaceholder: 'RST / Report', messagePlaceholder: 'Custom Message',
      albumTitle: 'Image Album', albumSubtitle: 'Received and transmitted images with timestamps', albumAll: 'All Images', albumRX: 'RX Only', albumTX: 'TX Only', albumImportTX: 'Import TX Image',
      albumEmpty: 'No images yet. Received and transmitted images are archived automatically.', albumFootnote: 'Click a thumbnail to preview. Right-click to export a timestamped PNG or delete an entry. Images are stored in your user data directory.',
      settingsTitle: 'Device Settings', language: 'Language', languageChinese: '简体中文', languageEnglish: 'English', languageJapanese: '日本語',
      audioDevices: 'Audio Devices', rxInput: 'RX Input Device', txOutput: 'TX Output Device', systemDefaultInput: 'System Default Input', systemDefaultOutput: 'System Default Output', refreshDevices: 'Refresh Devices',
      audioSharedHint: 'Input and output use shared mode. Saving briefly rebuilds the audio streams and resumes RX automatically.', audioAutoHint: 'RX starts automatically when the workbench opens.',
      pttTitle: 'PTT Control', pttMode: 'Control Method', pttVOX: 'VOX (Radio VOX)', pttRTS: 'Serial RTS', pttDTR: 'Serial DTR', pttHamlib: 'Hamlib Rigctld', rigctlAddress: 'Rigctl Address', hostPort: 'Host:Port', serialPort: 'Serial Port', chooseSerial: 'Select Serial Port', refreshSerial: 'Refresh',
      pttLead: 'PTT Lead-in (ms)', pttTail: 'PTT Release Tail (ms)', pttActiveHigh: 'RTS/DTR active-high', pttHint: 'VOX is the default and lets the radio key from SSTV audio. Serial RTS/DTR and Hamlib Rigctld are also supported.',
      pttTest: 'Test PTT (15 s max)', pttTestStop: 'Stop PTT Test', pttTestStarting: 'Keying PTT…', pttTestRunning: 'PTT test active · auto-release in {seconds}s', pttTestStarted: 'PTT keyed; it will release automatically in 15 seconds', pttTestStopped: 'PTT test stopped', pttTestFailed: 'PTT test failed: {error}', pttVoxUnavailable: 'Software PTT testing is not available in VOX mode',
      stationIdentity: 'Station Identity', myCallLabel: 'My Callsign', locatorLabel: 'My Locator', callsignExample: 'e.g. N0CALL', locatorExample: 'e.g. JJ00', stationHint: 'Saved values are filled into the workbench automatically. HisCall, QTH, report, and message remain per-QSO fields.',
      settingsUnsaved: 'Not saved', saveSettings: 'Save Settings', saving: 'Saving…', settingsSaved: 'Settings saved and applied', settingsFailed: 'Save failed: {error}',
      close: 'Close', refresh: 'Refresh', imageLibrary: 'Template Image Library', importImage: 'Import Image', readingLibrary: 'Loading image library…',
      save: 'Save',
      blankTemplate: 'Blank Template', containsImageComponent: 'Contains image layer', empty: 'Empty', rxStoppedLabel: 'RX stopped', rxAutoAllModes: 'Auto · All supported modes', rxAutoReceive: 'Auto · Receiving', fixedModeReceive: 'Fixed-mode receive',
      rxSlotStopTitle: 'Slot {slot}: click again to stop RX; right-click to change mode', rxSlotChangeTitle: 'Slot {slot}: {mode}; right-click to change', txSlotTitle: 'Slot {slot}: {mode}; right-click to change',
      modeSlotRXTitle: 'Configure RX mode slot {slot}', modeSlotTXTitle: 'Configure TX mode slot {slot}', modeNotReady: 'Codec not implemented yet',
      fontLimitTitle: 'Maximum font size for the current TX mode: {limit}px', lastRXImage: 'Last RX Image', waitingRXImage: 'Waiting for image',
      backgroundPrefix: 'Background: ', defaultBackgroundColor: 'Solid color background', backgroundScale: 'Background Scale', imageScaleLabel: 'Image Scale (Keep Aspect Ratio)',
      horizontalOffset: 'Horizontal Offset', horizontalPositionLabel: 'Horizontal Position', verticalOffset: 'Vertical Offset', verticalPositionLabel: 'Vertical Position',
      customText: 'Custom Text', templateImage: 'Template Image', templateImageLoadFailed: 'Unable to load template image',
      libraryLoading: 'Loading image library…', libraryEmpty: 'The image library is empty. Import an image using the button above.', setAsBackground: 'Set as Background', addLayer: 'Add Layer', libraryReadFailed: 'Failed to read image library: {error}',
      imageImported: 'Imported image: {name}', imageImportFailed: 'Image import failed: {error}', backgroundCleared: 'Shared background cleared',
      latestRXAdded: 'Added Last RX Image component', latestRXPlaceholderAdded: 'Added Last RX Image placeholder; the first received image will be inserted automatically',
      backgroundSaved: 'Shared background saved', backgroundKept: 'shared background kept', templateLoaded: 'Loaded template slot {slot}: {name}{background}', templateSaved: 'Saved template slot {slot}: {name}',
      rxInitializingHint: 'Initializing RX audio stream…', rxStoppedHint: 'RX stopped', rxLiveCapture: 'Capturing audio in real time', rxWaitingSamples: 'RX started; waiting for audio samples…',
      rxLiveDecoding: 'Decoding image in real time', rxImageCompleted: 'Image received', txEncoding: 'Encoding SSTV', txPlaying: 'Transmitting SSTV',
      audioInputIdle: 'RX not started', audioWaitingSamples: 'RX started; waiting for input samples', audioReadyStatus: 'Shared-mode audio ready', audioPendingStatus: 'Audio device pending; RX starts automatically',
      imageDragScaleHint: 'Drag / scale', rxContinuous: 'RX receiving', rxRestoring: 'RX restarting', txRunningLabel: 'TX active',
      droppedAudioBlocks: 'Capture queue dropped {count} audio blocks', inputNearSilence: 'Received {count} samples; input is near silence', samplesCount: 'Capturing audio · {count} samples',
      decodeComplete: '{mode} decoding complete', receivingRows: 'Receiving {mode} · {rows}/{total} rows', waitingSignal: 'Waiting for SSTV signal',
      liveImageAlt: '{mode} live decoded image', receivingImageAlt: '{mode} received image', liveRows: 'Live {mode} · {rows}/{total} rows',
      imageReceivedStatus: 'Image received', imageAddedAlbum: ' · added to album', txStatePlaying: 'Transmitting', txStateEncoding: 'Encoding', txStateCompleted: 'Completed', txStateStopped: 'Stopped', txStateError: 'Error', txStateDraft: 'Draft',
      audioDeviceCount: 'Found {inputs} input and {outputs} output devices', audioDeviceEnumFailed: 'Device enumeration failed: {error}', serialDisconnected: ' (disconnected)', serialNotFound: 'No serial port found', serialEnumFailed: 'Serial port enumeration failed', audioInputOpening: 'Opening shared-mode audio input…', rxInitFailedShort: 'Audio initialization failed',
      rxInitFailed: 'Audio initialization failed: {error}. Check device settings.', rxAutoStartFailed: 'Failed to start RX automatically: {error}', txOutputNotReady: 'TX audio output is not initialized. Select an output device in Settings and apply it.', txEncoderUnavailable: 'The encoder for the current TX mode is unavailable',
      spectrumPeak: 'Peak {peak} Hz · Crest {crest}', callsignSource: 'Source: {source}', callsignFilled: 'Callsign {callsign} received via {source} and filled into HisCall', callsignPreserved: 'Callsign {callsign} received via {source}; keeping the manually entered HisCall',
    },
    'ja': {
      appTitle: 'SATSSTV-Go', audioRate: 'オーディオサンプルレート', toneStop: 'トーン停止', txStop: '送信停止', rxLevel: 'RX レベルはスペクトラム左側に表示', ready: '準備完了', librarySubtitle: '画像はユーザーデータに保存され、テンプレートには個別のコピーが埋め込まれます。', navWorkbench: 'ワークベンチ', navAlbum: 'アルバム', audioDisconnected: 'オーディオデバイス未接続',
      rxTitle: '受信モニター', rxIdle: '待機中', rxAuto: 'Auto · 自動受信', rxAutoRunning: 'RX 動作中 · {rate} kHz', audioReady: 'オーディオ準備完了', audioInitializing: 'オーディオ初期化中', rxAutoReceiving: 'Auto 自動受信中', rxModeReceiving: '指定モード受信中', rxInitializing: '初期化中', rxStopped: 'RX 停止中', txTransmitting: '送信中', txTone: '1500 Hz トーン送信中', txDraftState: '下書き', rxSlotHint: '右クリックでスロット変更', rxWaitMode: 'モードを読み込み中…',
      rxWaitImage: '画像の受信待ち', rxWaitImageHint: 'デコードした画像がここに表示されます', rxMode: 'モード', rxSize: 'サイズ', rxCallsign: 'コールサイン', rxWaitFSKID: 'FSKID 待機中', rxTime: '受信時刻',
      rxWaitSignal: 'SSTV 信号待ち', saveCapture: '受信音声を保存', rxSpectrumHint: 'SSTV 音声スペクトラムとウォーターフォール（900～2800 Hz）', rxAutoStart: 'ワークベンチ起動時に RX を自動開始',
      txTitle: '送信エディター', txDraft: '下書き', encodeWav: 'WAV を生成', toneStart: '1500 Hz トーン', txStart: '送信開始', txWait: '送信準備完了',
      canvasDragHint: 'テキストや画像をドラッグして位置を調整', clear: 'クリア', defaultBackground: '既定の背景（画像未読み込み）', txReady: '編集画面の準備完了',
      templateComponents: 'テンプレート部品', addText: '＋ テキスト', addImage: '＋ 画像', addLastRX: '前回の RX 画像', componentProps: '部品プロパティ', delete: '削除', textContent: 'テキスト内容',
      fontSize: '文字サイズ', color: '色', horizontalPosition: '水平位置', verticalPosition: '垂直位置', bold: '太字', showOutline: '縁取りを表示', outlineWidth: '縁取り幅', outlineColor: '縁取り色',
      imageScale: '画像倍率（縦横比を維持）', editorHelp: 'テキスト、画像レイヤー、移動・拡大縮小可能な背景に対応。画像の縦横比を維持し、文字サイズ上限は TX モードの解像度に連動します。',
      txTemplates: '送信テンプレート', saveSlot: '現在のスロットを保存', presetHint: '最初の3つは編集可能、4つ目は空白テンプレートです。',
      myCallPlaceholder: '自局コールサイン', hisCallPlaceholder: '相手局コールサイン', qthPlaceholder: 'QTH', locatorPlaceholder: 'グリッドロケーター', reportPlaceholder: 'RST / レポート', messagePlaceholder: 'カスタムメッセージ',
      albumTitle: '画像アルバム', albumSubtitle: '受信・送信画像をタイムスタンプ付きで保存', albumAll: 'すべて', albumRX: 'RX のみ', albumTX: 'TX のみ', albumImportTX: 'TX 画像をインポート',
      albumEmpty: '画像はありません。受信または送信した画像は自動保存されます。', albumFootnote: 'サムネイルをクリックしてプレビュー。右クリックでタイムスタンプ付き PNG の保存または削除ができます。',
      settingsTitle: 'デバイス設定', language: '表示言語', languageChinese: '简体中文', languageEnglish: 'English', languageJapanese: '日本語',
      audioDevices: 'オーディオデバイス', rxInput: 'RX 入力デバイス', txOutput: 'TX 出力デバイス', systemDefaultInput: 'システム既定の入力', systemDefaultOutput: 'システム既定の出力', refreshDevices: 'デバイスを更新',
      audioSharedHint: '入力と出力は共有モードを使用します。保存時にオーディオストリームを再構築し、RX を自動再開します。', audioAutoHint: 'ワークベンチ起動時に RX を自動受信します。',
      pttTitle: 'PTT 送信制御', pttMode: '制御方式', pttVOX: 'VOX（無線機の VOX）', pttRTS: 'シリアル RTS', pttDTR: 'シリアル DTR', pttHamlib: 'Hamlib Rigctld', rigctlAddress: 'Rigctl アドレス', hostPort: 'ホスト名:ポート', serialPort: 'シリアルポート', chooseSerial: 'ポートを選択', refreshSerial: '更新',
      pttLead: 'PTT 先行時間 (ms)', pttTail: 'PTT 解除遅延 (ms)', pttActiveHigh: 'RTS/DTR アクティブ High', pttHint: '既定は VOX です。シリアル RTS/DTR または Hamlib Rigctld による PTT 制御も使用できます。',
      pttTest: 'PTT テスト（最大15秒）', pttTestStop: 'PTT テスト停止', pttTestStarting: 'PTT を有効化中…', pttTestRunning: 'PTT テスト中 · {seconds}秒後に自動解除', pttTestStarted: 'PTT を有効にしました。15秒後に自動解除します', pttTestStopped: 'PTT テストを停止しました', pttTestFailed: 'PTT テスト失敗：{error}', pttVoxUnavailable: 'VOX モードではソフトウェア PTT テストを使用できません',
      stationIdentity: '局情報', myCallLabel: '自局コールサイン', locatorLabel: '自局ロケーター', callsignExample: '例：N0CALL', locatorExample: '例：JJ00', stationHint: '保存するとワークベンチに自動入力されます。相手局、QTH、レポート、メッセージは QSO ごとに入力できます。',
      settingsUnsaved: '未保存', saveSettings: '設定を保存', saving: '保存中…', settingsSaved: '設定を保存して適用しました', settingsFailed: '保存失敗：{error}',
      close: '閉じる', refresh: '更新', imageLibrary: 'テンプレート画像ライブラリ', importImage: '画像をインポート', readingLibrary: 'ライブラリを読み込み中…',
      save: '保存',
      blankTemplate: '空白テンプレート', containsImageComponent: '画像レイヤーを含む', empty: '空', rxStoppedLabel: 'RX 停止中', rxAutoAllModes: 'Auto · 対応モードを自動認識', rxAutoReceive: 'Auto · 自動受信', fixedModeReceive: '指定モード受信',
      rxSlotStopTitle: 'スロット {slot}：再クリックで RX 停止、右クリックでモード変更', rxSlotChangeTitle: 'スロット {slot}：{mode}、右クリックで変更', txSlotTitle: 'スロット {slot}：{mode}、右クリックで変更',
      modeSlotRXTitle: 'RX モードスロット {slot} を設定', modeSlotTXTitle: 'TX モードスロット {slot} を設定', modeNotReady: 'コーデック未実装',
      fontLimitTitle: '現在の TX モードの文字サイズ上限：{limit}px', lastRXImage: '前回の RX 画像', waitingRXImage: '画像の受信待ち',
      backgroundPrefix: '背景：', defaultBackgroundColor: '単色背景', backgroundScale: '背景の拡大率', imageScaleLabel: '画像倍率（縦横比を維持）',
      horizontalOffset: '水平オフセット', horizontalPositionLabel: '水平位置', verticalOffset: '垂直オフセット', verticalPositionLabel: '垂直位置',
      customText: 'カスタムテキスト', templateImage: 'テンプレート画像', templateImageLoadFailed: 'テンプレート画像を読み込めません',
      libraryLoading: 'ライブラリを読み込み中…', libraryEmpty: '画像ライブラリは空です。上のボタンから画像をインポートしてください。', setAsBackground: '背景に設定', addLayer: 'レイヤーを追加', libraryReadFailed: '画像ライブラリの読み込みに失敗：{error}',
      imageImported: '画像をインポートしました：{name}', imageImportFailed: '画像のインポートに失敗：{error}', backgroundCleared: '共有背景を削除しました',
      latestRXAdded: '前回の RX 画像コンポーネントを追加しました', latestRXPlaceholderAdded: '前回の RX 画像プレースホルダーを追加しました。最初の受信画像が自動挿入されます',
      backgroundSaved: '共有背景を保存しました', backgroundKept: '共有背景を保持', templateLoaded: 'テンプレートスロット {slot} を読み込みました：{name}{background}', templateSaved: 'テンプレートスロット {slot} に保存しました：{name}',
      rxInitializingHint: 'RX オーディオストリームを初期化中…', rxStoppedHint: 'RX 停止中', rxLiveCapture: 'リアルタイム音声を受信中', rxWaitingSamples: 'RX 起動済み、オーディオ入力サンプル待ち…',
      rxLiveDecoding: 'リアルタイム画像をデコード中', rxImageCompleted: '画像を受信しました', txEncoding: 'SSTV をエンコード中', txPlaying: 'SSTV 送信中',
      audioInputIdle: 'RX 未起動', audioWaitingSamples: 'RX 起動済み、入力サンプル待ち', audioReadyStatus: '共有モードオーディオ準備完了', audioPendingStatus: 'オーディオデバイス待機中。ワークベンチ起動時に RX を開始します',
      imageDragScaleHint: 'ドラッグ / 拡大縮小', rxContinuous: 'RX 受信中', rxRestoring: 'RX を再開中', txRunningLabel: 'TX 動作中',
      droppedAudioBlocks: 'キャプチャキューで {count} 個のオーディオブロックを破棄', inputNearSilence: '{count} サンプルを受信しました。入力レベルが非常に低い状態です', samplesCount: 'リアルタイム音声取得中 · {count} サンプル',
      decodeComplete: '{mode} のデコード完了', receivingRows: '{mode} を受信中 · {rows}/{total} 行', waitingSignal: 'SSTV 信号待ち',
      liveImageAlt: '{mode} リアルタイムデコード画像', receivingImageAlt: '{mode} 受信画像', liveRows: '{mode} をリアルタイム表示 · {rows}/{total} 行',
      imageReceivedStatus: '画像を受信しました', imageAddedAlbum: ' · アルバムに追加しました', txStatePlaying: '送信中', txStateEncoding: 'エンコード中', txStateCompleted: '完了', txStateStopped: '停止', txStateError: 'エラー', txStateDraft: '下書き',
      audioDeviceCount: '入力 {inputs} 台、出力 {outputs} 台を検出', audioDeviceEnumFailed: 'デバイス列挙に失敗：{error}', serialDisconnected: '（切断）', serialNotFound: 'シリアルポートが見つかりません', serialEnumFailed: 'シリアルポートの列挙に失敗しました', audioInputOpening: '共有モードのオーディオ入力を開いています…', rxInitFailedShort: 'オーディオ初期化失敗',
      rxInitFailed: 'オーディオ初期化失敗：{error}。デバイス設定を確認してください。', rxAutoStartFailed: 'RX 自動起動失敗：{error}', txOutputNotReady: 'TX オーディオ出力が未初期化です。設定で出力デバイスを選択して適用してください。', txEncoderUnavailable: '現在の TX モードのエンコーダーは利用できません',
      spectrumPeak: 'ピーク {peak} Hz · クレスト {crest}', callsignSource: 'ソース：{source}', callsignFilled: '{source} でコールサイン {callsign} を認識し HisCall に入力しました', callsignPreserved: '{source} でコールサイン {callsign} を認識しました。手動入力した HisCall は保持します',
    }
  };

  function getLanguage() {
    const saved = localStorage.getItem(STORAGE_KEY);
    return dictionaries[saved] ? saved : 'zh-CN';
  }
  let language = getLanguage();
  function t(key, vars = {}) {
    const dict = dictionaries[language] || dictionaries['zh-CN'];
    let value = dict[key] ?? dictionaries['zh-CN'][key] ?? key;
    for (const [name, replacement] of Object.entries(vars)) value = value.replaceAll(`{${name}}`, String(replacement));
    return value;
  }
  function setNodeText(el, value) {
    const textNode = Array.from(el.childNodes).find(node => node.nodeType === Node.TEXT_NODE && node.textContent.trim());
    if (textNode) textNode.textContent = value + (textNode.textContent.endsWith(' ') ? ' ' : '');
    else if (!el.children.length) el.textContent = value;
  }
  function applyTranslations(root = document) {
    document.documentElement.lang = language === 'zh-CN' ? 'zh-CN' : language === 'ja' ? 'ja-JP' : 'en';
    document.title = t('appTitle');
    root.querySelectorAll?.('[data-i18n]').forEach(el => setNodeText(el, t(el.dataset.i18n)));
    root.querySelectorAll?.('[data-i18n-placeholder]').forEach(el => { el.placeholder = t(el.dataset.i18nPlaceholder); });
    root.querySelectorAll?.('[data-i18n-title]').forEach(el => { el.title = t(el.dataset.i18nTitle); });
    root.querySelectorAll?.('[data-i18n-aria]').forEach(el => { el.setAttribute('aria-label', t(el.dataset.i18nAria)); });
    root.querySelectorAll?.('option[data-i18n]').forEach(el => { el.textContent = t(el.dataset.i18n); });
    const selector = document.getElementById('languageSelect');
    if (selector && selector.value !== language) selector.value = language;
  }
  function setLanguage(next) {
    if (!dictionaries[next]) return false;
    language = next;
    localStorage.setItem(STORAGE_KEY, next);
    applyTranslations();
    window.dispatchEvent(new CustomEvent('satsstv-language-change', { detail: { language } }));
    return true;
  }
  const exactChineseKeys = new Map([
    ['编辑画面就绪', 'txReady'], ['就绪', 'ready'], ['停止发射', 'txStop'], ['开始发射', 'txStart'],
    ['停止单音', 'toneStop'], ['1500 Hz 单音', 'toneStart'], ['停止 RX', 'rxStopped'],
    ['等待接收图像', 'rxWaitImage'], ['等待 SSTV 信号', 'rxWaitSignal'], ['音频已就绪', 'audioReady'],
    ['音频待初始化', 'audioInitializing'], ['PTT 测试已停止', 'pttTestStopped'],
  ]);
  function localize(value) {
    const text = String(value ?? '');
    const key = exactChineseKeys.get(text);
    return key ? t(key) : text;
  }
  window.SATSSTVI18n = { t, localize, getLanguage: () => language, setLanguage, applyTranslations };
  document.addEventListener('DOMContentLoaded', () => applyTranslations());
})();
