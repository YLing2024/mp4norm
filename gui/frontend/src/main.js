import './style.css';

import {
  Probe,
  Normalize,
  Reencode,
  FFmpegVersion,
  InitialTargets,
  ChooseInput,
  ChooseInputs,
  ChooseFolder,
  ChooseOutput,
  ChooseOutputDir,
  ScanPaths,
  BatchNormalize,
  CancelBatch,
  SetLanguage,
} from '../wailsjs/go/main/App';
import { EventsOn } from '../wailsjs/runtime/runtime';
import { t, getLang, setLang, findingMessage } from './i18n';

// state is the single source of truth for every user-editable field so the whole
// page can be re-rendered on a language switch without losing input.
const state = {
  input: '',
  format: 'progressive',
  window: '1000',
  frag: '2000',
  outnorm: '',
  vcodec: 'h264',
  hw: 'auto',
  crf: '23',
  preset: 'medium',
  gop: '2',
  outreenc: '',
  report: null,
  showReport: false,
  // Collapsible sections. Kept in memory only (no persistence): every launch
  // starts collapsed, but a re-render (e.g. language switch) must not re-close
  // a section the user just opened.
  normAdvanced: false,
  reencodeOpen: false,
  ffmpeg: { status: 'checking', version: '' },
  logs: [],

  // Batch list state.
  files: [], // []classify.Verdict
  scanRoots: [], // the paths the list was scanned from, for relative display
  filter: 'all', // all | needs | broken
  sortBySize: false,
  expanded: {}, // path -> bool
  scanning: false,
  outdir: '',
  fileStatus: {}, // path -> { state, error, pct }
  batch: { running: false, done: 0, skipped: 0, failed: 0, total: 0, finished: false, cancelled: false },
};

const $ = (id) => document.getElementById(id);

