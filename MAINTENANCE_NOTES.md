# SATSSTV-Go maintenance update

本次维护基于上传的仓库快照完成，覆盖 RX 音频链路、实时 SSTV 解码、瀑布图和 TX 模板编辑器。

## 修改内容

- RX 音频：把 PCM 解码与 UI 频谱遥测拆分为独立消费者；遥测允许合并中间状态，不再阻塞 SSTV PCM 顺序处理。缩小应用队列和采集队列的积压窗口。
- 采集过载恢复：采集队列满时丢弃过期积压并按顺序发送断流标记和最新音频块。RX 收到标记后重置 SSTV 与 FSKID 解码状态，避免跨丢样区间拼接图像。
- 模式切换：RX 模式选择通过原子请求在音频块边界应用；切换模式时清空旧模式的 PCM、频率跟踪和预览状态。
- 诊断录音：使用固定容量环形缓冲区保存最近 10 秒 PCM，避免缓冲区满后每个音频块都移动整段历史数据。
- 瀑布图：使用 64 行循环离屏画布，每次只写入新的一行；修复环形缓冲区绘制时把行指针误用为源 X 坐标的问题，现按源 Y 坐标拼接两段连续行，并裁剪到单一瀑布图显示区域。此前的 X 坐标错误会随缓冲区前进造成向左漂移和视觉上的重复/多层纹理。
- 图片图库：新增本机持久化图库，支持导入 PNG/JPEG、缩略图缓存、选择为模板底图或添加为图像图层。模板内嵌图片数据，因此图库内容变更不会破坏已保存模板。
- 模板编辑：底图支持缩放和拖动，并随模板保存；普通图像和上次 RX 图像统一为单一缩放比例，始终保持源图宽高比。
- 字号：文字字号最大值根据当前 TX 模式的分辨率动态计算，切换模式时更新上限并约束已有文字组件。
- 绑定：同步更新 Wails JavaScript 和 TypeScript 方法绑定，以及图库条目数据模型。

## 瀑布图专项修复（2026-11-01）

- `drawWaterfall()` 的源矩形修正为 `(sx=0, sy=waterfallNextRow, sw=width, sh=olderRows)`，第二段为 `(0, 0, width, waterfallNextRow)`。
- 使用 Canvas clip 限定在瀑布图矩形内，避免绘制越界；两个环形区段仅首尾拼接成一张连续瀑布图，不是多个叠加图层。
- 已用 Node Canvas API mock 检查环形指针位于 0、中间位置和接近末尾时的源/目标矩形参数，并检查 JS 语法。

## 已执行检查

- 43 种已实现模式：以 8 kHz 合成音频、2048 样本分块，使用默认 Auto `StreamDecoder` 连续接收；43/43 正确识别模式并输出对应分辨率。
- SSTV 实时流：分块接收、模式切换后清理旧缓冲、固定模式跳过其他模式帧等定向测试通过。
- FSKID：输入断流后清理残留候选，并能从下一段完整有效信号重新识别。
- Go 根包、音频、PTT、Rigctl 测试通过；全部 Go 包的编译检查通过；`go vet` 检查通过。
- 前端：`app.js` 和 Wails JS 绑定语法检查通过；Wails TypeScript 模型/声明检查通过；HTML ID 无重复，脚本引用的 100 个元素均存在。

## 尚未完成的环境验证

当前执行环境只有 Go 1.23.2，而仓库要求 Go 1.25.0；环境无法访问 `proxy.golang.org`，未缓存完整 Wails 和原生音频依赖。因此，Go 根包和包级测试使用了临时的最小 Wails API 替身完成编译/测试，该替身未包含在项目中；这不等于真实 Wails 桌面程序构建成功。全量历史 SSTV 测试组在执行时限内未能完成，已单独运行并通过本次修改相关的实时解码回归测试。当前环境没有 Windows 声卡和真实 SSTV RF/音频输入，故音频设备负载表现、实际瀑布图帧率和接收效果仍需在目标 Windows 设备上验证。

