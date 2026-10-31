# SATSSTV-Go

Go 语言 SSTV 编解码与 Wails Windows 桌面收发工作台。项目包含 43 种指定 SSTV 模式、图文模板编辑、实时音频 RX/TX、音频频谱、PTT 控制和历史相册。

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
- 发射结束后自动附加 MMSSTV 兼容 FSKID 呼号识别数据。接收端并行扫描 FSKID，在校验通过后单独显示识别到的呼号，不依赖从图像文字中 OCR 识别。
- FSKID 按 MMSSTV `fskid.txt` 原始规范传输：45.45 baud、6 bit 符号、B5 到 B0 顺序、`0x2A` 呼号起始标记、`0x01` 结束标记及字符 XOR 校验。普通模式前导音为 1500 Hz，窄带模式为 1900 Hz，随后为 2100 Hz 和 1900 Hz 起始位。当前实现仅发送呼号字段，不包含比赛编号扩展字段。

FSKID 兼容性依据：[MMSSTV 原始 FSKID.TXT](https://github.com/n5ac/mmsstv/blob/master/fskid.txt)。

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
