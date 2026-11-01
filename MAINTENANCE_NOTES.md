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


## 2026-11-01 FSKID 互操作与 RX 尾部过载修复

### 音频处理偶发积压

- 对 48 kHz Robot 36、10 ms 音频块的实时路径做逐块耗时剖析，确认完整帧到达的最后一个 `Feed()` 曾同步执行整幅图像的像素重建，峰值约 56 ms，远高于一个采集块的时长。
- `StreamDecoder` 新增可选的 deferred-final-decode 模式。Wails RX 启用该模式后，`Feed()` 在帧结束时只返回 `DecodeRequest`（不可变频率块头快照、帧元数据和原始 PCM 视图），由独立帧工作线程执行 `DecodeFinal()`、PNG 编码和相册归档。通用库调用默认仍保留原先同步返回 `Image` 的行为。
- 相同基准下，启用 deferred 模式后帧尾 `Feed()` 降至约 27 µs；该结果说明已移除可复现的同步重建尖峰，但不能保证所有 Windows 音频驱动在任意系统负载下永不丢块。采集驱动/操作系统长时间调度停顿仍可能触发有界队列的断流保护。

### MMSSTV FSKID

- 对照随仓库提供的 MMSSTV C++ 源码：`OutputFSKID()` 依次输出 2100 Hz、100 ms guard；1900 Hz、22 ms start；`WriteFSK()` 按 B0→B5 输出 6 bit；标记为 `0x2A`；呼号字符减 `0x20`；`0x01` EOT；最后输出字符 XOR 校验。符号时长使用 C++ 累积采样位置的 `int(m_dPos)`，而不是逐段四舍五入。
- FSKID 接收器按 MMSSTV `DecodeFSK()` 的实际状态机工作：直接捕获 2100 Hz guard，估算 AFC，再检测 1900 Hz 起始位和 22 ms 数据符号。可选的 300 ms 1500 Hz（普通）或 1900 Hz（窄带）leader 不再是捕获前提；截断 leader 或环路尾部只保留 guard 时仍可识别。
- 完整图像结束后，主 RX worker 从 `StreamDecoder.PendingAudio()` 取得当前采集块中剩余的真实图像后音频，并将它与后续 PCM 顺序送至独立 FSKID worker。FSKID 不在图像解调线程运行；侧通道队列溢出只影响 FSKID 识别，不会重置 SSTV 图像接收。
- 前端 `rx:callsign` 事件会将合法呼号自动写入 `hisCall`，并分发 `input` 事件；如果输入框为空，或当前值是 FSKID 上次自动写入的值，可以填入/更新；用户手动修改后不会被后续识别覆盖。

### 本轮回归

- `CGO_ENABLED=0 go test ./internal/sstv -run 'FSKID|TestStreamDecoderCanDeferFinalRasterDecode|TestStreamDecoderReceivesRobot36At48k10msBlocks|TestStreamDecoderReceivesAllImplementedModesInChunks|TestStreamDecoderAutoAcceptsVISWithDamagedParity|TestStreamDecoderFixedModeFallbackSupportsMMSSTVAndNarrowVIS' -count=1`: passed.
- FSKID end-to-end regression combines a full Robot 36 SSTV transmission and MMSSTV-reference FSKID in one continuous PCM stream, deliberately splits the stream into 1024-sample blocks, and checks both Auto and fixed Robot 36 modes. It verifies only the post-frame tail is passed to FSKID and that the trailer does not falsely activate another SSTV frame.
- Existing FSKID tests cover normal/narrow leaders, direct guard acquisition without leader, sample rates 8/12/44.1/48/96 kHz, irregular capture chunks, receiver frequency offsets, deterministic broadband noise with reduced signal level, malformed candidates, resets, checksum, and MMSSTV LSB-first symbols.
- `CGO_ENABLED=0 go vet ./internal/sstv ./internal/audio ./internal/radio/... ./cmd/sstv-go`: passed.
- `node --check frontend/app.js`: passed. `node --test frontend/tests/*.test.cjs`: 5/5 passed, including four `rx:callsign`/HisCall integration-handler tests and the waterfall regression.
- The root Wails app package could not be fully tested in this container because required Wails v2.17.0 and malgo v0.11.26 modules are not cached and external module download is unavailable. The app changes were formatted and statically reviewed, but a real Windows Wails build and physical audio/RF validation remain outstanding.

## 2026-11-01 MMSSTV FSKID 反向互操作修复

### 根因与协议修正

