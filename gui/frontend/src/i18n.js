// i18n.js — bilingual strings (zh default / en) and language persistence for the
// mp4norm GUI. Static copy mirrors the spec's copy table exactly.

const STORAGE_KEY = 'mp4norm.lang';
const DEFAULT_LANG = 'zh';
const LANGS = ['zh', 'en'];

const STRINGS = {
  zh: {
    'app.name': 'mp4norm',

    // Header / file selection
    'ffmpeg.checking': '正在检测 ffmpeg…',
    'ffmpeg.missing': '未找到 ffmpeg',
    'input.placeholder': '选择 MP4 文件…',
    'btn.choose': '选择文件…',
    'btn.inspect': '检查',
    'card.diagnosis': '诊断结果',
    'info.aria': '详细说明',
    'single.card': '单文件处理',

    // Normalize card
    'card.normalize': '无损规整',
    'label.input': '视频文件',
    'label.format': '输出格式',
    'opt.progressive': '普通 MP4（最通用）',
    'opt.fmp4': '分片 MP4（适合边下边播）',
    'hint.format': '普通 MP4 最通用；分片 MP4 适合边下边播',
    'info.format':
      '普通 MP4：最通用，本地播放、网页播放都合适。分片 MP4：适合边下边播、直播类场景。',
    'label.output': '输出路径',
    'label.window': '交织窗口（毫秒）',
    'hint.window': '音频与视频交织的时间粒度，不确定就保持 1000',
    'info.window':
      '音频与视频数据交叉存放的时间粒度。调小 → 拖动更顺、文件略大；调大 → 相反。不确定就保持 1000（1 秒）。',
    'label.frag': '分片时长（毫秒）',
    'hint.frag': '分片 MP4 每片时长，不确定就保持 2000',
    'info.frag': '每个分片装多少秒内容，只在选了「分片 MP4」时生效。不确定就保持 2000。',
    'advanced.norm': '高级选项（一般不用改）',
    'out.norm.placeholder': '输出路径（默认：<输入>.norm.mp4）',
    'btn.saveas': '另存为…',
    'btn.normalize': '开始规整',

    // Re-encode card
    'card.reencode': '重新编码（可选 · 高级）',
    'card.reencode.sub': '重新压一遍，画质有损',
    'label.codec': '视频编码',
    'opt.codec.copy': '直接复制（copy）',
    'hint.codec': 'H.264 兼容最好，copy 只换容器、不重编码',
    'info.codec':
      'H.264 兼容性最好；H.265 体积更小但老设备可能播不了；copy 表示只换容器、不重新编码。',
    'label.hw': '硬件加速',
    'opt.hw.auto': '自动',
    'opt.hw.on': '开启',
    'opt.hw.off': '关闭',
    'hint.hw': '自动优先用显卡编码（更快）',
    'info.hw': '自动 = 优先用显卡编码（快很多）。要画质或显卡不兼容时选「关闭」。',
    'label.crf': 'CRF（画质档位）',
    'hint.crf': '数值越小越清晰、文件越大，推荐 23',
    'info.crf': '画质档位：数值越小越清晰、文件越大。推荐 23（18 接近无损）。',
    'label.preset': '编码预设',
    'hint.preset': '速度与体积的取舍，medium 均衡',
    'info.preset': '速度与体积的取舍：ultrafast 最快但文件最大 → slow 最慢但最小。',
    'label.gop': '关键帧间隔（GOP·秒）',
    'hint.gop': '关键帧间隔，推荐 2 秒',
    'info.gop': '每隔多久插入一个「可跳转点」。调小 → 拖动更精准、文件略大。推荐 2 秒。',
    'out.reenc.placeholder': '输出路径（默认：<输入>.reenc.mp4）',
    'btn.reencode': '开始重编码',
    'card.log': '日志',

    // Dynamic log
    'log.ready': '就绪',
    'log.selected': '已选择 {path}',
    'log.noFile': '尚未选择文件',
    'log.inspected': '检查完成',
    'log.inspectFailed': '检查失败：{err}',
    'log.normalizing': '正在规整…',
    'log.normalizeFailed': '规整失败：{err}',
    'log.reencoding': '正在重新编码…',
    'log.reencodeFailed': '重新编码失败：{err}',
    'log.done': '完成 → {path}',

    // Batch list (the default entry point)
    'batch.card': '检查并修复',
    'btn.chooseFiles': '选择文件…',
    'btn.chooseFolder': '选择文件夹…',
    'btn.clear': '清空列表',
    'btn.scan': '重新检测',
    'btn.batchNormalize': '批量规整',
    'btn.batchNormalize.count': '修复这 {n} 个文件',
    'btn.cancel': '中止',
    'btn.retry': '重试',
    'btn.chooseOutdir': '选择…',
    'batch.drop': '拖入文件或文件夹，或点上方按钮选择',
    'batch.filter': '筛选',
    'batch.filter.all': '全部',
    'batch.filter.needs': '仅需处理',
    'batch.filter.broken': '仅无法处理',
    'batch.sortSize': '按大小排序',
    'batch.count': '共 {total} 个文件 · {needs} 个需要修复',
    'batch.col.file': '文件名',
    'batch.col.size': '大小',
    'batch.col.verdict': '结论',
    'batch.col.reason': '原因',
    'batch.col.status': '状态',
    'batch.details': '详情',
    'batch.outdir': '输出目录',
    'batch.outdir.placeholder': '默认：与原文件同目录',
    'batch.detail.path': '完整路径',
    'batch.detail.rawError': '原始错误',
    // Plain-language batch failure reasons. The raw error is kept in the
    // detail block (batch.detail.rawError) for troubleshooting.
    'batch.fail.reason.structure':
      '失败：文件的数据块结构异常，无法安全规整（可尝试用重新编码处理）',
    'batch.fail.reason.unreadable': '失败：文件无法读取（可能已被移动或删除）',
    'batch.fail.reason.moov-missing': '失败：文件缺少索引（moov），无法处理',
    'batch.fail.reason.unknown': '失败：{err}',
    'batch.progress': '已完成 {done}/{total}',
    'batch.progress.current': '正在修复 {index}/{total}：{name}',
    'batch.summary': '成功 {ok} / 跳过 {skip} / 失败 {fail}',
    'batch.summary.cancelled': '已中止：成功 {ok} / 跳过 {skip} / 失败 {fail}',
    'batch.nothing': '没有需要处理的文件',
    'batch.status.queued': '排队中',
    'batch.status.running': '正在修复',
    'batch.status.done': '完成',
    'batch.status.failed': '失败',
    'batch.status.skipped': '跳过',
    'status.ok': '✅ 无需处理',
    'status.needs_work': '⚠️ 建议规整',
    'status.broken': '❌ 无法处理',
    'reason.moov-at-end': '索引在文件尾部：首次播放要读完整文件（约 {size}）才能出画面',
    'reason.mdat-fragmented': '数据碎片化（{n} 个数据块）：拖动进度条时播放器要在文件里来回跳',
    'reason.not-interleaved': '音视频未交织：声音和画面数据分开存，拖动卡顿',
    'reason.moov-missing': '未找到索引（moov 缺失），文件可能损坏',
    'reason.truncated-box': '文件被截断：最后一个数据块不完整，可能没下载完',
    'reason.not-mp4': '不是有效的 MP4 文件',
    'reason.scan-failed': '无法读取文件',
    'reason.no-ftyp': '未找到 ftyp 块：文件类型未声明',
    'reason.fragmented': '分片 MP4（存在 moof 块）',
    'reason.ok': '已规范',
    'log.scanning': '正在检测 {n} 个文件…',
    'log.scanned': '检测完成：{ok} 无需处理 / {needs} 建议规整 / {broken} 无法处理',
    'log.scanFailed': '检测失败：{err}',
    'log.cleared': '已清空列表',
    'log.batchStart': '开始批量规整（{n} 个）…',
    'log.batchDone': '批量规整结束：成功 {ok} / 失败 {fail}',
    'log.batchRefreshed': '已重新检测处理结果，刷新结论与按钮计数',
    'log.batchCancelled': '批量规整已中止',
    'log.batchFailed': '批量规整失败：{err}',
    'log.retry': '重试 {name}…',
    'log.retryDone': '重试完成 → {name}',
    'log.retryFailed': '重试失败：{err}',

    // Output mode (new file vs replace-in-place) and the backup manager.
    'batch.mode.label': '输出方式',
    'batch.mode.new': '生成新文件（推荐）',
    'batch.mode.inplace': '替换原文件（自动备份）',
    'batch.mode.hint': '替换模式会先自动备份原文件',
    'batch.inplace.warning':
      '⚠️ 将直接替换原文件。原文件会先自动备份到「{dir}」，可在「备份管理」里恢复。',
    'batch.inplace.defaultDir': '各文件旁的 .mp4norm-backup',
    'batch.inplace.backupdir': '备份目录',
    'batch.inplace.backupdir.placeholder': '留空则备份到每个文件旁边的 .mp4norm-backup',
    'backup.card': '备份管理',
    'backup.card.sub': '查看、恢复或删除「替换原文件」时自动产生的备份',
    'backup.dir': '备份目录',
    'backup.dir.placeholder': '媒体文件夹（或 .mp4norm-backup）',
    'backup.dir.hint':
      '选一个媒体文件夹，会自动查找它下面的 .mp4norm-backup；也可直接选择备份目录本身',
    'btn.chooseBackupDir': '选择…',
    'btn.listBackups': '列出备份',
    'btn.restore': '恢复',
    'btn.deleteBackup': '删除备份',
    'btn.confirmDelete': '确认删除',
    'backup.empty': '没有找到备份。',
    'backup.resolved': '备份目录：{dir}',
    'backup.col.original': '原文件',
    'backup.col.backup': '备份文件',
    'backup.col.size': '大小',
    'backup.col.time': '备份时间',
    'backup.col.state': '原文件状态',
    'backup.col.actions': '操作',
    'backup.state.exists': '原文件存在',
    'backup.state.missing': '原文件缺失',
    'backup.state.backupMissing': '备份文件缺失',
    'backup.deleteConfirm': '将删除该文件的 {n} 份备份，再次点击确认',
    'backup.deleted': '已删除 {n} 份备份。',
    'backup.restored': '已恢复 {path}',
    'backup.restoreNote': '被覆盖的当前版本已另存为备份 {name}',
    'backup.failed': '备份操作失败：{err}',
    'log.backupListed': '备份列表已更新：{n} 份',

    // Before/after benefit numbers.
    'gain.title': '✅ 已完成（无损，画质不变）',
    'gain.firstplay': '首次可播放需读取：约 {before} → 约 {after}',
    'gain.mdat': '数据碎片：{before} 块 → {after} 块',
    'gain.segments': '分片：{after} 段',
    'gain.size': '体积：{before} → {after}',
    'gain.summary': '共 {n} 个文件，合计“首次可播放需读取”从约 {before} 降到约 {after}',

    // Report
    'report.file': '文件',
    'report.size': '大小',
    'report.brand': '封装品牌',
    'report.moov': 'moov 位置',
    'report.fragmented': '分片',
    'report.mdatCount': 'mdat 块数',
    'report.moov.front': '文件头',
    'report.moov.end': '文件尾',
    'report.moov.unknown': '未知',
    'report.bool.yes': '是',
    'report.bool.no': '否',
  },

  en: {
    'app.name': 'mp4norm',

    // Header / file selection
    'ffmpeg.checking': 'checking ffmpeg…',
    'ffmpeg.missing': 'ffmpeg not found',
    'input.placeholder': 'choose an MP4 file…',
    'btn.choose': 'Choose file…',
    'btn.inspect': 'Inspect',
    'card.diagnosis': 'Diagnosis',
    'info.aria': 'Details',
    'single.card': 'Single file',

    // Normalize card
    'card.normalize': 'Normalize',
    'label.input': 'Video file',
    'label.format': 'Format',
    'opt.progressive': 'Progressive (plays anywhere)',
    'opt.fmp4': 'Fragmented (for streaming)',
    'hint.format': 'Progressive plays anywhere; Fragmented suits streaming',
    'info.format':
      'Progressive: plays anywhere, local or web. Fragmented: for streaming / live-style delivery.',
    'label.output': 'Output',
    'label.window': 'Interleave window (ms)',
    'hint.window': 'Audio/video interleave granularity; keep 1000 if unsure',
    'info.window':
      'How finely audio and video are interleaved. Smaller → smoother seeking, slightly larger file. Keep 1000 (1s) if unsure.',
    'label.frag': 'Fragment length (ms)',
    'hint.frag': 'Length of each fragment; keep 2000 if unsure',
    'info.frag': 'Seconds per fragment; only used for Fragmented MP4. Keep 2000 if unsure.',
    'advanced.norm': 'Advanced (usually not needed)',
    'out.norm.placeholder': 'output (default: <input>.norm.mp4)',
    'btn.saveas': 'Save as…',
    'btn.normalize': 'Normalize',

    // Re-encode card
    'card.reencode': 'Re-encode (optional · advanced)',
    'card.reencode.sub': 'Re-encodes (lossy)',
    'label.codec': 'Codec',
    'opt.codec.copy': 'copy',
    'hint.codec': 'H.264 plays everywhere; copy only remuxes',
    'info.codec':
      'H.264 plays everywhere; H.265 is smaller but older devices may not decode it; copy only remuxes.',
    'label.hw': 'Hardware acceleration',
    'opt.hw.auto': 'auto',
    'opt.hw.on': 'on',
    'opt.hw.off': 'off',
    'hint.hw': 'auto prefers the GPU encoder (faster)',
    'info.hw': 'auto prefers the GPU encoder (much faster); choose off for best quality or if unsupported.',
    'label.crf': 'CRF (quality)',
    'hint.crf': 'Lower = sharper and larger; 23 is a good default',
    'info.crf': 'Quality dial: lower = sharper and larger. 23 is a good default (~18 ≈ lossless).',
    'label.preset': 'Preset',
    'hint.preset': 'Speed vs size; medium is balanced',
    'info.preset': 'Speed vs size trade-off: ultrafast is fastest and largest → slow is smallest.',
    'label.gop': 'Keyframe / GOP (s)',
    'hint.gop': 'Keyframe interval; 2s is fine',
    'info.gop':
      'How often a seekable keyframe is inserted. Smaller → more precise scrubbing, slightly larger. 2s is fine.',
    'out.reenc.placeholder': 'output (default: <input>.reenc.mp4)',
    'btn.reencode': 'Re-encode',
    'card.log': 'Log',

    // Dynamic log
    'log.ready': 'ready',
    'log.selected': 'selected {path}',
    'log.noFile': 'no file selected',
    'log.inspected': 'inspected',
    'log.inspectFailed': 'inspect failed: {err}',
    'log.normalizing': 'normalizing…',
    'log.normalizeFailed': 'normalize failed: {err}',
    'log.reencoding': 're-encoding…',
    'log.reencodeFailed': 're-encode failed: {err}',
    'log.done': 'done -> {path}',

    // Batch list (the default entry point)
    'batch.card': 'Check & fix',
    'btn.chooseFiles': 'Choose files…',
    'btn.chooseFolder': 'Choose folder…',
    'btn.clear': 'Clear list',
    'btn.scan': 'Re-scan',
    'btn.batchNormalize': 'Normalize all',
    'btn.batchNormalize.count': 'Fix {n} file(s)',
    'btn.cancel': 'Stop',
    'btn.retry': 'Retry',
    'btn.chooseOutdir': 'Choose…',
    'batch.drop': 'Drop files or folders here, or use the buttons above',
    'batch.filter': 'Filter',
    'batch.filter.all': 'All',
    'batch.filter.needs': 'Needs work',
    'batch.filter.broken': 'Broken',
    'batch.sortSize': 'Sort by size',
    'batch.count': '{total} file(s) · {needs} need fixing',
    'batch.col.file': 'File',
    'batch.col.size': 'Size',
    'batch.col.verdict': 'Verdict',
    'batch.col.reason': 'Reason',
    'batch.col.status': 'Status',
    'batch.details': 'Details',
    'batch.outdir': 'Output folder',
    'batch.outdir.placeholder': 'Default: same folder as the source file',
    'batch.detail.path': 'Full path',
    'batch.detail.rawError': 'Raw error',
    // Plain-language batch failure reasons. The raw error is kept in the
    // detail block (batch.detail.rawError) for troubleshooting.
    'batch.fail.reason.structure':
      'Failed: the file’s data-block structure is unusual and cannot be normalized safely (try re-encoding)',
    'batch.fail.reason.unreadable': 'Failed: the file could not be read (it may have been moved or deleted)',
    'batch.fail.reason.moov-missing': 'Failed: the file has no index (moov) and cannot be processed',
    'batch.fail.reason.unknown': 'Failed: {err}',
    'batch.progress': '{done}/{total} done',
    'batch.progress.current': 'Fixing {index}/{total}: {name}',
    'batch.summary': '{ok} succeeded / {skip} skipped / {fail} failed',
    'batch.summary.cancelled': 'Stopped: {ok} succeeded / {skip} skipped / {fail} failed',
    'batch.nothing': 'Nothing needs work',
    'batch.status.queued': 'queued',
    'batch.status.running': 'fixing',
    'batch.status.done': 'done',
    'batch.status.failed': 'failed',
    'batch.status.skipped': 'skipped',
    'status.ok': '✅ OK',
    'status.needs_work': '⚠️ Needs work',
    'status.broken': '❌ Cannot process',
    'reason.moov-at-end': 'Index at the end: the first frame needs the whole file (~{size})',
    'reason.mdat-fragmented': 'Fragmented into {n} data blocks: seeking jumps back and forth',
    'reason.not-interleaved': 'Audio and video are not interleaved',
    'reason.moov-missing': 'No index (moov) found; the file is probably damaged',
    'reason.truncated-box': 'File is truncated: the final data block is incomplete',
    'reason.not-mp4': 'Not a valid MP4 file',
    'reason.scan-failed': 'Could not read the file',
    'reason.no-ftyp': 'No ftyp box found; the file type is undeclared',
    'reason.fragmented': 'Fragmented MP4 (moof boxes present)',
    'reason.ok': 'Already normalized',
    'log.scanning': 'scanning {n} file(s)…',
    'log.scanned': 'scan done: {ok} OK / {needs} need work / {broken} broken',
    'log.scanFailed': 'scan failed: {err}',
    'log.cleared': 'list cleared',
    'log.batchStart': 'normalizing {n} file(s)…',
    'log.batchDone': 'batch done: {ok} succeeded / {fail} failed',
    'log.batchRefreshed': 're-checked the results and refreshed the verdicts and button count',
    'log.batchCancelled': 'batch stopped',
    'log.batchFailed': 'batch failed: {err}',
    'log.retry': 'retrying {name}…',
    'log.retryDone': 'retry done -> {name}',
    'log.retryFailed': 'retry failed: {err}',

    // Output mode (new file vs replace-in-place) and the backup manager.
    'batch.mode.label': 'Output mode',
    'batch.mode.new': 'New file (recommended)',
    'batch.mode.inplace': 'Replace originals (auto backup)',
    'batch.mode.hint': 'Replace mode backs up originals first',
    'batch.inplace.warning':
      '⚠️ Original files will be replaced. Each source is backed up first to “{dir}”; restore it from “Backup management”.',
    'batch.inplace.defaultDir': '.mp4norm-backup beside each file',
    'batch.inplace.backupdir': 'Backup folder',
    'batch.inplace.backupdir.placeholder': 'Leave empty to store backups in .mp4norm-backup beside each file',
    'backup.card': 'Backup management',
    'backup.card.sub': 'Review, restore or delete the backups made by replace-in-place',
    'backup.dir': 'Backup folder',
    'backup.dir.placeholder': 'media folder (or .mp4norm-backup)',
    'backup.dir.hint':
      'Pick a media folder to find its .mp4norm-backup, or pick the backup folder itself',
    'btn.chooseBackupDir': 'Choose…',
    'btn.listBackups': 'List backups',
    'btn.restore': 'Restore',
    'btn.deleteBackup': 'Delete',
    'btn.confirmDelete': 'Confirm delete',
    'backup.empty': 'No backups found.',
    'backup.resolved': 'Backup folder: {dir}',
    'backup.col.original': 'Original',
    'backup.col.backup': 'Backup',
    'backup.col.size': 'Size',
    'backup.col.time': 'Backed up',
    'backup.col.state': 'Original state',
    'backup.col.actions': 'Actions',
    'backup.state.exists': 'Original exists',
    'backup.state.missing': 'Original missing',
    'backup.state.backupMissing': 'backup file missing',
    'backup.deleteConfirm': 'This deletes all {n} backup(s) of the file — click again to confirm',
    'backup.deleted': 'Deleted {n} backup(s).',
    'backup.restored': 'Restored {path}',
    'backup.restoreNote': 'The replaced version was saved as backup {name}',
    'backup.failed': 'Backup operation failed: {err}',
    'log.backupListed': 'backup list refreshed: {n}',

    // Before/after benefit numbers.
    'gain.title': '✅ Done (lossless, no quality change)',
    'gain.firstplay': 'Bytes before first frame: ~{before} → ~{after}',
    'gain.mdat': 'Data fragments: {before} → {after}',
    'gain.segments': 'Fragments: {after}',
    'gain.size': 'Size: {before} → {after}',
    'gain.summary': '{n} file(s): bytes before first frame went from ~{before} down to ~{after}',

    // Report
    'report.file': 'file',
    'report.size': 'size',
    'report.brand': 'brand',
    'report.moov': 'moov',
    'report.fragmented': 'fragmented',
    'report.mdatCount': 'mdat boxes',
    'report.moov.front': 'front',
    'report.moov.end': 'end',
    'report.moov.unknown': 'unknown',
    'report.bool.yes': 'true',
    'report.bool.no': 'false',
  },
};

