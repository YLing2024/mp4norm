[简体中文](README.md) ｜ [English](README.en.md)

# mp4norm

一句话：**mp4norm 不改画质，只重新整理 MP4 文件内部的“摆放方式”**，让视频能秒开、拖动进度条时更跟手。

它做的是**无损重写容器**（container，可以理解为“装音视频数据的盒子 / 文件外壳”）：把索引挪到文件开头、把音视频数据交叉排布、清理历史遗留的编辑表。只有极少数文件才需要真的重新压一遍。

## 它解决什么问题

很多 MP4 文件“包装得有问题”，不播放的时候看不出来：

- `moov`（索引，记录每个数据块在文件里的位置）落在文件**末尾** → 渐进式 / 边下边播必须几乎读完整个文件才能出第一帧；
- 音视频**未交织**（interleave，指音频和视频数据交叉排布，而不是各存一大段）→ 拖动时播放器要在文件里来回跳；
- 关键帧（keyframe，可独立解码、用作拖动落点的帧）稀疏或不规整，`elst` 编辑表损坏，`timescale`（时间刻度，即帧时间戳的单位）不一致 → 拖动缓慢或直接失效。

编码数据本身通常是好的，需要修的是**容器布局**。

## 它做什么

`mp4norm` **无损重写容器**（stream copy，直接复制已编码的数据，不重新编码，画质零损失）：

1. 把 `moov` 移到文件开头（**faststart**，俗称“秒开”）。
2. 把音视频重新交织成小的时间片（约 0.5–1.0 秒）。
3. 清理 edit list / timescale 不一致，并重新计算数据块偏移（`stco`/`co64`）。
4. 输出二选一：**普通 progressive faststart MP4**，或带 `sidx` 的**分片 MP4（fMP4）**。
5. 校验结果（moov 位置、交织偏差、关键帧间隔）。

`probe` 检查会给出分级诊断（findings）：`critical` / `warn` / `info`，例如 `moov-at-end`（索引在尾部）、`fragmented`（分片文件）、`no-ftyp`（未声明文件类型），以及文件尾部截断或残留字节的 `truncated-box` 提醒。

当源文件无法靠“改容器”修好时（关键帧过于稀疏、非标准编码、VFR 可变帧率），提供一条**可选的重编码**路径，交给 `ffmpeg` 处理，并优先使用硬件编码器（NVENC / QSV / AMF / VideoToolbox）。

## 技术栈