- 进一步核对 MMSSTV 的 `CVCO::SetSampleFreq()`、`CVCO::Do()` 和 `CSSTVMOD::Write()` 后，纠正了此前对 `TxSampOffset` 的错误假设：它改变的是每个 FSK 音调写入的采样数量/持续时间；VCO 仍按音频设备的标称采样率产生 1900 Hz、2100 Hz 音调。不能从 2100 Hz guard 的频率偏移推算符号时钟。
- MMSSTV 窄带模式可在真正 FSKID guard 前输出 1900 Hz、300 ms leader。旧的宽 AFC Guard 粗捕获会把该 leader 当作 2100 Hz guard，或把 leader/guard 交界的混合 40 ms 窗口当作 Guard，随后锁错起点并跳过真实 FSKID。
- FSKID Guard 粗捕获与稳定窗的频率差门限收紧至 120 Hz，起点精修使用 90 Hz 容差，避免混合过渡窗被接受；测试覆盖 ±80 Hz 接收频偏。普通 1500 Hz leader 和窄带 1900 Hz leader 均不再作为 Guard 本身。
- 符号时钟从实际 MMSSTV 固定前导恢复：2100 Hz Guard 后 1900 Hz start，再跟随 `0x2A` 的六个 LSB-first 位，形成七个交替的 1900/2100 Hz 符号。对七个已知音调边界使用 Hann 加窗 Goertzel 能量比寻找转折点，再以七个边界的最小二乘拟合得到符号周期和起始相位。AFC 仅校正载波判决，不影响波特率估算。
- 载荷解码使用前导拟合得到的实际符号周期，不再用 `2100 Hz + offset` 推算时钟。附近的相位候选中只要有完整、校验正确的包即可确认；不再因末尾的其他相位窗口尚未收齐而无限保持“等待 FSKID”。
- 完整 SSTV 帧结束后，即使接收器暂时把窄带 1900 Hz leader 视为新的 SSTV 同步候选，也持续把剩余 PCM 顺序送到独立 FSKID worker；该监听仍具有 8 秒上限，并在输入断流时复位。

### 回归测试

- `go test ./internal/sstv -run 'FSKID' -count=1`: passed。覆盖普通/窄带 leader、无 leader 直接 Guard 捕获、8/12/44.1/48/96 kHz、任意 PCM 分块、±80 Hz 接收频偏、低电平叠加确定性噪声、损坏候选后恢复、呼号空格裁剪、XOR 校验、完整 Robot 36 后紧接 FSKID，以及 `TxSampOffset` 仅改变持续时间的 ±1.5% 时钟偏差。
- 增加最长 16 字符呼号在 48 kHz、±1.5% `TxSampOffset` 下的测试，验证长数据包不会因位周期误差累计而错位。
- 根包 `TestProcessRXFSKIDWorkDisarmsAfterIdentity`、`TestEnqueueRXFSKIDWorkResetsOnlySideChannelOnOverflow`、`TestMaterializeRXFrameEventDecodesDeferredRaster` 使用本机临时 Wails API stub 定向通过；这不是完整 Wails GUI 构建。
- `go test ./internal/audio ./internal/radio/... ./cmd/sstv-go`: passed。
- `go vet ./internal/sstv ./internal/audio ./internal/radio/... ./cmd/sstv-go`: passed。
- `node --test frontend/tests/*.test.cjs`: 5/5 passed；`node --check frontend/app.js` 和 Wails JS 绑定语法检查通过。
- 测试运行期间临时将本地工作副本 `go.mod` 中的 Go 版本改为 1.23.0，完成后恢复为项目声明的 `go 1.25.0`。当前容器没有实际 Windows 声卡，也没有用户侧 MMSSTV FSKID 录音；参考向量由仓库内 MMSSTV C++ 发送逻辑生成。因此这些结果证明源代码级协议回归通过，但仍需用实际 MMSSTV 发射录音做最终互操作确认。

## 2026-11-01 FSKID 每图接收周期与提交前仓库整理

### FSKID 偶发停留在等待状态

- 复现到 FSKID 前导和符号时钟已正确锁定，但载荷解析第一次返回 `incomplete` 后，候选状态固定等待额外 40 ms 才重试。若最后的有效呼号/校验符号刚好落在监听窗口末端或最后一个设备回调中，解码器已有足够样本却没有再触发判决。
- 待完成候选现在在每次追加新 PCM 后重试（通过 `nextTryLen = len(pending)+1` 保证下一块音频到达即重试），取消固定额外 40 ms 延迟。FSKID 定向测试在 8/12/44.1/48/96 kHz 和不规则采集分块下通过，包含信号恰好结束、不附加尾部静音的情况。

### 一张 SSTV 图像对应一个 FSKID 周期

