// i18n.js — bilingual strings (zh default / en) and language persistence for the
// mp4norm GUI. Static copy mirrors the spec's copy table exactly.

const STORAGE_KEY = 'mp4norm.lang';
const DEFAULT_LANG = 'zh';
const LANGS = ['zh', 'en'];

const STRINGS = {
  zh: {
    'app.name': 'mp4norm',
    'app.tagline': '不改画质，只重排文件内部结构 —— 让视频秒开、拖动不卡',

    // Header / file selection
    'ffmpeg.checking': '正在检测 ffmpeg…',
    'ffmpeg.missing': '未找到 ffmpeg',
    'input.placeholder': '选择 MP4 文件…',
    'btn.choose': '选择文件…',
    'btn.inspect': '检查',
    'card.diagnosis': '诊断结果',

    // Normalize card
    'card.normalize': '无损规整',
    'card.normalize.sub': '不重新压画质，只整理文件结构（<strong>推荐先用这个</strong>）',
    'label.input': '视频文件',
    'hint.input': '第 1 步：选择要处理的 MP4 文件，再点「检查」看看有没有问题',
    'label.format': '输出格式',
    'opt.progressive': '普通 MP4（最通用）',
    'opt.fmp4': '分片 MP4（适合边下边播）',
    'hint.format':
      '普通 MP4：最通用，本地播放、网页播放都合适｜分片 MP4：适合边下边播、直播类场景',
    'label.output': '输出路径',
    'hint.output.norm': '第 2 步：留空则保存为「输入文件名.norm.mp4」',
    'label.window': '交织窗口（毫秒）',
    'hint.window':
      '音频与视频数据交叉存放的时间粒度。调小 → 拖动更顺、文件略大；调大 → 相反。不确定就保持 1000（1 秒）',
    'label.frag': '分片时长（毫秒）',
    'hint.frag': '每个分片装多少秒内容，只在选了「分片 MP4」时生效。不确定就保持 2000',
    'advanced.norm': '高级选项（一般不用改）',
    'out.norm.placeholder': '输出路径（默认：<输入>.norm.mp4）',
    'btn.saveas': '另存为…',
    'btn.normalize': '开始规整',

    // Re-encode card
    'card.reencode': '重新编码（可选 · 高级）',
    'card.reencode.sub':
      '<strong>多数情况用不到</strong> —— 只有拖动卡顿、帧率异常时才需要；会重新压一遍，画质有损',
    'label.codec': '视频编码',
    'opt.codec.copy': '直接复制（copy）',
    'hint.codec':
      'H.264 兼容性最好；H.265 体积更小但老设备可能播不了；copy 表示只换容器、不重新编码',
    'label.hw': '硬件加速',
    'opt.hw.auto': '自动',
    'opt.hw.on': '开启',
    'opt.hw.off': '关闭',
    'hint.hw': '自动 = 优先用显卡编码（快很多）。要画质或显卡不兼容时选「关闭」',
    'label.crf': 'CRF（画质档位）',
    'hint.crf': '画质档位：数值越小越清晰、文件越大。推荐 23（18 接近无损）',
    'label.preset': '编码预设',
    'hint.preset': '速度与体积的取舍：ultrafast 最快但文件最大 → slow 最慢但最小',
    'label.gop': '关键帧间隔（GOP·秒）',
    'hint.gop': '每隔多久插入一个「可跳转点」。调小 → 拖动更精准、文件略大。推荐 2 秒',
    'hint.output.reenc': '留空则保存为「输入文件名.reenc.mp4」',
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
    'app.tagline':
      'No re-encode: rearranges the file layout so videos open instantly and seek smoothly',

    // Header / file selection
    'ffmpeg.checking': 'checking ffmpeg…',
    'ffmpeg.missing': 'ffmpeg not found',
    'input.placeholder': 'choose an MP4 file…',
    'btn.choose': 'Choose file…',
    'btn.inspect': 'Inspect',
    'card.diagnosis': 'Diagnosis',

    // Normalize card
    'card.normalize': 'Normalize',
    'card.normalize.sub': 'Rewrites the container only — no quality loss (start here)',
    'label.input': 'Video file',
    'hint.input': 'Step 1: pick the MP4 file, then click Inspect to check for problems',
    'label.format': 'Format',
    'opt.progressive': 'Progressive (plays anywhere)',
    'opt.fmp4': 'Fragmented (for streaming)',
    'hint.format':
      'Progressive: plays anywhere｜Fragmented: for streaming / live-style delivery',
    'label.output': 'Output',
    'hint.output.norm': 'Step 2: leave empty to save as "<input>.norm.mp4"',
    'label.window': 'Interleave window (ms)',
    'hint.window':
      'How finely audio and video are interleaved. Smaller → smoother seeking, slightly larger file. Keep 1000 (1s) if unsure',
    'label.frag': 'Fragment length (ms)',
    'hint.frag': 'Seconds per fragment; only used for Fragmented MP4. Keep 2000 if unsure',
    'advanced.norm': 'Advanced (usually not needed)',
    'out.norm.placeholder': 'output (default: <input>.norm.mp4)',
    'btn.saveas': 'Save as…',
    'btn.normalize': 'Normalize',

    // Re-encode card
    'card.reencode': 'Re-encode (optional · advanced)',
    'card.reencode.sub':
      'Usually unnecessary — only for slow seeking or odd frame rates; re-encodes (lossy)',
    'label.codec': 'Codec',
    'opt.codec.copy': 'copy',
    'hint.codec':
      'H.264 plays everywhere; H.265 is smaller but older devices may not decode it; copy only remuxes',
    'label.hw': 'Hardware acceleration',
    'opt.hw.auto': 'auto',
    'opt.hw.on': 'on',
    'opt.hw.off': 'off',
    'hint.hw': 'auto prefers the GPU encoder (much faster); choose off for best quality or if unsupported',
    'label.crf': 'CRF (quality)',
    'hint.crf': 'Quality dial: lower = sharper and larger. 23 is a good default (~18 ≈ lossless)',
    'label.preset': 'Preset',
    'hint.preset': 'Speed vs size trade-off: ultrafast is fastest and largest → slow is smallest',
    'label.gop': 'Keyframe / GOP (s)',
    'hint.gop':
      'How often a seekable keyframe is inserted. Smaller → more precise scrubbing, slightly larger. 2s is fine',
    'hint.output.reenc': 'Leave empty to save as "<input>.reenc.mp4"',
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