| 层 | 选择 |
| --- | --- |
| 语言 | Go |
| 容器引擎 | [`Eyevinn/mp4ff`](https://github.com/Eyevinn/mp4ff)（MIT） |
| 重编码（可选） | `ffmpeg` 子进程，优先硬件编码器 |
| 图形界面 | [Wails](https://wails.io)（Go 后端 + Web 前端） |
| 平台 | Windows、macOS、Linux |

## 性能原则

- 无损路径是 **I/O 瓶颈**：单遍处理、大缓冲 / mmap、绝不把整个文件读进内存。
- 无损路径不做临时文件来回搬移。
- 多文件并发处理。
- 重编码路径使用硬件编码器。

## 实测数据

测试环境：Windows 11 · Go 1.27 · H 盘（7200rpm 级机械盘）· 测量日期 2026-10-01

| 场景 | 结果 |
|---|---|
| 1.2 GB 真实视频（索引在文件尾）规整 | **15.9 秒** |
| 同一文件「替换原文件」模式（含备份） | **23.7 秒** |
| 规整 1.2 GB 文件时的内存峰值 | **67 MB**（不把整个文件读入内存） |
| 首次可播放需读取的数据量 | **1.2 GiB → 5.7 MiB** |
| 扫描 30 个真实视频（191 MB ~ 1811 MB） | **0.3 秒**（只读文件头尾） |
| 40 KB 小文件 | 瞬时（毫秒级） |

对照：同一台机器上用 `ffmpeg -c copy +faststart` 处理 800 MB 怪封装文件，
写入约 18 秒、moov 重排再花约 36 秒（合计约 54 秒）——
mp4norm 走容器手术路径，1.2 GB 只要约 16 秒。

## 用法

CLI：

```
mp4norm probe <file>                 # 诊断容器布局
mp4norm normalize <input>            # faststart + 交织（无损）
mp4norm normalize -format fmp4 <in>  # 带 sidx 的分片 MP4（无损）
mp4norm faststart <input>            # 只把 moov 移到文件开头
mp4norm reencode -crf 23 <input>     # 可选重编码（修关键帧稀疏 / VFR）
mp4norm tmp <目录...>                # 列出中断残留的临时文件（`.mp4norm-tmp-*`）及大小
```

常用 flag：`-o <out>` 输出路径、`-window <ms>` 交织窗口、`-frag-ms <ms>` 分片时长、`-vcodec h264|h265|copy`、`-hw auto|on|off`、`-gop <sec>` 关键帧间隔。

规整过程中若进程被强杀（任务管理器结束进程、关窗口强退），可能留下 `.mp4norm-tmp-*` 临时文件。下一次规整开始前会自动清理目标目录中 **mtime 超过 60 分钟** 的陈旧残留并打印日志（正在运行的其它实例的临时文件较新，不会被误删）；`mp4norm tmp <目录>` 可随时查看残留。

批量检测（推荐入口）—— 扫描一个文件夹，直接告诉你**每个文件要不要处理**：

```
mp4norm scan <目录|文件...>        # 人话表格：需要处理 / 无法处理 / 无需处理
mp4norm scan <目录> --needs-work   # 只列出需要处理的（脚本化用）
mp4norm scan <目录> --json         # 机器可读
mp4norm scan <目录> -v             # 附带原始字段（brand / moov 位置 / mdat 块数）
```

结论只有三种：

- ✅ **无需处理** —— 索引已在文件开头、数据块完整、没有异常
- ⚠️ **建议规整** —— 索引在文件尾部，或数据碎片化（数据块过多），或音视频未交织
- ❌ **无法处理** —— 缺少索引或文件损坏

退出码：`0` 全部无需处理 / `1` 存在建议规整 / `2` 存在无法处理，方便脚本化。

`batch` 可以并行处理多个文件或目录：

```
mp4norm batch [-jobs n] [-format progressive|fmp4] [-outdir dir] [-suffix s] <input...|dir...>
```

`watch` 轮询目录，等文件**停止写入后**再规整：

```
mp4norm watch [-interval 5m] [-outdir dir] [-in-place] [-backup-dir dir] [-once] [-quiet] <目录...>
```

**第一轮只做基线观察**：记录每个候选文件（`.mp4`/`.m4v`/`.mov`）的大小与修改时间后跳过，正在下载 / 录制中的文件不会被误判为已完成；从第二轮起才开始分类与处理。仍在变化的文件会记为 `writing`，**绝不会记为失败**——只有真正稳定、确实损坏的文件才会记失败。`-once` 只跑一轮观察即退出（沿用 scan 的退出码 `0`/`1`/`2`），需要运行两次才能「先观察、再处理」。

桌面 GUI（Wails）—— **批量检测与批量规整是主要入口**：

```
cd gui && wails build                          # 产物在 build/bin/mp4norm
./build/bin/mp4norm.exe "D:\你的视频目录"       # 也可直接带目录/文件启动，自动检测
```

- 「选择文件夹…」或「选择文件…」（支持多选），也可把文件 / 文件夹**直接拖进窗口** → 列表逐行给出**结论与原因**，可按结论筛选、按大小排序
- 「修复这 N 个文件」一键处理所有需要处理的文件，实时显示 `正在修复 N/M：文件名` 与每个文件状态，结束给出成功 / 跳过 / 失败汇总；失败行旁有「重试」，完成后结论与按钮计数自动刷新
- 原始字段（brand / moov 位置 / mdat 块数）收在每行的「详情」里，不再糊在主界面
- 单文件流程（选择单个文件 → 检查 → 规整 / 重新编码）默认收起为「单文件处理」，需要时再展开
- 重新编码是可选的高级路径：**多数情况用不到**，只有拖动卡顿、帧率异常时才需要；它会重新压一遍、画质有损

### 界面语言（GUI 双语）

GUI 界面**默认简体中文**。窗口右上角有 `中文 / English` 切换按钮，点击后界面立即切换；**选择会被记住**（写入浏览器本地存储 `localStorage`，键名 `mp4norm.lang`），下次打开沿用上次的语言。打开 / 保存文件的系统对话框标题，以及 `probe` 诊断结果的说明文字，也会跟随当前语言。界面上每个参数只留一行简短提示，完整解释收在参数旁的 **ⓘ** 里，悬停或点击才展开。

## 打包 ffmpeg

可选的重编码路径依赖外部 ffmpeg。`mp4norm` 按以下顺序查找：`MP4NORM_FFMPEG` → 可执行文件旁边或工作目录下的 `ffmpeg/` 或 `third_party/ffmpeg/` → `PATH`。

把静态构建拉取到 `third_party/ffmpeg/bin`：

```
make fetch-ffmpeg   # 或 scripts/fetch-ffmpeg.ps1 / scripts/fetch-ffmpeg.sh
```

注意：拉取的构建是 GPL 的。再分发它们会让整个 bundle 受 GPL 条款约束；如果需要规避，请附带 LGPL 构建，或要求用户自行提供 ffmpeg。

## 开发

```
make build   # CLI -> bin/mp4norm
make test    # go test ./...
make vet
make fmt
make bench   # 无损路径基准测试
make gui     # Wails 桌面应用 -> gui/build/bin
```

CI（`.github/workflows/ci.yml`）在 Windows、macOS、Linux 上运行 vet、测试和 CLI 构建，并在三个平台分别构建 GUI。推送 `v*` tag 会发布 5 个平台的 CLI 二进制：linux/amd64、linux/arm64、darwin/amd64、darwin/arm64、windows/amd64。

## 状态

已可用：probe、无损 faststart、交织、fMP4 + sidx、带硬件编码器探测的可选 ffmpeg 重编码、并行 batch CLI，以及 Wails 桌面界面（支持中英文切换）——并配有跨平台 CI 和 tag 触发的 release 工作流。

## 许可证

MIT —— 见 [LICENSE](LICENSE)。

注意：拉取的 `ffmpeg` 构建是 GPL 的。再分发它们会让整个 bundle 受 GPL 条款约束；如果需要规避，请附带 LGPL 构建，或要求用户自行提供 ffmpeg。