- RX 主线程不再用 `fskidArmed` 和独立样本计数两个布尔/整数拼接生命周期，而是使用 `rxFSKIDCycle` 状态机：`Idle → Searching → Draining → Decoded/Expired`。
- 每次完整图像完成都会生成新的 `cycleID`，FSKID PCM 批次、重置命令和解码完成通知都携带该 ID。旧图像异步工作队列中的延迟批次或解码结果不能修改新图像的监听状态。
- 8 秒限制按输入采样数计算。主采集线程到达监听窗口末端时进入 `Draining`，先把最后不足 40 ms 的 PCM 批次入队，再发送有序 finish 标记；FSKID worker 处理完此前所有 PCM 后才确认 `Expired`。因此 worker 暂时落后时，已经进入队列的有效呼号仍有机会完成校验，不会被主线程提前关闭。
- 采集发生断流时，只重置 FSKID 部分符号时序，继续保留同一图像周期的监听预算；下一张图像完成时无条件建立新的周期。
- 新增测试覆盖连续多张图像分别解析不同呼号、旧周期迟到结果不能关闭新周期、8 秒样本预算、worker drain/finish 顺序、采集断流与 FSKID 队列溢出。

### 仓库整理脚本

- 新增 `scripts/cleanup_repo.py`，仅使用 Python 标准库。默认 dry-run，只有 `--apply` 才执行删除；`--apply` 默认要求输入 `yes`，`--apply --yes` 可用于自动化。
- 清理目标限制为明确的临时/诊断/编译产物和已知生成目录（例如 `bin/`、`dist/`、`build/bin/`、`frontend/dist/`、Python 缓存）。脚本不清理 `build/` 本体及 `build/windows` 资源，不扫描或删除 `mmsstv-master/`，不跟随符号链接。
- 在 Git 工作树中，脚本先确认传入路径就是 Git 根目录，再读取 `git ls-files`；任何被 Git 跟踪的候选文件都会跳过，包含已跟踪内容的生成目录也不会整体删除。嵌套 `.git` 目录会阻止目录清理。
- 新增 `scripts/test_cleanup_repo.py`，验证临时文件和构建目录识别、MMSSTV 源码/Windows 资源保留、跟踪文件保护以及实际删除行为；测试 3/3 通过。

### 本轮验证

- `CGO_ENABLED=0 go test ./internal/sstv -run 'FSKID' -count=1`: passed；覆盖 MMSSTV FSKID 协议、采样率、噪声/频偏、完整图像尾随 FSKID、连续两张图片各自的 FSKID，以及无尾部音频时的最终判决。
- 根包 FSKID 状态机定向测试：通过临时 Wails API 接口替身运行；测试覆盖每图周期 ID、过期/迟到结果、Draining 完成确认、同一周期连续识别和队列 overflow。
- `node --test frontend/tests/*.test.cjs`: 5/5 passed。
- `python -m unittest discover -s scripts -p 'test_*.py'`: 3/3 passed；Python `py_compile` passed。
- 项目 go.mod 已恢复 `go 1.25.0`。当前容器仅有 Go 1.23，真实 Wails 依赖无法联网获取；根 Wails 桌面程序及真实 Windows 声卡仍需在目标环境构建/验证。

### 本轮补充验证（FSKID 周期 drain）

- 新增 `TestRXFSKIDWorkerDrainsQueuedAudioBeforeFinishAndIgnoresStaleFinish`：将 MMSSTV 参考 FSKID 拆成不规则 PCM 批次入队，随后插入上一周期的过期 finish 标记和当前周期 finish 标记。测试要求工作线程必须先解出当前周期呼号，再确认当前周期结束；过期 finish 不得提前关闭当前周期。
- 根包 FSKID 周期相关测试使用临时 Wails API 替身运行，全部通过；`go vet .` 通过。项目 `go.mod` 已恢复 `go 1.25.0`。
- `CGO_ENABLED=0 go test ./internal/sstv -run 'FSKID' -count=1` 通过（约 29 秒）。
- `python -m unittest discover -s scripts -p 'test_*.py' -v`：3/3 通过；Python `py_compile` 通过。
- `node --test frontend/tests/*.test.cjs`：5/5 通过；`node --check frontend/app.js` 和 `node --check frontend/wailsjs/go/main/App.js` 通过。
- 清理脚本 dry-run 在移除测试生成的 `__pycache__` 后显示仓库无待清理项；dry-run 不执行删除。

## UI usability, PTT self-test, and localization update

- Added global non-selectable UI text while keeping `input`, `textarea`, `select`, and contenteditable values selectable/editable.
- Added a PTT self-test for RTS, DTR, and Hamlib Rigctld. The user can stop it early; the Go backend independently enforces a 15-second maximum and releases/closes the PTT controller on timeout, settings application, or application shutdown. VOX mode does not show a software PTT test control.
- Prevented the settings dialog from closing via backdrop click or Escape. The Save Settings action closes the dialog only after station settings and audio/PTT settings have both been successfully saved/applied. The explicit close button remains available.
- Added persistent Chinese (Simplified), English, and Japanese localization with a language selector in the top bar. Static UI strings are translated from `data-i18n` attributes, and PTT/settings/device status strings use the selected locale.
- Added frontend tests for locale switching/persistence, PTT test UI/watchdog contract, and settings-dialog dismissal/save behavior.

