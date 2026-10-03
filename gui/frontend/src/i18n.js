// i18n.js — strings and language persistence for the mp4norm GUI.
//
// There is no "single file" vs "batch" vocabulary anywhere: N files are just a
// list with N rows, and the copy never says "mode". Every visible state pairs a
// colour with text (see style.css), so the wording here always names the state.

const STORAGE_KEY = 'mp4norm.lang';
const DEFAULT_LANG = 'zh';

// LANGUAGES drives the header selector. label is shown as-is, never translated.
export const LANGUAGES = [
  { code: 'zh', label: '中文' },
  { code: 'zh_Hant', label: '繁體中文' },
  { code: 'en', label: 'English' },
  { code: 'ja', label: '日本語' },
  { code: 'ko', label: '한국어' },
];
const LANGS = LANGUAGES.map((l) => l.code);

const STRINGS = {
  zh: {
    'app.name': 'mp4norm',
    'info.aria': '详细说明',
    'lang.aria': '语言',

    // Header / ffmpeg
    'ffmpeg.checking': '正在检测 ffmpeg…',
    'ffmpeg.missing': '未找到 ffmpeg',

    // Entry point
    'input.drop': '拖入视频或文件夹，或点下方按钮（可多选、可多次添加）',
    'btn.chooseFiles': '选择文件…',
    'btn.chooseFolder': '选择文件夹…',
    'btn.rescan': '重新检测',
    'btn.clear': '清空列表',
    'btn.chooseDir': '选择…',

    // The list
    'list.count': '共 {total} 个视频 · 已选 {selected} 个',
    'list.expand': '展开',
    'list.collapse': '收起',
    'list.select': '选择该文件',

    // Per-file states. Colour is applied by tone; the text always names it.
    'file.waiting': '等待检查',
    'file.checking': '检查中…',
    'file.ok': '无需处理',
    'file.needs': '需要处理',
    'file.broken': '无法处理',
    'file.queued': '排队中',
    'file.queued.pos': '排队中（第 {pos} 位）',
    'file.running': '修复中…',
    'file.done': '已完成',
    'file.failed': '失败',
    'file.skipped': '无需处理',

    // Processing settings
    'settings.process': '处理方式',
    'settings.remux': '重排封装（无损、快）',
    'settings.reencode': '重编码（改变编码/体积，慢）',
    'label.format': '输出格式',
    'opt.progressive': '普通 MP4（最通用）',
    'opt.fmp4': '分片 MP4（适合边下边播）',
    'hint.format': '普通 MP4 最通用；分片 MP4 适合边下边播',
    'info.format':
      '普通 MP4：最通用，本地播放、网页播放都合适。分片 MP4：适合边下边播、直播类场景。',
    'advanced.norm': '高级选项（一般不用改）',
    'label.window': '交织窗口（毫秒）',
    'hint.window': '音频与视频交织的时间粒度，不确定就保持 1000',
    'info.window':
      '音频与视频数据交叉存放的时间粒度。调小 → 拖动更顺、文件略大；调大 → 相反。不确定就保持 1000（1 秒）。',
    'label.frag': '分片时长（毫秒）',
    'hint.frag': '分片 MP4 每片时长，不确定就保持 2000',
    'info.frag': '每个分片装多少秒内容，只在选了「分片 MP4」时生效。不确定就保持 2000。',
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

    // Output
    'output.label': '输出方式',
    'output.inplace': '原地替换（自动备份）',
    'output.new': '输出到目录',
    'output.inplace.hint': '替换原文件前会先自动备份。',
    'output.inplace.warning':
      '⚠️ 将直接替换原文件。原文件会先自动备份到「{dir}」，可在下方「备份管理」里恢复。',
    'output.inplace.defaultDir': '各文件旁的 .mp4norm-backup',
    'output.backupdir': '备份目录',
    'output.backupdir.placeholder': '留空则备份到每个文件旁边的 .mp4norm-backup',
    'output.outdir': '输出目录',
    'output.outdir.placeholder': '留空则写到每个文件旁边',
    'output.namerule': '文件名规则',
    'output.namerule.suffix': '保留原名 + 后缀',
    'output.namerule.keep': '保留原名',

    // Primary + row actions
    'btn.fix': '修复这 {n} 个文件',
    'btn.cancel': '中止',
    'btn.retry': '重试',
    'btn.retryFailed': '重试失败的 {n} 个',
    'btn.undo': '撤销这次修复',

    // Global status
    'scan.finding': '正在查找视频… 已找到 {n} 个',
    'scan.checking': '正在检查… 已找到 {n} 个',
    'progress.current': '正在修复 {index}/{total} · {name}',
    'progress.done': '已完成 {done}/{total}',
    'summary.done': '完成 · 成功 {ok} · 无需处理 {need} · 失败 {fail} · {saved}',
    'summary.cancelled': '已中止 · 成功 {ok} · 无需处理 {need} · 失败 {fail} · {saved}',
    'summary.saved': '节省 {size}',
    'summary.grew': '增加 {size}',
    'summary.nothing': '没有需要处理的文件',

    // Row details
    'detail.path': '完整路径',
    'detail.reasons': '诊断',
    'detail.probe': '封装信息',
    'detail.rawError': '原始错误',
    'fail.reason.structure':
      '文件的数据块结构异常，无法安全重排（可尝试改用「重编码」）',
    'fail.reason.unreadable': '文件无法读取（可能已被移动或删除）',
    'fail.reason.moov-missing': '文件缺少索引（moov），无法处理',
    'fail.reason.unknown': '{err}',

    // Reasons (classification)
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

    // Log
    'log.ready': '就绪',
    'log.noFile': '尚未选择文件',
    'log.cleared': '已清空列表',
    'log.scanning': '正在查找 {n} 个位置的视频…',
    'log.scanned': '查找完成：{ok} 无需处理 / {needs} 需要处理 / {broken} 无法处理',
    'log.scanFailed': '查找失败：{err}',
    'log.fixStart': '开始修复 {n} 个文件…',
    'log.fixDone': '修复结束：成功 {ok} / 失败 {fail}',
    'log.fixCancelled': '已中止修复',
    'log.fixFailed': '修复失败：{err}',
    'log.retry': '重试 {name}…',
    'log.retryDone': '重试完成 → {name}',
    'log.retryFailed': '重试失败：{err}',
    'log.undoDone': '已撤销：恢复 {restored} / 删除 {deleted} / 失败 {failed}',
    'log.undoFailed': '撤销失败：{err}',
    'log.backupListed': '备份列表已更新：{n} 份',

    // Backup management
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

    // Benefit numbers
    'gain.title': '无损完成（画质不变）',
    'gain.firstplay': '首次可播放需读取：约 {before} → 约 {after}',
    'gain.mdat': '数据碎片：{before} 块 → {after} 块',
    'gain.segments': '分片：{after} 段',
    'gain.size': '体积：{before} → {after}',

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
    'info.aria': 'Details',
    'lang.aria': 'Language',

    'ffmpeg.checking': 'checking ffmpeg…',
    'ffmpeg.missing': 'ffmpeg not found',

    'input.drop': 'Drop videos or folders here, or use the buttons below (many at once, again and again)',
    'btn.chooseFiles': 'Choose files…',
    'btn.chooseFolder': 'Choose folder…',
    'btn.rescan': 'Re-scan',
    'btn.clear': 'Clear list',
    'btn.chooseDir': 'Choose…',

    'list.count': '{total} video(s) · {selected} selected',
    'list.expand': 'Expand',
    'list.collapse': 'Collapse',
    'list.select': 'Select this file',

    'file.waiting': 'Waiting',
    'file.checking': 'Checking…',
    'file.ok': 'Nothing to do',
    'file.needs': 'Needs work',
    'file.broken': 'Cannot process',
    'file.queued': 'Queued',
    'file.queued.pos': 'Queued (position {pos})',
    'file.running': 'Fixing…',
    'file.done': 'Done',
    'file.failed': 'Failed',
    'file.skipped': 'Nothing to do',

    'settings.process': 'Processing',
    'settings.remux': 'Remux (lossless, fast)',
    'settings.reencode': 'Re-encode (changes codec/size, slow)',
    'label.format': 'Format',
    'opt.progressive': 'Progressive (plays anywhere)',
    'opt.fmp4': 'Fragmented (for streaming)',
    'hint.format': 'Progressive plays anywhere; Fragmented suits streaming',
    'info.format':
      'Progressive: plays anywhere, local or web. Fragmented: for streaming / live-style delivery.',
    'advanced.norm': 'Advanced (usually not needed)',
    'label.window': 'Interleave window (ms)',
    'hint.window': 'Audio/video interleave granularity; keep 1000 if unsure',
    'info.window':
      'How finely audio and video are interleaved. Smaller → smoother seeking, slightly larger file. Keep 1000 (1s) if unsure.',
    'label.frag': 'Fragment length (ms)',
    'hint.frag': 'Length of each fragment; keep 2000 if unsure',
    'info.frag': 'Seconds per fragment; only used for Fragmented MP4. Keep 2000 if unsure.',
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

    'output.label': 'Output',
    'output.inplace': 'Replace originals (auto backup)',
    'output.new': 'Write to a folder',
    'output.inplace.hint': 'Originals are backed up before they are replaced.',
    'output.inplace.warning':
      '⚠️ Original files will be replaced. Each source is backed up first to “{dir}”; restore it from “Backup management” below.',
    'output.inplace.defaultDir': '.mp4norm-backup beside each file',
    'output.backupdir': 'Backup folder',
    'output.backupdir.placeholder': 'Leave empty to store backups in .mp4norm-backup beside each file',
    'output.outdir': 'Output folder',
    'output.outdir.placeholder': 'Leave empty to write beside each source file',
    'output.namerule': 'File naming',
    'output.namerule.suffix': 'Keep name + suffix',
    'output.namerule.keep': 'Keep name',

    'btn.fix': 'Fix {n} file(s)',
    'btn.cancel': 'Stop',
    'btn.retry': 'Retry',
    'btn.retryFailed': 'Retry the {n} failed',
    'btn.undo': 'Undo this run',

    'scan.finding': 'Looking for videos… {n} found',
    'scan.checking': 'Checking… {n} found',
    'progress.current': 'Fixing {index}/{total} · {name}',
    'progress.done': '{done}/{total} done',
    'summary.done': 'Done · {ok} succeeded · {need} nothing to do · {fail} failed · {saved}',
    'summary.cancelled': 'Stopped · {ok} succeeded · {need} nothing to do · {fail} failed · {saved}',
    'summary.saved': 'saved {size}',
    'summary.grew': 'grew {size}',
    'summary.nothing': 'Nothing needs work',

    'detail.path': 'Full path',
    'detail.reasons': 'Diagnosis',
    'detail.probe': 'Container',
    'detail.rawError': 'Raw error',
    'fail.reason.structure':
      'the data-block structure is unusual and cannot be remuxed safely (try “Re-encode”)',
    'fail.reason.unreadable': 'the file could not be read (it may have been moved or deleted)',
    'fail.reason.moov-missing': 'the file has no index (moov) and cannot be processed',
    'fail.reason.unknown': '{err}',

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

    'log.ready': 'ready',
    'log.noFile': 'no file selected',
    'log.cleared': 'list cleared',
    'log.scanning': 'looking for videos in {n} location(s)…',
    'log.scanned': 'scan done: {ok} nothing to do / {needs} need work / {broken} cannot process',
    'log.scanFailed': 'scan failed: {err}',
    'log.fixStart': 'fixing {n} file(s)…',
    'log.fixDone': 'fix done: {ok} succeeded / {fail} failed',
    'log.fixCancelled': 'fix stopped',
    'log.fixFailed': 'fix failed: {err}',
    'log.retry': 'retrying {name}…',
    'log.retryDone': 'retry done -> {name}',
    'log.retryFailed': 'retry failed: {err}',
    'log.undoDone': 'undone: {restored} restored / {deleted} deleted / {failed} failed',
    'log.undoFailed': 'undo failed: {err}',
    'log.backupListed': 'backup list refreshed: {n}',

    'backup.card': 'Backup management',
    'backup.card.sub': 'Review, restore or delete the backups made by replace-originals',
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

    'gain.title': 'Done (lossless, no quality change)',
    'gain.firstplay': 'Bytes before first frame: ~{before} → ~{after}',
    'gain.mdat': 'Data fragments: {before} → {after}',
    'gain.segments': 'Fragments: {after}',
    'gain.size': 'Size: {before} → {after}',

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
// An unlisted Code falls back to the probe's own Message so we never render an
// empty string or a bare key.
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

/** getLang returns the active language code. */
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
  if (current === 'zh') return FINDINGS_ZH[finding.Code] || finding.Message;
  return finding.Message;
}