// Chinese explanations for every probe finding Code in internal/probe/probe.go.
// English keeps the Message emitted by the probe; an unlisted Code also falls
// back to the Message so we never render an empty string or a bare key.
const FINDINGS_ZH = {
  'moov-missing': '未找到 moov 原子：文件元数据缺失，无法播放',
  'moov-at-end':
    'moov 位于文件尾部：未做 faststart，渐进播放/流式播放必须读到文件末尾才能出第一帧',
  'fragmented': '分片 MP4（存在 moof 块）：能否快速定位取决于播放器对 sidx 的支持',
  'no-ftyp': '未找到 ftyp 块：文件类型未声明',
};

function readLang() {
  try {
    const saved = localStorage.getItem(STORAGE_KEY);
    if (LANGS.includes(saved)) return saved;
  } catch (_) {
    // localStorage may be unavailable; fall through to the default.
  }
  return DEFAULT_LANG;
}

let current = readLang();

/** getLang returns the active language code ("zh" or "en"). */
export function getLang() {
  return current;
}

/** setLang persists and activates a language, returning the active code. */
export function setLang(lang) {
  if (!LANGS.includes(lang)) return current;
  current = lang;
  try {
    localStorage.setItem(STORAGE_KEY, lang);
  } catch (_) {
    // Ignore storage failures; the in-memory selection still applies.
  }
  return current;
}

/** t looks up a key in the active pack and substitutes {name} placeholders. */
export function t(key, vars) {
  const pack = STRINGS[current] || STRINGS[DEFAULT_LANG];
  let s = pack[key];
  if (s === undefined) s = STRINGS[DEFAULT_LANG][key];
  if (s === undefined) s = key;
  if (vars) {
    for (const name of Object.keys(vars)) {
      s = s.split(`{${name}}`).join(String(vars[name]));
    }
  }
  return s;
}

/** findingMessage localizes a probe finding by Code, falling back to Message. */
export function findingMessage(finding) {
  if (!finding) return '';
  if (current === 'en') return finding.Message;
  return FINDINGS_ZH[finding.Code] || finding.Message;
}