在安装仓库要求的 Go 1.25.0、Wails CLI 和依赖后，建议在目标 Windows 环境运行：

```powershell
go test ./...
go vet ./...
wails build
```

## 实时接收与 MMSSTV 频谱对照修复（2026-11-01）

- 单音造成 Auto 检测过载：空闲状态下 Auto 每个音频块会反复检查标准 VIS、MMSSTV 扩展 VIS、N-VIS；旧实现对长窗口重复执行 Goertzel，3 秒纯 1500 Hz 输入在基准环境曾耗时约 7.3 秒。现使用已有的连续频率轨迹对 1900 Hz 导频做低成本预筛，只有疑似导频才执行精确 PCM 判决；3 秒音频的专项实时预算测试要求处理时间低于 2 秒。
- 低采样率解调：瞬时频率跟踪的最小半周期由 2 个采样点修正为 1 个采样间隔，允许 8 kHz 输入正确跟踪 2 kHz 以上的 SSTV 色调。
- RX 队列：原生采集队列和应用解码队列均缩至 8 块。应用回调不再阻塞等待解码器；过载时清除旧积压并放入有序断流标记和最新块，解码器重置后从下一次 VIS 重新同步。频谱遥测队列独立处理，遇到自己的积压也重置分析窗口，避免将断流前后的频谱拼接。
- MMSSTV 频谱：放弃常规 Hann FFT 幅度谱，按 `mmsstv-master/Fft.cpp` 的 `CFFT::CalcFQC` 和 `mmsstv-master/sstv.cpp` 的 `CFQC::Do` 复现默认 FQC 路径：输入按采样率 /4、/2 或直通抽取；2048 点原生频率格；使用 `fir.cpp::MakeIIR` 对应的预畸变二阶 Butterworth 400 Hz 低通；中心频率桶 +30、相邻桶 +15、中心桶上限 720（FFTGain=5）；最后按 700–2700 Hz（FFTWidth=1）映射到显示宽度。
- 频谱 UI：移除额外的黄色峰值保持线，仅保留 MMSSTV 风格的单条蓝色实时曲线；频率刻度改为 700–2700 Hz；瀑布图按 MMSSTV 对应窗口时长刷新（48 kHz 时约 171 ms），使用相同 FQC 直方图数据。
- TX 启动：VOX 没有软件 PTT 硬件控制线，不再等待默认 300 ms PTT 前置延时；硬件 PTT 仍严格等待配置延时。连接音频时提前初始化并启动共享播放端点，空闲期间只输出数字静音，避免第一次发射时才初始化设备。
- Progressive RX 预览由约每 12% 降为每 20% 一次，降低高分辨率帧接收期间重复全帧重绘/JPEG 编码的 CPU 抢占，完整帧仍使用原解码路径。

## 本轮测试

- MMSSTV FQC 频谱：8/16/24/48 kHz 输入；1200/1500/1900/2300 Hz 参考音；静音底噪窗口；频率峰值及幅度响应测试通过。400 Hz 低通使用 MMSSTV 原生 `MakeIIR` 系数，而非近似 RBJ biquad；初始滤波器状态预置为静音频率，避免瀑布图启动时生成 1900 Hz 假横线。
- StreamDecoder：3 秒 1500 Hz 空闲单音实时预算、Robot 36 分块接收、43 种实现模式 Auto 分块接收、长静音后识别、扩展/N-VIS 跨块识别、模式切换/清缓冲和跳过坏帧继续解码测试通过。最近一次单音基准：3 秒输入处理约 90 ms。
- 根包定向测试、音频 / PTT / Rigctl 测试、根包 `go vet`、`app.js` Node 语法检查通过。VOX 不等待 300 ms、硬件 PTT 仍遵守配置的延时测试通过。新增 `frontend/tests/waterfall.test.cjs`：环形指针 0/1/17/32/63 时验证横向原点固定、两段源行连续覆盖 64 行、目标区域无缝拼接；Node 测试通过。
- 全量 `internal/sstv` 历史测试组在 170 秒执行限时内未完成；不是已确认失败。已单独运行本轮相关解码与频谱回归集并通过。