Validation in this environment: Node syntax checks passed; 12 frontend tests passed; HTML parser and locale-key completeness checks passed; Go PTT/Rigctl/serialptt package tests passed using the locally available Go 1.23.2 with a temporary `go.mod` toolchain directive, which was restored to Go 1.25.0. A full root-package build/test could not complete because Wails v2.17.0 and Malgo are not cached and the environment cannot reach `proxy.golang.org`.

## 2026-11-02 UI 防误触、PTT 测试及三语 i18n

### 界面交互

- 对整个应用界面启用 `user-select: none`，避免按钮、状态栏、标签、普通文本和图表标题被鼠标拖选；`input`、`textarea`、`select`、`contenteditable` 及 `.selectable-text` 仍明确允许选择和编辑。
- 设置对话框移除通过背景空白区域关闭的逻辑，并拦截 Escape `cancel` 事件，防止未保存表单因误触关闭。右上角显式关闭按钮仍可关闭；点击“保存设置”时先持久化本站资料、再应用音频/PTT 配置，全部成功后才关闭对话框，失败则保留页面并显示错误。

### PTT 自检

- RTS、DTR、Hamlib Rigctld 三种硬件控制方式增加同一个 PTT 测试按钮；VOX 模式隐藏该按钮。
- 前端显示 15 秒倒计时，可提前按同一按钮停止。Go 后端独立执行 15 秒 watchdog，执行 PTT 释放和控制器关闭，不依赖前端计时器或窗口是否仍然打开。
- 测试与 TX 启动通过 `txOpMu` 串行化，避免 PTT 自检与正常发射同时控制线路；关闭应用、重新应用音频设置时也会释放正在进行的测试。
- 增加 `StartPTTTest` / `StopPTTTest` Wails 绑定，并同步更新 `frontend/wailsjs/go/main/App.js` 和 `.d.ts`。

### 简体中文 / English / 日本語

- 新增 `frontend/i18n.js`，语言选择位于顶部栏，支持 `zh-CN`、`en`、`ja`，通过 `localStorage` 持久化。
- HTML 使用 `data-i18n`、`data-i18n-placeholder`、`data-i18n-title` 和 `data-i18n-aria`；应用动态 RX/TX 状态、音频遥测、进度、FSKID 呼号提示、图库动作和模板编辑属性也使用对应语言词条。
- 新增/扩展前端测试，覆盖语言切换和持久化、动态插值、PTT 自检限制/15 秒 watchdog、设置对话框 backdrop/Escape 行为和保存后关闭。

### 验证

- `node --check frontend/app.js`、`node --check frontend/i18n.js`、Wails JS 绑定语法检查：通过。
- `node --test frontend/tests/*.test.cjs`：12/12 通过。
- HTML 解析通过；98 个 HTML i18n 属性引用均能找到词条；app.js 使用的动态 i18n key 在简体中文、英文、日文三套字典中均存在。
- `.github/workflows/go.yml` YAML 解析通过，并已将 Node 测试步骤改为运行全部 `frontend/tests/*.test.cjs`。
- `go test ./internal/radio/ptt ./internal/radio/rigctl ./internal/radio/serialptt`：通过。测试期间临时把本地 `go.mod` 的 `go 1.25.0` 改为 `go 1.23.0` 以适配当前容器，结束后已恢复 `go 1.25.0`。
- 根 Wails app 包及完整桌面构建未能在此容器执行：本机只有 Go 1.23.2，且 Wails v2.17.0 / Malgo 依赖未缓存，外网模块下载受限。未进行真实串口 PTT 硬件测试。


## 2026-11-02 UI/Station Defaults Fix

- RX Auto selection now checks the actual backend `audioState.rxActive` value. If the UI persisted an Auto selection while the native receiver is inactive, one click starts the receiver instead of toggling the stale selection off. Startup calls the explicit `InitializeAudio` API and retries once if RX is still not active after the initial state query.
- Template switching now has a single revision-guarded refresh path: update font limits and property controls, draw immediately, and draw again on the next animation frame. `renderTemplates` previously named its loop parameter `t`, shadowing the translation function `t()`; the fourth blank template invoked `t('blankTemplate')` and threw, aborting the template-switch handler before the canvas redraw. The loop variable is now `templateSlot`, and a regression test renders all four presets.
- Station defaults are now `N0CALL` and `JJ00` in the UI and Go settings backend. Missing or blank legacy settings are normalized to these defaults. QSO token `{HISCALL}` resolves to `ToCall` when the HisCall input is empty.
- Added frontend regression tests for stale Auto state, canvas refresh, all four template slots, station defaults, and `{HISCALL}` fallback. Added Go station settings tests for first-run defaults, legacy blank-value migration, and saving defaults.
