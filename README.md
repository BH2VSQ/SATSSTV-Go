# SATSSTV-Go

Go 语言 SSTV 编解码与 Wails 跨平台桌面收发工作台。项目包含 43 种指定 SSTV 模式、图文模板编辑、实时音频 RX/TX、音频频谱、PTT 控制和历史相册。

## GitHub Actions 自动构建

`.github/workflows/go.yml` 会在每次向任意分支 push commit、创建/更新 Pull Request，或手动触发时自动执行：

- Windows x64、Linux x64、macOS Apple Silicon（arm64）和 macOS Intel（amd64）分别运行 Go 测试与 `go vet`。
- 运行前端 JavaScript 语法/Node 测试，以及仓库清理脚本的 Python 单元测试。
- 使用 Wails CLI v2.17.0 和 CGO 原生工具链构建桌面应用，同时构建命令行工具。
- 将各平台压缩包上传到当前 Actions workflow run 的 Artifacts，保留 14 天。

每次 workflow 成功后会产生以下压缩包：

| Artifact | 内容 |
| --- | --- |
| `SATSSTV-Go-windows-amd64` | Windows GUI `.exe` + CLI `.exe` |
| `SATSSTV-Go-linux-amd64` | Linux GUI + CLI，`.tar.gz` |
| `SATSSTV-Go-macos-arm64` | Apple Silicon `.app` + CLI |
| `SATSSTV-Go-macos-amd64` | Intel `.app` + CLI |

构建产物只作为 Actions Artifacts 保存，不创建 Git Tag，不发布 GitHub Release。打开仓库的 **Actions → 对应 workflow run → Artifacts** 即可下载。

Linux CI 使用 Ubuntu 24.04 x64 和 WebKit2GTK 4.1（`webkit2_41` 构建标签）；macOS 分别生成 Apple Silicon 与 Intel 两种原生架构包。Linux 桌面运行时需要 GTK 3 与 WebKit2GTK 4.1；Windows 桌面使用 WebView2；macOS 使用系统 WebKit。Windows RTS/DTR 串口 PTT 仍仅在 Windows 上可用。

## GUI 工作台