## VIS 自动识别与固定模式不解码修复（2026-11-01）

- 根因一：标准 VIS 检测主要依赖频率跟踪器的零交叉平均值。接收音频中存在频偏、宽带噪声、低电平或 SSB 滤波时，频率平均值可能不足以可靠地区分 1100/1200/1300/1900 Hz 符号。现在标准 VIS 增加基于原始 PCM 的窄带 Goertzel 能量判决，逐符号确认 leader、break、second leader、start、7 bit 数据、parity 和 stop；原频率轨迹路径保留为兼容回退。频谱 FQC 数据仍仅供显示，未被送入图像解调器。
- 根因二：固定模式此前仍要求完整 VIS 码字先识别成功，因此手动选中 Robot 36、PD120 等模式并不能绕过损坏的 VIS 数据位/校验位。现在固定模式在完整头部识别失败时，可按选中模式对应的头部家族（标准 VIS、MMSSTV 扩展 VIS、N-VIS）寻找可靠的同步前导，并锁定用户选择的模式；它仍要求有效的导频/分隔音/第二导频前导，不会仅凭任意音频强行开始解码。AVT90 继续使用其原生同步头检测。
- 根因三：持续 1900 Hz 导频会使扩展 VIS 与 N-VIS 搜索重复执行昂贵的长窗口 Goertzel 检测。两种路径现在先使用已有频率轨迹廉价验证后续分隔音（扩展 VIS 约 1200 Hz；N-VIS 约 2100 Hz），通过后才进行 PCM 精确判决。
- 新增回归：标准 VIS 低电平 + 确定性宽带噪声；VIS ±45 Hz 频偏与任意起始偏移；损坏标准 VIS 校验位时固定 Robot 36 仍可解码；将 MP73 扩展 VIS 和 MP73-N N-VIS 分别替换成未注册码后，固定模式仍能通过同步前导完成整帧解码；43 种模式头部家族在 48 kHz 下识别；Robot 36 在 48 kHz/10 ms 小块下 Auto 与固定模式都完成整帧解码；3 秒连续 1900 Hz 空闲输入实时预算。

## 本轮 RX 解码验证结果

- 43/43 已实现模式的头部识别在 48 kHz 合成输入下通过；该项测试确认 VIS/扩展/N-VIS/AVT 头部识别，不等同于 43 种模式的整帧图像解码。
- 完整流式图像解码抽样通过：Robot 36、Scottie 1、Scottie 2、Martin 1、Martin 2、PD120、PD160、MP73、MP73 Narrow、AVT90；Robot 36 另通过 48 kHz、10 ms 分块下的 Auto 与固定模式双路径。固定模式对损坏头部的整帧回退测试覆盖 Robot 36、MP73 和 MP73-N。
- 噪声、频偏、任意起始偏移、损坏 VIS 校验位固定模式回退测试通过。
- 3 秒 1500 Hz 空闲音约 88 ms 完成；3 秒 1900 Hz 连续导频约 191 ms 完成，均低于 2 秒实时预算阈值。
- `go vet ./internal/sstv` 通过。因本容器 Go 版本低于 go.mod 声明的 Go 1.25.0，执行这些包级测试时仅临时将本地工作副本 go.mod 改为 Go 1.23.0，测试后立即恢复；提交/交付项目仍保持 Go 1.25.0。
- 全部 43 种模式的完整高分辨率帧测试未能在本环境的执行时限内跑完；因此不把它列为通过。实际 Windows 声卡/RF 接收仍需目标设备验证。

## 2026-11-01 RX progressive decode, Auto VIS and persistent TX background

### RX progressive rendering and backlog