const escapeHtml = (s) =>
  String(s).replace(/[&<>]/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;' }[c]));
const escapeAttr = (s) => escapeHtml(s).replace(/"/g, '&quot;');

// humanBytes mirrors internal/probe.humanBytes so sizes show at the right
// magnitude (B/KiB/MiB) instead of a rounded "0.0 MiB".
const humanBytes = (n) => {
  const unit = 1024;
  if (n < unit) return `${n} B`;
  let div = unit;
  let exp = 0;
  for (let x = Math.floor(n / unit); x >= unit; x = Math.floor(x / unit)) {
    div *= unit;
    exp++;
  }
  return `${(n / div).toFixed(1)} ${'KMGTPE'[exp]}iB`;
};

const moovValue = (pos) => {
  switch (pos) {
    case 'front':
      return t('report.moov.front');
    case 'end':
      return t('report.moov.end');
    default:
      return t('report.moov.unknown');
  }
};

const fmt = (report) => {
  if (!report) return '';
  const label = (key) => t(key).padEnd(12);
  const lines = [
    `${label('report.file')}${report.Path}`,
    `${label('report.size')}${humanBytes(report.FileSize)}`,
    `${label('report.brand')}${report.MajorBrand}`,
    `${label('report.moov')}${moovValue(report.MoovPosition)}`,
    `${label('report.fragmented')}${report.Fragmented ? t('report.bool.yes') : t('report.bool.no')}`,
    `${label('report.mdatCount')}${report.MdatCount}`,
  ];
  for (const f of report.Findings || []) {
    lines.push(`[${f.Severity}] ${f.Code}: ${findingMessage(f)}`);
  }
  return lines.join('\n');
};

// ---- Batch list helpers --------------------------------------------------

const statusText = (status) => t(`status.${status}`);

// reasonTextOf localizes a classification reason. t() returns the key itself
// when a translation is missing, so an unknown code falls back to the raw
// probe message instead of showing a bare "reason.xxx".
const reasonTextOf = (r) => {
  const key = `reason.${r.code}`;
  const s = t(key, { n: r.count });
  return s === key ? r.message || r.code : s;
};

const reasonText = (v) => {
  if (!v.reasons || !v.reasons.length) return t('reason.ok');
  return v.reasons.map(reasonTextOf).join(' + ');
};

const rawDetails = (v) => {
  const lines = [`${t('batch.detail.path')}: ${v.path}`];
  if (!v.probe) {
    if (v.error) lines.push(`${t('batch.detail.rawError')}: ${v.error}`);
    return lines.join('\n');
  }
  const p = v.probe;
  lines.push(
    `${t('report.brand')}: ${p.MajorBrand || '?'}`,
    `${t('report.moov')}: ${moovValue(p.MoovPosition)}`,
    `${t('report.mdatCount')}: ${p.MdatCount}`,
    `${t('report.fragmented')}: ${p.Fragmented ? t('report.bool.yes') : t('report.bool.no')}`,
    `${t('report.size')}: ${humanBytes(p.FileSize)} (${p.FileSize} B)`,
  );
  for (const f of p.Findings || []) {
    lines.push(`[${f.Severity}] ${f.Code}: ${findingMessage(f)}`);
  }
  // A failed batch attempt keeps its raw error here so the reason column can
  // stay plain-language.
  const st = state.fileStatus[v.path];
  if (st && st.error) lines.push(`${t('batch.detail.rawError')}: ${st.error}`);
  return lines.join('\n');
};

// toSlashes normalises Windows separators so relative display is stable.
const toSlashes = (p) => String(p).replace(/\\/g, '/');

// relDisplay shows a file relative to the scanned root (e.g.
// "only-good/2-good-faststart.mp4") so same-named files in different folders
// are distinguishable. Falls back to the basename when no root matches.
const relDisplay = (v) => {
  const full = toSlashes(v.path);
  for (const root of state.scanRoots) {
    const r = toSlashes(root).replace(/\/+$/, '');
    if (!r) continue;
    const fl = full.toLowerCase();
    const rl = r.toLowerCase();
    if (fl === rl) return v.name;
    if (fl.startsWith(`${rl}/`)) return full.slice(r.length + 1);
  }
  return v.name;
};

// failureReason turns a raw batch error into a plain-language sentence. Unknown
// errors keep their original text; the raw string always lands in the detail
// block so nothing is lost.
const failureReason = (err) => {
  const raw = String(err == null ? '' : err);
  const l = raw.toLowerCase();
  if (l.includes('moov')) return t('batch.fail.reason.moov-missing');
  if (l.includes('mdat') || l.includes('decode mp4')) return t('batch.fail.reason.structure');
  if (
    l.includes('no such file') ||
    l.includes('not exist') ||
    l.includes('cannot find') ||
    l.includes('could not find') ||
    l.includes('permission denied') ||
    l.includes('access is denied') ||
    l.includes('being used by another process') ||
    l.includes('is a directory')
  ) {
    return t('batch.fail.reason.unreadable');
  }
  return t('batch.fail.reason.unknown', { err: raw });
};

const stateText = (v) => {
  const st = state.fileStatus[v.path];
  if (!st) return '';
  switch (st.state) {
    case 'queued':
      return t('batch.status.queued');
    case 'running':
      return `${t('batch.status.running')} ${st.pct || 0}%`;
    case 'done':
      return t('batch.status.done');
    case 'failed':
      return failureReason(st.error);
    case 'skipped':
      return t('batch.status.skipped');
    default:
      return '';
  }
};

// needsWorkFiles counts the files the batch button would still process. A file
// that just failed normalization is excluded (re-scan the list to retry it), so
// a finished batch with only failures and successes disables the button.
const needsWorkFiles = () =>
  state.files.filter(
    (v) => v.status === 'needs_work' && state.fileStatus[v.path]?.state !== 'failed',
  );

const visibleFiles = () => {
  let list = state.files.filter((v) => {
    if (state.filter === 'needs') return v.status === 'needs_work';
    if (state.filter === 'broken') return v.status === 'broken';
    return true;
  });
  if (state.sortBySize) list = [...list].sort((a, b) => b.size - a.size);
  return list;
};

const ffmpegText = () => {
  if (state.ffmpeg.status === 'ok') return state.ffmpeg.version;
  if (state.ffmpeg.status === 'missing') return t('ffmpeg.missing');
  return t('ffmpeg.checking');
};

const log = (msg) => {
  const locale = getLang() === 'zh' ? 'zh-CN' : 'en-US';
  const line = `${new Date().toLocaleTimeString(locale)}  ${msg}`;
  state.logs.push(line);
  const el = $('log');
  if (el) {
    el.textContent += `${line}\n`;
    el.scrollTop = el.scrollHeight;
  }
};

const langButton = (lang, label) => {
  const active = getLang() === lang ? ' active' : '';
  return `<button type="button" class="lang-btn${active}" data-lang="${lang}">${label}</button>`;
};

// batchSection renders the whole file list, toolbar and progress block.
const batchSection = () => {
  const needs = needsWorkFiles().length;
  const total = state.files.length;

  if (total === 0 && !state.scanning) {
    return `
      <div class="batch-empty" id="batch-drop">${escapeHtml(t('batch.drop'))}</div>`;
  }

  const rows = visibleFiles()
    .map((v) => {
      const open = !!state.expanded[v.path];
      const main = `
        <tr class="batch-row verdict-${v.status}" data-path="${escapeAttr(v.path)}">
          <td class="col-name" title="${escapeAttr(v.path)}">${escapeHtml(relDisplay(v))}</td>
          <td class="col-size">${humanBytes(v.size)}</td>
          <td class="col-verdict">${statusText(v.status)}</td>
          <td class="col-reason">${escapeHtml(reasonText(v))}</td>
          <td class="col-state">${escapeHtml(stateText(v))}</td>
        </tr>`;
      if (!open) return main;
      return `${main}
        <tr class="batch-details"><td colspan="5"><pre>${escapeHtml(rawDetails(v))}</pre></td></tr>`;
    })
    .join('');

  const b = state.batch;
  const summary = b.finished
    ? b.cancelled
      ? t('batch.summary.cancelled', { ok: b.done, skip: b.skipped, fail: b.failed })
      : t('batch.summary', { ok: b.done, skip: b.skipped, fail: b.failed })
    : '';
  const progress = b.running || b.finished
    ? `<progress id="batch-progress" max="${Math.max(b.total, 1)}" value="${b.done + b.failed}"></progress>
       <div class="muted batch-progress-text">${escapeHtml(
         t('batch.progress', { done: b.done + b.failed, total: b.total }),
       )} ${escapeHtml(summary)}</div>`
    : '';

  return `
    <div class="row batch-toolbar">
      <label class="field inline">
        <span>${t('batch.filter')}</span>
        <select id="filter">
          <option value="all"${state.filter === 'all' ? ' selected' : ''}>${t('batch.filter.all')}</option>
          <option value="needs"${state.filter === 'needs' ? ' selected' : ''}>${t('batch.filter.needs')}</option>
          <option value="broken"${state.filter === 'broken' ? ' selected' : ''}>${t('batch.filter.broken')}</option>
        </select>
      </label>
      <label class="field inline checkbox">
        <span>&nbsp;</span>
        <span><input type="checkbox" id="sort-size"${state.sortBySize ? ' checked' : ''} /> ${escapeHtml(
          t('batch.sortSize'),
        )}</span>
      </label>
      <span class="spacer"></span>
      <button id="batch-normalize" class="btn primary" ${
        needs === 0 || b.running ? 'disabled' : ''
      }>${t('btn.batchNormalize.count', { n: needs })}</button>
      ${
        b.running
          ? `<button id="batch-cancel" class="btn">${t('btn.cancel')}</button>`
          : ''
      }
    </div>
    ${progress}
    <table class="batch-table" id="batch-table">
      <colgroup>
        <col class="w-name" />
        <col class="w-size" />
        <col class="w-verdict" />
        <col class="w-reason" />
        <col class="w-status" />
      </colgroup>
      <thead>
        <tr>
          <th class="col-name">${t('batch.col.file')}</th>
          <th class="col-size">${t('batch.col.size')}</th>
          <th class="col-verdict">${t('batch.col.verdict')}</th>
          <th class="col-reason">${t('batch.col.reason')}</th>
          <th class="col-state">${t('batch.col.status')}</th>
        </tr>
      </thead>
      <tbody>${rows}</tbody>
    </table>`;
};

function render() {
  const scrollY = window.scrollY;
  document.querySelector('#app').innerHTML = `
  <header>
    <h1>${t('app.name')}</h1>
    <span id="ffmpeg" class="muted">${escapeHtml(ffmpegText())}</span>
    <div id="lang-switch" class="lang-switch">
      ${langButton('zh', '中文')}
      ${langButton('en', 'English')}
    </div>
  </header>
  <p class="tagline">${escapeHtml(t('app.tagline'))}</p>

  <section class="card batch-card" id="batch-card" style="--wails-drop-target: drop">
    <h2>${t('batch.card')} <span class="sub muted">${t('batch.card.sub')}</span></h2>
    <div class="row">
      <button id="choose-files" class="btn primary">${t('btn.chooseFiles')}</button>
      <button id="choose-folder" class="btn">${t('btn.chooseFolder')}</button>
      <button id="clear-list" class="btn ghost">${t('btn.clear')}</button>
      <button id="scan-again" class="btn ghost">${t('btn.scan')}</button>
    </div>
    <div class="row">
      <label class="field">
        <span>${t('batch.outdir')}</span>
        <input id="outdir" type="text" placeholder="${escapeAttr(
          t('batch.outdir.hint'),
        )}" value="${escapeAttr(state.outdir)}" readonly />
      </label>
      <button id="choose-outdir" class="btn ghost">${t('btn.chooseOutdir')}</button>
    </div>
    ${batchSection()}
  </section>

  <section class="card">
    <div class="row">
      <label class="field">
        <span>${t('label.input')}</span>
        <input id="input" type="text" placeholder="${escapeAttr(t('input.placeholder'))}" value="${escapeAttr(
          state.input,
        )}" readonly />
        <span class="hint">${escapeHtml(t('hint.input'))}</span>
      </label>
      <button id="choose" class="btn">${t('btn.choose')}</button>
      <button id="inspect" class="btn">${t('btn.inspect')}</button>
    </div>
  </section>

  <section class="card" id="report-card" ${state.showReport ? '' : 'hidden'}>
    <h2>${t('card.diagnosis')}</h2>
    <pre id="report">${escapeHtml(state.report ? fmt(state.report) : '')}</pre>
  </section>

  <section class="card">
    <h2>${t('card.normalize')} <span class="sub muted">${t('card.normalize.sub')}</span></h2>
    <div class="row">
      <label class="field">
        <span>${t('label.format')}</span>
        <select id="format">
          <option value="progressive"${state.format === 'progressive' ? ' selected' : ''}>${t('opt.progressive')}</option>
          <option value="fmp4"${state.format === 'fmp4' ? ' selected' : ''}>${t('opt.fmp4')}</option>
        </select>
        <span class="hint">${escapeHtml(t('hint.format'))}</span>
      </label>
    </div>
    <div class="row">
      <label class="field">
        <span>${t('label.output')}</span>
        <input id="outnorm" type="text" placeholder="${escapeAttr(
          t('out.norm.placeholder'),
        )}" value="${escapeAttr(state.outnorm)}" readonly />
        <span class="hint">${escapeHtml(t('hint.output.norm'))}</span>
      </label>
      <button id="savenorm" class="btn ghost">${t('btn.saveas')}</button>
      <button id="normalize" class="btn primary">${t('btn.normalize')}</button>
    </div>
    <details class="advanced" id="advanced-norm"${state.normAdvanced ? ' open' : ''}>
      <summary>${t('advanced.norm')}</summary>
      <div class="row">
        <label id="window-label" class="field"${state.format === 'fmp4' ? ' hidden' : ''}>
          <span>${t('label.window')}</span>
          <input id="window" type="number" value="${escapeAttr(state.window)}" min="200" step="100" />
          <span class="hint">${escapeHtml(t('hint.window'))}</span>
        </label>
        <label id="frag-label" class="field"${state.format === 'fmp4' ? '' : ' hidden'}>
          <span>${t('label.frag')}</span>
          <input id="frag" type="number" value="${escapeAttr(state.frag)}" min="500" step="500" />
          <span class="hint">${escapeHtml(t('hint.frag'))}</span>
        </label>
      </div>
    </details>
  </section>

  <details class="card" id="reencode-card"${state.reencodeOpen ? ' open' : ''}>
    <summary class="card-summary">
      <h2>${t('card.reencode')} <span class="sub muted">${t('card.reencode.sub')}</span></h2>
    </summary>
    <div class="row">
      <label class="field">
        <span>${t('label.codec')}</span>
        <select id="vcodec">
          <option value="h264"${state.vcodec === 'h264' ? ' selected' : ''}>H.264</option>
          <option value="h265"${state.vcodec === 'h265' ? ' selected' : ''}>H.265</option>
          <option value="copy"${state.vcodec === 'copy' ? ' selected' : ''}>${t('opt.codec.copy')}</option>
        </select>
        <span class="hint">${escapeHtml(t('hint.codec'))}</span>
      </label>
      <label class="field">
        <span>${t('label.hw')}</span>
        <select id="hw">
          <option value="auto"${state.hw === 'auto' ? ' selected' : ''}>${t('opt.hw.auto')}</option>
          <option value="on"${state.hw === 'on' ? ' selected' : ''}>${t('opt.hw.on')}</option>
          <option value="off"${state.hw === 'off' ? ' selected' : ''}>${t('opt.hw.off')}</option>
        </select>
        <span class="hint">${escapeHtml(t('hint.hw'))}</span>
      </label>
      <label class="field">
        <span>${t('label.crf')}</span>
        <input id="crf" type="number" value="${escapeAttr(state.crf)}" min="0" max="51" />
        <span class="hint">${escapeHtml(t('hint.crf'))}</span>
      </label>
      <label class="field">
        <span>${t('label.preset')}</span>
        <select id="preset">
          <option${state.preset === 'ultrafast' ? ' selected' : ''}>ultrafast</option><option${
            state.preset === 'veryfast' ? ' selected' : ''
          }>veryfast</option>
          <option${state.preset === 'medium' ? ' selected' : ''}>medium</option><option${
            state.preset === 'slow' ? ' selected' : ''
          }>slow</option>
        </select>
        <span class="hint">${escapeHtml(t('hint.preset'))}</span>
      </label>
      <label class="field">
        <span>${t('label.gop')}</span>
        <input id="gop" type="number" value="${escapeAttr(state.gop)}" min="0.5" step="0.5" />
        <span class="hint">${escapeHtml(t('hint.gop'))}</span>
      </label>
    </div>
    <div class="row">
      <label class="field">
        <span>${t('label.output')}</span>
        <input id="outreenc" type="text" placeholder="${escapeAttr(
          t('out.reenc.placeholder'),
        )}" value="${escapeAttr(state.outreenc)}" readonly />
        <span class="hint">${escapeHtml(t('hint.output.reenc'))}</span>
      </label>
      <button id="savetreenc" class="btn ghost">${t('btn.saveas')}</button>
      <button id="reencode" class="btn">${t('btn.reencode')}</button>
    </div>
    <progress id="progress" max="1" value="0"></progress>
  </details>

  <section class="card">
    <h2>${t('card.log')}</h2>
    <pre id="log" class="log">${escapeHtml(state.logs.length ? state.logs.join('\n') + '\n' : '')}</pre>
  </section>
  `;
  bindEvents();
  const el = $('log');
  if (el) el.scrollTop = el.scrollHeight;
  window.scrollTo(0, scrollY);
}

function bindEvents() {
  document.querySelectorAll('.lang-btn').forEach((btn) => {
    btn.onclick = () => switchLang(btn.dataset.lang);
  });

  // Remember open/closed state across re-renders (language switch, format
  // change) without persisting it to disk.
  const adv = $('advanced-norm');
  if (adv) adv.ontoggle = () => { state.normAdvanced = adv.open; };
  const re = $('reencode-card');
  if (re) re.ontoggle = () => { state.reencodeOpen = re.open; };

  // ---- Batch list --------------------------------------------------------
  const cf = $('choose-files');
  if (cf) cf.onclick = async () => {
    const paths = await ChooseInputs();
    scanPaths(paths);
  };
  const cfd = $('choose-folder');
  if (cfd) cfd.onclick = async () => {
    const dir = await ChooseFolder();
    if (dir) scanPaths([dir]);
  };
  const clr = $('clear-list');
  if (clr) clr.onclick = () => {
    state.files = [];
    state.scanRoots = [];
    state.fileStatus = {};
    state.expanded = {};
    state.filter = 'all';
    state.sortBySize = false;
    state.batch = { running: false, done: 0, skipped: 0, failed: 0, total: 0, finished: false, cancelled: false };
    render();
    log(t('log.cleared'));
  };
  const sc = $('scan-again');
  if (sc) sc.onclick = () => {
    const paths = state.scanRoots.length ? state.scanRoots : state.files.map((v) => v.path);
    if (!paths.length) return log(t('log.noFile'));
    scanPaths(paths);
  };
  const cod = $('choose-outdir');
  if (cod) cod.onclick = async () => {
    const dir = await ChooseOutputDir();
    if (dir) {
      state.outdir = dir;
      render();
    }
  };
  const filt = $('filter');
  if (filt) filt.onchange = () => { state.filter = filt.value; render(); };
  const sortSize = $('sort-size');
  if (sortSize) sortSize.onchange = () => { state.sortBySize = sortSize.checked; render(); };
  const bn = $('batch-normalize');
  if (bn) bn.onclick = () => startBatch();
  const bc = $('batch-cancel');
  if (bc) bc.onclick = () => CancelBatch();

  const table = $('batch-table');
  if (table) {
    table.onclick = (e) => {
      const row = e.target.closest('tr[data-path]');
      if (!row) return;
      const p = row.dataset.path;
      state.expanded[p] = !state.expanded[p];
      render();
    };
  }

  $('choose').onclick = async () => {
    const path = await ChooseInput();
    if (!path) return;
    state.input = path;
    state.outnorm = '';
    state.outreenc = '';
    render();
    log(t('log.selected', { path }));
  };

  $('inspect').onclick = async () => {
    if (!state.input) return log(t('log.noFile'));
    try {
      state.report = await Probe(state.input);
      state.showReport = true;
      render();
      log(t('log.inspected'));
    } catch (e) {
      log(t('log.inspectFailed', { err: e }));
    }
  };

  $('format').onchange = () => {
    state.format = $('format').value;
    render();
  };

  // Keep plain fields mirrored into state so a re-render (e.g. on language
  // switch) never loses what the user typed or selected.
  const mirror = (id, key) => {
    const el = $(id);
    if (!el) return;
    el.oninput = el.onchange = () => {
      state[key] = el.value;
    };
  };
  mirror('window', 'window');
  mirror('frag', 'frag');
  mirror('crf', 'crf');
  mirror('gop', 'gop');
  mirror('vcodec', 'vcodec');
  mirror('hw', 'hw');
  mirror('preset', 'preset');

  $('savenorm').onclick = async () => {
    const p = await ChooseOutput('output.mp4');
    if (p) {
      state.outnorm = p;
      $('outnorm').value = p;
    }
  };

  $('savetreenc').onclick = async () => {
    const p = await ChooseOutput('output.reenc.mp4');
    if (p) {
      state.outreenc = p;
      $('outreenc').value = p;
    }
  };

  $('normalize').onclick = async () => {
    if (!state.input) return log(t('log.noFile'));
    log(t('log.normalizing'));
    try {
      const res = await Normalize({
        input: state.input,
        output: state.outnorm,
        format: state.format,
        windowMs: Number(state.window),
        fragmentMs: Number(state.frag),
      });
      showResult(res);
    } catch (e) {
      log(t('log.normalizeFailed', { err: e }));
    }
  };

  $('reencode').onclick = async () => {
    if (!state.input) return log(t('log.noFile'));
    log(t('log.reencoding'));
    $('progress').value = 0;
    try {
      const res = await Reencode({
        input: state.input,
        output: state.outreenc,
        videoCodec: state.vcodec,
        hardware: state.hw,
        crf: Number(state.crf),
        preset: state.preset,
        gopSeconds: Number(state.gop),
      });
      showResult(res);
    } catch (e) {
      log(t('log.reencodeFailed', { err: e }));
    }
  };
}

// ---- Batch actions -------------------------------------------------------

// refreshAfterBatch re-detects what a run produced so the verdicts and the
// batch button count reflect reality while the run summary stays visible:
//   - a successfully normalized file is replaced by its (now clean) output;
//   - failed/skipped inputs stay listed; failures keep their raw error for the
//     detail block and are not auto-retried until the list is re-scanned.
async function refreshAfterBatch(res) {
  const outputByInput = new Map();
  for (const o of res.results || []) {
    if (o.status === 'done' && o.output) outputByInput.set(o.path, o.output);
  }

  let byOutput = new Map();
  const outputs = [...outputByInput.values()];
  if (outputs.length) {
    const refreshed = await ScanPaths(outputs);
    byOutput = new Map((refreshed || []).map((v) => [v.path, v]));
  }

  state.files = state.files.map((v) => {
    const out = outputByInput.get(v.path);
    return out && byOutput.has(out) ? byOutput.get(out) : v;
  });

  // A re-run can create an output that was already listed; drop the duplicate
  // that the swap would otherwise introduce.
  const seen = new Set();
  state.files = state.files.filter((v) => {
    const key = toSlashes(v.path).toLowerCase();
    if (seen.has(key)) return false;
    seen.add(key);
    return true;
  });

  const status = {};
  for (const o of res.results || []) {
    if (o.status === 'failed') status[o.path] = { state: 'failed', error: o.error };
    else if (o.status === 'skipped') status[o.path] = { state: 'skipped' };
  }
  state.fileStatus = status;
}

async function scanPaths(paths) {
  if (!paths || !paths.length) return;
  state.scanning = true;
  state.scanRoots = [...paths];
  render();
  log(t('log.scanning', { n: paths.length }));
  try {
    const verdicts = await ScanPaths(paths);
    state.files = verdicts || [];
    state.fileStatus = {};
    state.expanded = {};
    state.scanning = false;
    state.batch = { running: false, done: 0, skipped: 0, failed: 0, total: 0, finished: false, cancelled: false };
    const ok = state.files.filter((v) => v.status === 'ok').length;
    const needs = state.files.filter((v) => v.status === 'needs_work').length;
    const broken = state.files.filter((v) => v.status === 'broken').length;
    log(t('log.scanned', { ok, needs, broken }));
    render();
  } catch (e) {
    state.scanning = false;
    log(t('log.scanFailed', { err: e }));
    render();
  }
}

async function startBatch() {
  const targets = needsWorkFiles();
  if (!targets.length) return log(t('batch.nothing'));
  state.fileStatus = {};
  for (const v of targets) state.fileStatus[v.path] = { state: 'queued' };
  state.batch = {
    running: true,
    done: 0,
    skipped: 0,
    failed: 0,
    total: targets.length,
    finished: false,
    cancelled: false,
  };
  render();
  log(t('log.batchStart', { n: targets.length }));

  let res;
  try {
    res = await BatchNormalize({
      inputs: targets.map((v) => v.path),
      outdir: state.outdir,
      format: state.format,
      windowMs: Number(state.window),
      fragmentMs: Number(state.frag),
    });
  } catch (e) {
    state.batch.running = false;
    log(t('log.batchFailed', { err: e }));
    render();
    return;
  }

  // Reconcile with the authoritative result (progress events may have been
  // throttled or missed).
  const done = new Set();
  for (const o of res.results || []) {
    done.add(o.path);
    state.fileStatus[o.path] = { state: o.status, error: o.error };
  }
  for (const v of targets) {
    if (!done.has(v.path)) state.fileStatus[v.path] = { state: 'skipped' };
  }
  state.batch.running = false;
  state.batch.finished = true;
  state.batch.cancelled = !!res.cancelled;
  state.batch.done = res.done || 0;
  state.batch.failed = res.failed || 0;
  state.batch.skipped = res.skipped || 0;
  log(
    res.cancelled
      ? t('log.batchCancelled')
      : t('log.batchDone', { ok: state.batch.done, fail: state.batch.failed }),
  );
  await refreshAfterBatch(res);
  log(t('log.batchRefreshed'));
  render();
}

const showResult = (res) => {
  state.report = res.report || res.Report;
  state.showReport = true;
  render();
  log(t('log.done', { path: res.output || res.Output }));
};

async function refreshFFmpeg() {
  try {
    const v = await FFmpegVersion();
    state.ffmpeg = { status: 'ok', version: v.split(' Copyright')[0] };
  } catch (e) {
    state.ffmpeg = { status: 'missing', version: '' };
  }
  const el = $('ffmpeg');
  if (el) el.textContent = ffmpegText();
}

// openInitialTargets handles directories/files passed on the command line. It
// feeds them through scanPaths — the exact same path as the "choose folder"
// button — so no separate code is needed. It only detects; normalizing still
// requires the user to press the batch button.
async function openInitialTargets() {
  let targets;
  try {
    targets = await InitialTargets();
  } catch (e) {
    return; // No command-line support available; keep the empty state.
  }
  if (Array.isArray(targets) && targets.length) scanPaths(targets);
}

async function switchLang(lang) {
  if (lang === getLang()) return;
  setLang(lang);
  render();
  try {
    await SetLanguage(getLang());
  } catch (e) {
    // Non-fatal: the UI already switched; dialogs keep the previous language.
  }
}

EventsOn('reencode:progress', (p) => {
  if (!p) return;
  const secs = (p.OutTime || 0) / 1e6;
  const bar = $('progress');
  if (!bar) return;
  bar.value = Math.min(0.99, secs / 100);
  bar.title = `${secs.toFixed(1)}s  frame ${p.Frame}  ${p.Speed}`;
});

// Batch progress: update the per-row status column and the overall bar.
EventsOn('batch:progress', (p) => {
  if (!p) return;
  if (p.status === 'running') {
    const pct = p.total > 0 ? Math.round(((p.index - 1) / p.total) * 100) : 0;
    state.fileStatus[p.path] = { state: 'running', pct };
  } else if (p.status === 'done') {
    state.batch.done++;
    state.fileStatus[p.path] = { state: 'done' };
  } else if (p.status === 'failed') {
    state.batch.failed++;
    state.fileStatus[p.path] = { state: 'failed', error: p.error };
  }
  render();
});

// Files dropped onto the batch card start a scan.
EventsOn('files:dropped', (paths) => {
  if (Array.isArray(paths) && paths.length) scanPaths(paths);
});

render();
refreshFFmpeg();
SetLanguage(getLang());
log(t('log.ready'));
openInitialTargets();