- MMSSTV 风格 RX 模式栏位于 RX 区域顶部，包含 Auto 和 8 个可配置槽位。左键选择，右键更换槽位模式；再次点击当前选项可停止接收。
- 工作台启动时按保存的 RX 选择初始化音频。Auto 自动识别帧头，固定槽位按指定模式接收；取消当前 RX 选择后会停止采集与解码，同时保留音频输出供 TX 使用。
- TX 模式栏位于 TX 区域顶部，不提供 Auto，首次默认 Robot 36。支持 1500 Hz、10 秒单音测试，可提前停止；SSTV 发射仍使用独立的开始/停止按钮。
- RX 频谱使用 FFT 实时频谱、蓝色信号曲线、黄色峰值保持线和高对比度黑白瀑布图。音频处理队列有界并优先保留最新采样块，降低解码落后时的显示延迟。
- 音频输入和输出均使用共享模式，避免独占系统音频设备。RX 采集与 TX 播放使用独立流，发送期间继续运行 RX。
- TX 支持多个独立文字及图像组件。对象可在画布上拖动，文字支持字号、颜色、粗体和坐标设置，图像支持位置、宽度和高度调整。
- 模板固定为四个槽位，默认前三个为 CQ、RST、73，第四个为空白。用户可以修改模板设计和名称，保存按钮直接覆盖当前选中的槽位，无需额外输入保存文件名。
- 可以从文件添加图像，也可以插入“上次 RX 图像组件”，组件会自动载入最近一次接收图像并显示在当前画布中，支持调整位置和尺寸，添加的图像会随模板保存。RX 相册自动归档，不提供单独的 RX 历史图像导入按钮。
- 动态模板变量包括 `{MYCALL}`、`{HISCALL}`、`{QTH}`、`{LOCATOR}`、`{REPORT}`、`{MESSAGE}`、`{DATE}` 和 `{TIME}`。
- 首次启动的本站呼号和网格保持空白。示例模板只使用虚构数据，例如 `N0CALL`、`N0TEST`、`EXAMPLE QTH`、`AA00AA`。真实呼号和网格由用户在设置页自行填写。
- 本台呼号及本站网格保存在 `<UserConfigDir>/SATSSTV-Go/settings.json`。模板保存在同目录的 `templates.json`，音频设置保存在 `audio.json`。Windows 通常为 `%APPDATA%\SATSSTV-Go\`。

## 实时音频与电台控制

- `MalgoDuplex` 使用独立的输入采集流和输出播放流。TX 播放不会主动停止 RX 采集，RX 解码和电平/频谱遥测继续运行。
- RX 支持 Auto 帧头识别和固定模式槽位。收到有效帧头后，解码器保留跨音频块的 PLL 状态并持续重建图像预览，界面通过 `rx:preview` 显示渐进式图像和行数；完整帧完成后再以 PNG 保存并归档。输入电平、频谱和瀑布图均由实时采集回调更新，等待输入采样时会显示明确提示。
- TX 支持把当前编辑画布编码为 PCM 并实时播放。发射结束后将图像归档到 TX 相册。单独生成 WAV 时也会归档，状态明确标记为 WAV 已生成。
- PTT 不提供手动模式，首次启动默认为 VOX，由电台检测 SSTV 音频自动进入发射。仍可按设备需要选择 Hamlib `rigctld`、Windows 串口 RTS 或 DTR。旧版本保存的 `manual` 设置会自动迁移为 VOX。
- RTS/DTR 当前由 Windows COM 口实现；非 Windows 平台会返回明确的暂不支持错误。VOX 模式由电台自身检测发射音频，不发送硬件 PTT 命令。
- `settings/audio.json` 保存采样率、输入/输出设备、PTT 方式、串口或 rigctld 地址、电平极性及 PTT 前后延时。

软件全双工只保证采集流与播放流可并行运行。真实 RF 全双工仍取决于电台是否有独立的同时收发链路、音频路由以及射频/天线隔离。半双工电台无法仅靠软件获得空中全双工能力。

## SSTV 模式目录

模式目录严格包含以下 43 项，顺序与用户指定清单一致：

1. B/W 8
2. B/W 12
3. Robot 24
4. Robot 36
5. Robot 72
6. AVT 90
7. Scottie 1
8. Scottie 2
9. ScottieDX
10. Martin 1
11. Martin 2
12. SC2 180
13. SC2 120
14. SC2 60
15. PD50
16. PD90
17. PD120
18. PD160
19. PD180
20. PD240
21. PD290
22. P3
23. P5
24. P7
25. MP73
26. MP115
27. MP140
28. MP175
29. MR73
30. MR90
31. MR115
32. MR140
33. MR175
34. ML180
35. ML240
36. ML280
37. ML320
38. MP73-N
39. MP110-N
40. MP140-N
41. MC110-N
42. MC140-N
43. MC180-N

编解码实现覆盖标准 VIS、MMSSTV 16 位扩展 VIS、N-VIS 和 AVT 90 的三重 VIS/数字倒计时同步头。Robot、Martin、Scottie、SC2、PD、Pasokon、MP、MR/ML、MC-N 和 AVT 家族均有对应编码与解码分支。测试包含 43 种模式逐一编码、帧头识别和自身波形解码回环；部分扩展模式还需要与 MMSSTV/QSSTV 实机互操作复核。

回环测试证明编码器与解码器实现相互一致，不代表已经通过所有型号硬件和 MMSSTV 版本的空中互操作验证。正式上机前建议先用 WAV 文件与 MMSSTV/QSSTV 做双向对测，尤其关注长时序模式、窄带模式与 AVT 90。

## 历史图像相册

RX 和 TX 图像分别归档到 `<UserConfigDir>/SATSSTV-Go/album/`。每条记录包含模式、类型、状态和时间戳。右键缩略图可导出带毫秒时间戳的 PNG 文件。相册索引为 `album.json`，图片为独立 PNG 文件。

## v3.7 自动识别与实时图像更新

- RX Auto 会先完成标准 VIS、MMSSTV 16 位扩展 VIS、N-VIS 和 AVT 90 的帧头族判别，再锁定模式，降低扩展 VIS 前 7 位与标准 VIS 偶然重合时选错模式的概率。VIS 符号支持频率偏移搜索、细化帧头起点，并校验起始位、奇偶校验位和停止位；噪声或低能量符号会被拒绝。
- SSTV 图像在接收过程中持续生成 JPEG 预览，通过 `rx:preview` 事件更新 RX 图像区域。解码器跨输入块保存 PLL 状态，避免每个采集块重新建立频率跟踪。预览快照约每 1.5% 图像高度刷新一次，完整帧完成后仍保存原始 PNG 到相册。
- “上一次 RX 图像”组件始终可插入。如果尚无接收图像，会在画布上显示具有可调位置和尺寸的空白占位框，不再弹出错误。收到首张完整 RX 图像后，组件自动载入最新图像；删除最近一张 RX 图像后，会自动切换到前一张可用图像，若已无图像则恢复占位框。
- 增加渐进式图像测试，覆盖 Robot 36、Martin 1 和 PD 120；检查多次预览更新、单调递增的行数，以及预览中已完成区域与最终图像像素一致。增加频偏和任意帧头起始偏移测试。

## v3.6 收发与 FSKID 更新

- 移除手动 PTT 选项并以 VOX 作为首次启动默认方式，旧配置中的 `manual` 会迁移为 `vox`。RTS、DTR 和 Hamlib 仍可作为外部电台控制选项。
- RX 解码进度事件使用独立节流时钟，按实时接收样本更新进度百分比和估算行数。音频遥测与 RX 进度互不影响。
- 瀑布图和输入电平增加采集状态反馈：音频设备启动后会等待第一个输入采样块，收到采样后显示实时电平和频谱；低电平时明确提示当前输入接近静音。
- TX 的开始与停止整合为一个按钮，空闲为绿色，发射期间为红色。进度条覆盖图像编码及播放阶段。
- 移除相册中的 RX 历史图像导入入口。TX 模板中的“上次 RX 图像组件”会把最近一次自动归档的 RX 图像插入为可移动、可缩放的图层。
- 发射结束后自动附加 MMSSTV 兼容 FSKID 呼号识别数据。接收端在完整 SSTV 图像结束后，将图像尾部剩余 PCM 和后续音频送入独立 FSKID 工作线程；校验通过后显示识别到的呼号，并自动填入 HisCall（对方呼号）输入框。若用户已经手动填写 HisCall，则保留手动内容。
- FSKID 按 MMSSTV 原始 C++ 实现传输：固定 22 ms 符号间隔（约 45.45 baud）、6 bit 符号、B0 到 B5（LSB-first）、`0x2A` 呼号起始标记、`0x01` 结束标记及字符 XOR 校验。普通模式可带 1500 Hz 前导音，窄带模式可带 1900 Hz 前导音；接收端按 MMSSTV `DecodeFSK()` 的方式直接捕获后续 2100 Hz/100 ms guard，再检测 1900 Hz 起始位，因此不依赖可选的前导音完整存在。每张完整图像都会开启独立 FSKID 监听周期，带周期 ID 的异步批次避免前一张图像的迟到结果影响下一张；监听结束时等待 FSKID worker 消化完已入队 PCM 后再关闭。当前实现仅发送/解析呼号字段，不包含 MMSSTV 可选的比赛编号扩展字段。

FSKID 兼容性依据：[MMSSTV 原始 FSKID.TXT](https://github.com/n5ac/mmsstv/blob/master/fskid.txt)。


### FSKID 与 RX 实时性维护（2026-11-01）

- 完整 SSTV 帧完成时，`StreamDecoder.Feed()` 不再同步重建整张最终图像。它只提交不可变频率块快照及原始 PCM 视图；独立帧工作线程执行最终像素重建、PNG 编码和相册写入，降低最后一个音频块的突发耗时。
- FSKID 采用独立有界工作队列，不在 SSTV PCM 解码线程执行符号扫描。帧完成时从 `PendingAudio()` 获取同一个采集块中真正位于图像尾部之后的 PCM，再按顺序追加后续音频。
- 接收器与 MMSSTV 原始 C++ `OutputFSKID()` / `DecodeFSK()` 对齐：2100 Hz/100 ms guard、1900 Hz/22 ms 起始位、22 ms 符号、六位 B0-first、`0x2A` STX、`0x01` EOT 和 XOR checksum。AFC 从稳定 guard 估算，不要求 300 ms 1500/1900 Hz 前导音完整保留。
- `rx:callsign` 事件在验证呼号后自动填写 HisCall；只在输入框为空或其当前值此前由 FSKID 自动填写时更新，用户手动输入后不会被覆盖。输入事件会同步触发表单状态更新。
- Node 回归覆盖空白输入自动填写、后续 FSKID 更新此前自动填写的值、保护手动输入及拒绝非法呼号；Go 回归覆盖 MMSSTV 参考符号顺序、多采样率/任意块边界、频偏、截断前导，以及整帧后紧接 FSKID 的 Auto/固定模式端到端接收。

## v3.5 交互调整

- RX 模式与 TX 模式拆分。RX Auto/固定模式只控制接收解码策略，TX 仅选择具体模式，默认 Robot 36。
- 去除界面上的音频连接、断开和接收启动控制；工作台自动初始化共享模式音频流。内部设备生命周期方法改为非导出方法，不再作为 Wails 前端绑定接口暴露。
- TX 点击发射不再显示二次确认。
- 模板调整为四个持久化槽位，前三个为 CQ、RST、73，第四个为空白。保存直接覆盖当前槽位，可选修改名称。
- 模板支持图像图层，可导入本地图片或插入上一张 RX 图像，并调整位置和宽高。
- 相册支持点击大图预览及右键删除，模式栏限定在工作台页面。
- Malgo capture/playback 明确指定 `malgo.Shared`，避免独占系统音频端点。

## v3.4.1 收尾修正

- 接收频谱增加 1200、1500、1900、2300 Hz 调谐参考标记，并增加滚动瀑布历史。
- 用户在 TX 编码阶段主动停止时显示正常停止状态，不再误报为编码错误。
- 本站设置、音频设置和相册索引采用带回滚的替换流程，尽量避免 Windows 文件替换失败造成原文件丢失。
- 增加 TX 取消时释放 PTT 的回归测试。
- Go 主工程与建议使用的 Wails CLI/运行时统一到 v2.17.0，最低 Go 版本调整为 1.25。

## 仓库整理与提交前清理

项目提供 `scripts/cleanup_repo.py`，用于清理本地临时文件、诊断输出、Go 测试产物和已知构建缓存，帮助保持 Git 仓库整洁。脚本默认只预览，不会直接删除文件：

```powershell
python scripts/cleanup_repo.py
```

确认候选清单没有需要保留的文件后再执行实际清理：

```powershell
python scripts/cleanup_repo.py --apply
```

自动化环境可使用 `--apply --yes`。脚本只处理明确列出的临时文件和构建目录；不会删除 `mmsstv-master` 参考源码、`build/windows` 图标/清单、Go/前端源码、资源文件或 Git 已跟踪文件。`build/` 目录本身不会整体删除，只针对 `build/bin` 等生成目录。清理后可运行 `git status --short` 检查待提交变更。

注意：脚本只清理仓库目录内的文件，不会删除用户配置目录中的设置、模板、图库或 RX/TX 相册。

## Windows 构建

建议使用 Go 1.25 或更新版本、Windows WebView2 Runtime、Wails CLI v2.17.0，以及启用 CGO 的 MinGW-w64 GCC 工具链。较新的 Wails CLI 包含更新的 Go 包类型分析器，可规避旧版 CLI 在较新 Go 标准库上出现 `package "bufio" without types` 的问题。项目运行时模块也固定为 Wails v2.17.0，避免 CLI 与运行时版本错配。

在项目根目录，也就是包含 `wails.json` 和根目录 `main.go` 的目录执行：

```powershell
go version
go install github.com/wailsapp/wails/v2/cmd/wails@v2.17.0
wails version