- `StreamDecoder.Feed` no longer reconstructs an ever-growing partial raster synchronously on the audio-processing path. It emits immutable frequency-track preview requests; `runRXPreviewWorker` reconstructs pixels and JPEG-encodes off the ordered PCM worker.
- Preview requests are latest-wins and carry a monotonically increasing sequence/frame ID. The frontend worker drops requests that became stale during reconstruction or after the final frame was delivered.
- Progressive updates are generated at approximately 1/60 of the image height per update (about four rows for a 240-line Robot 36 frame), replacing the former 20% / approximately 48-row cadence.
- The test suite drains preview requests per audio block, matching the production RX worker rather than retaining a frame's worth of large snapshots in memory.

### Auto VIS recognition

- Standard VIS Auto detection now tolerates a damaged parity symbol when the 1900 Hz leaders, 1200 Hz break/start/stop symbols, and all seven data symbols remain confidently classified. Mode identity still comes from the seven data bits.
- Standard VIS, MMSSTV extended VIS and N-VIS Auto detection now estimate their AFC offset from the leader and search a wider practical tuning range (approximately ±350 Hz).
- Added coverage for bad-parity Robot 36 Auto reception and ±240/±300 Hz standard, extended and narrow-band header detection.

### Persistent TX background

- Added `tx-background.json` in the user's SATSSTV-Go configuration directory. The shared background, image data, scale and pan are saved independently of the four template slots.
- Template slot changes preserve the shared background. On startup, the shared background is restored automatically; older projects migrate the first background embedded in a template slot.
- Explicitly clearing the background removes both the global setting and old per-slot copies so startup migration cannot unexpectedly restore it.
- Added Wails JavaScript/TypeScript bindings for GetTXBackground, SaveTXBackground and ClearTXBackground.

### Regression checks for this update

- `go test ./internal/sstv -run 'TestDetectAllModeHeaderFamiliesAt48k|TestDetectModeHeaderAFCOffsetsForExtendedAndNarrowVIS|TestDetectModeHeaderSupportsWideAutoFrequencyOffsets|TestDecodeToleratesFrequencyOffset|TestStreamDecoderAutoAcceptsVISWithDamagedParity|TestStreamDecoderEmitsProgressiveImagesBeforeFrameCompletion|TestStreamDecoderReceivesRobot36At48k10msBlocks|TestStreamDecoderFixedModeFallsBackWhenVISBitsAreDamaged|TestStreamDecoderFixedModeFallbackSupportsMMSSTVAndNarrowVIS|TestStreamDecoderIdle.*'`: passed.
- `go test ./internal/sstv -run '^TestStreamDecoderReceivesAllImplementedModesInChunks$'`: passed (all implemented modes in the live chunked path).
- `go test ./internal/radio/...`: passed.
- `node frontend/tests/waterfall.test.cjs`: passed.
- `node --check frontend/app.js` and `node --check frontend/wailsjs/go/main/App.js`: passed.
- `tsc --noEmit --target es2022 --module esnext --moduleResolution bundler --skipLibCheck frontend/wailsjs/go/main/App.d.ts frontend/wailsjs/go/models.ts`: passed.
- The global background and template persistence tests were run in an isolated Go harness with the actual `settings.go`, `templates.go`, `templates_test.go`, and `tx_background.go` sources because Wails and malgo dependencies are not cached in the current environment.
- The complete `internal/sstv` historical test suite exceeded the current command execution time limit; it is not reported as a complete pass. The project requires Go 1.25, while this environment provides Go 1.23.2 and cannot download Wails/malgo dependencies. The repository's `go.mod` version remains 1.25.0.

### Fixed-mode wide AFC parity check

- The fixed-mode synchronization-prefix fallback now uses the same approximately ±350 Hz leader offset window as Auto mode (previously ±220 Hz), so manual mode selection does not lose lock at offsets that Auto can accept.
- Added `TestFixedModePrefixSupportsWideFrequencyOffsets` for -300 Hz and +300 Hz; the test allows one fine-search step of timing tolerance.