$env:CGO_ENABLED = "1"
go mod tidy
go test ./...
wails build -clean -platform windows/amd64
```

需要确认 `gcc.exe` 位于 `PATH`：

```powershell
where.exe gcc
where.exe go
where.exe wails
```

GUI 输出位置：

```text
build\bin\SATSSTV-Go.exe
```

如果仅需构建命令行工具：

```powershell
go build -o SATSSTV-CLI.exe ./cmd/sstv-go
```

禁用 CGO 的构建可以运行编解码与控制逻辑测试，但实时音频设备访问会返回明确的 CGO 不可用提示。完整 GUI/音频构建请启用 CGO。

## 测试

不依赖本地音频硬件的核心测试：

```powershell
$env:CGO_ENABLED = "0"
go test ./internal/sstv ./internal/audio ./internal/radio/... ./cmd/sstv-go -count=1
go vet ./internal/... ./cmd/sstv-go
```

启用 CGO 并完成依赖下载后，执行全项目测试：

```powershell
go test ./...
```

## 项目结构

- `main.go`：Wails 入口、模式目录绑定、图像导入/导出及相册。
- `app_audio.go`：音频与 PTT 设置、RX/TX 生命周期、遥测和实时收发编排。
- `audio_runtime_cgo.go`：Malgo 实时音频设备实现。
- `audio_runtime_nocgo.go`：未启用 CGO 时的受限实现。
- `frontend/`：Wails HTML/CSS/JavaScript 界面，含接收频谱和历史瀑布图。
- `cmd/sstv-go/`：命令行入口。
- `internal/sstv/`：43 种模式、音频波形编码/解码、帧头识别和 WAV。
- `internal/audio/`：独立 RX/TX 流及全双工控制。
- `internal/radio/`：Hamlib rigctld、PTT 抽象和 Windows RTS/DTR。
- `settings.go`：本站身份信息持久化。

## Interface language and PTT test

The desktop interface can be switched between Simplified Chinese, English, and Japanese using the language selector in the top bar. The selection is stored locally and restored on the next launch.

For hardware PTT modes (serial RTS, serial DTR, or Hamlib Rigctld), open Device Settings and use **Test PTT** to key the configured endpoint. The test releases automatically after 15 seconds and can be stopped earlier with the same button. VOX mode does not need a software PTT test.

Settings are no longer dismissed by clicking the dialog backdrop or pressing Escape. Use **Save Settings** to persist and apply changes (the dialog closes after success), or the explicit close button to leave the dialog.

## Interface usability and localization

- UI labels and decorative text are non-selectable to prevent accidental text highlighting while operating the workbench. Input fields, text areas, and selectors remain editable and selectable.
- The interface language can be changed from the selector in the top bar: Simplified Chinese, English, or Japanese. The selection is persisted locally and restored on the next launch. RX/TX status, audio telemetry, progress labels, image-library actions, and template-editor controls follow the selected language.
- RTS, DTR, and Hamlib Rigctld settings include a **Test PTT** control. A test is released automatically after 15 seconds, and the same button can stop it earlier. The timeout is enforced in the Go backend, not only by the UI.
- The Settings dialog cannot be dismissed by clicking its backdrop or pressing Escape. **Save Settings** saves and applies the settings, then closes the dialog only when the operation succeeds. The explicit close button remains available.


### Default station identity

On first launch, the station callsign defaults to `N0CALL` and the Maidenhead locator defaults to `JJ00`. Existing empty station settings are migrated to these defaults. In TX templates, `{HISCALL}` displays `ToCall` until a callsign is entered in the HisCall field.
