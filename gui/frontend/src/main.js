import './style.css';

import {
  FFmpegVersion,
  InitialTargets,
  ChooseInputs,
  ChooseFolder,
  ChooseOutputDir,
  ScanStream,
  BatchNormalize,
  BatchReencode,
  CancelBatch,
  UndoBatch,
  SetLanguage,
  ListBackups,
  RestoreBackup,
  DeleteBackups,
} from '../wailsjs/go/main/App';
import { EventsOn, OnFileDrop } from '../wailsjs/runtime/runtime';
import { t, getLang, setLang, findingMessage, LANGUAGES } from './i18n';

// There is exactly ONE processing flow. A drop of any count (files, folders,
// mixed, repeated) appends rows to one list; N=1 is just a list with one row.
// Processing settings and the output mode appear once and apply to every row.
const state = {
  // Processing settings (one set, used for any file count).
  processMode: 'remux', // remux | reencode
  format: 'progressive',
  window: '1000',
  frag: '2000',
  vcodec: 'h264',
  hw: 'auto',
  crf: '23',
  preset: 'medium',
  gop: '2',
  normAdvanced: false,

  // One output mode, one backup folder field.
  outputMode: 'new', // new | inplace
  outdir: '',
  backupDir: '',
  nameRule: 'suffix', // suffix | keep

  // The single list. byPath makes event lookups O(1); rowEls maps a path to its
  // live DOM node so streamed updates never re-render thousands of rows.
  files: [],
  byPath: {},
  rowEls: {},
  selection: {},
  touched: {},
  expanded: {},
  scanRoots: [],
  scanning: false,
  scanPhase: '',
  queue: {},

  // Per-row run status: path -> { state, error, gain, pos }.
  fileStatus: {},
  batch: emptyBatch(),
  lastRun: null, // { outcomes, kind, inPlace, backupDir, failedPaths }

  ffmpeg: { status: 'checking', version: '' },
  logs: [],

  // Backup management.
  backupOpen: false,
  backups: [],
  backupResolvedDir: '',
  backupConfirm: -1,
};

function emptyBatch() {
  return {
    running: false, finished: false, cancelled: false, total: 0,
    done: 0, failed: 0, skipped: 0, saved: 0,
    currentIndex: 0, currentName: '',
  };
}

const $ = (id) => document.getElementById(id);
const basename = (p) => String(p).split(/[\\/]/).pop();

const escapeHtml = (s) =>
  String(s).replace(/[&<>]/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;' }[c]));
const escapeAttr = (s) => escapeHtml(s).replace(/"/g, '&quot;');

// info renders the ⓘ toggle shown next to a parameter label.
const info = (key) =>
  `<details class="info"><summary aria-label="${escapeAttr(t('info.aria'))}">ⓘ</summary>` +
  `<span class="info-pop" role="tooltip">${escapeHtml(t(key))}</span></details>`;

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

const gainText = (g) => {
  if (!g) return '';
  const lines = [t('gain.title')];
  lines.push(
    t('gain.firstplay', {
      before: humanBytes(g.beforeFirstPlay),
      after: humanBytes(g.afterFirstPlay),
    }),
  );
  if (g.fragmented) lines.push(t('gain.segments', { after: g.afterMdat }));
  else lines.push(t('gain.mdat', { before: g.beforeMdat, after: g.afterMdat }));
  lines.push(t('gain.size', { before: humanBytes(g.beforeSize), after: humanBytes(g.afterSize) }));
  return lines.join('\n');
};

// savedText is the size delta of one finished file, named either way so a file
// that grew is never silently reported as a saving.
const savedText = (g) => {
  if (!g) return '';
  const delta = g.beforeSize - g.afterSize;
  if (delta >= 0) return t('summary.saved', { size: humanBytes(delta) });
  return t('summary.grew', { size: humanBytes(-delta) });
};

const inplace = () => state.outputMode === 'inplace';
const backupDirDisplay = () => state.backupDir || t('output.inplace.defaultDir');

// ---- List helpers --------------------------------------------------------

const reasonTextOf = (r, size) => {
  const key = `reason.${r.code}`;
  const s = t(key, { n: r.count, size: size ? humanBytes(size).replace('.0 ', ' ') : '' });
  return s === key ? r.message || r.code : s;
};

const reasonText = (v) => {
  if (!v.reasons || !v.reasons.length) return t('reason.ok');
  return v.reasons.map((r) => reasonTextOf(r, v.size)).join(' + ');
};

// failureReason turns a raw error into a plain-language sentence; the raw text
// is always kept in the row details.
const failureReason = (err) => {
  const raw = String(err == null ? '' : err);
  const l = raw.toLowerCase();
  if (l.includes('moov')) return t('fail.reason.moov-missing');
  if (l.includes('mdat') || l.includes('decode mp4')) return t('fail.reason.structure');
  if (
    l.includes('no such file') ||
    l.includes('not exist') ||
    l.includes('cannot find') ||
    l.includes('could not find') ||
    l.includes('permission denied') ||
    l.includes('access is denied') ||
    l.includes('being used by another process') ||
    l.includes('is a directory') ||
    l.includes('overwrite the input')
  ) {
    return t('fail.reason.unreadable');
  }
  return t('fail.reason.unknown', { err: raw });
};

const toSlashes = (p) => String(p).replace(/\\/g, '/');

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

// rowStatus maps a row to its single visible state: a colour tone paired with
// text. Run status wins over the pre-run verdict once a row has been queued.
function rowStatus(v) {
  const st = state.fileStatus[v.path];
  if (st) {
    switch (st.state) {
      case 'queued':
        return {
          text: st.pos ? t('file.queued.pos', { pos: st.pos }) : t('file.queued'),
          tone: 'grey',
        };
      case 'running':
        return { text: t('file.running'), tone: 'blue' };
      case 'done':
        return { text: st.gain ? `${t('file.done')} · ${savedText(st.gain)}` : t('file.done'), tone: 'green' };
      case 'failed':
        return { text: `${t('file.failed')} · ${failureReason(st.error)}`, tone: 'red' };
      case 'skipped':
        return { text: t('file.ok'), tone: 'green' };
    }
  }
  if (v.phase === 'checking') return { text: t('file.checking'), tone: 'blue' };
  switch (v.status) {
    case 'ok':
      return { text: t('file.ok'), tone: 'green' };
    case 'needs_work':
      return { text: `${t('file.needs')} · ${reasonText(v)}`, tone: 'yellow' };
    case 'broken':
      return { text: `${t('file.broken')} · ${reasonText(v)}`, tone: 'red' };
    default:
      return { text: t('file.waiting'), tone: 'grey' };
  }
}

const VERDICT_RANK = { broken: 0, needs_work: 1, ok: 2 };
const verdictRank = (status) => VERDICT_RANK[status] ?? 3;

function sortFiles() {
  state.files.sort(
    (a, b) => verdictRank(a.status) - verdictRank(b.status) || a.name.localeCompare(b.name),
  );
}

const selectedRows = () =>
  state.files.filter((v) => state.selection[v.path] && v.status !== 'broken');
const selectedCount = () => selectedRows().length;

// ---- HTML -----------------------------------------------------------------

function headerHTML() {
  const options = LANGUAGES.map(
    (l) => `<option value="${l.code}"${getLang() === l.code ? ' selected' : ''}>${l.label}</option>`,
  ).join('');
  return `
  <header>
    <h1>${t('app.name')}</h1>
    <span id="ffmpeg" class="muted" title="${escapeAttr(ffmpegTitle())}">${escapeHtml(ffmpegText())}</span>
    <select id="lang" class="lang-select" aria-label="${escapeAttr(t('lang.aria'))}">${options}</select>
  </header>`;
}

// globalStatusHTML is the whole-scan / whole-run status above the list: streamed
// discovery, real progress counts, and the completion summary with its actions.
function globalStatusHTML() {
  const b = state.batch;
  if (state.scanning) {
    const text =
      state.scanPhase === 'check'
        ? t('scan.checking', { n: state.files.length })
        : t('scan.finding', { n: state.files.length });
    return `<div class="global-status tone-blue" id="global-status">${escapeHtml(text)}</div>`;
  }
  if (b.running) {
    const done = b.done + b.failed;
    return `
      <div class="global-status tone-blue" id="global-status">
        <div>${escapeHtml(t('progress.current', { index: b.currentIndex, total: b.total, name: b.currentName }))}</div>
        <progress id="run-progress" max="${Math.max(b.total, 1)}" value="${done}"></progress>
        <div class="muted">${escapeHtml(t('progress.done', { done, total: b.total }))}</div>
      </div>`;
  }
  if (b.finished) {
    const saved = b.saved || 0;
    const savedStr = saved >= 0
      ? t('summary.saved', { size: humanBytes(saved) })
      : t('summary.grew', { size: humanBytes(-saved) });
    const line = t(b.cancelled ? 'summary.cancelled' : 'summary.done', {
      ok: b.done, need: b.skipped, fail: b.failed, saved: savedStr,
    });
    const failed = state.lastRun ? state.lastRun.failedPaths : [];
    return `
      <div class="global-status tone-${b.failed > 0 ? 'red' : 'green'}" id="global-status">
        <div>${escapeHtml(line)}</div>
        <div class="row">
          ${failed.length ? `<button class="btn" data-act="retry-failed">${t('btn.retryFailed', { n: failed.length })}</button>` : ''}
          ${state.lastRun ? `<button class="btn ghost" data-act="undo">${t('btn.undo')}</button>` : ''}
        </div>
      </div>`;
  }
  if (state.files.length) {
    return `<div class="global-status" id="global-status"><span class="muted">${escapeHtml(
      t('list.count', { total: state.files.length, selected: selectedCount() }),
    )}</span></div>`;
  }
  return `<div class="global-status" id="global-status"></div>`;
}

function fileListHTML() {
  return `<div id="file-list" class="file-list">${state.files.map(rowHTML).join('')}</div>`;
}

function rowHTML(v) {
  const s = rowStatus(v);
  const open = !!state.expanded[v.path];
  const checked = state.selection[v.path] ? ' checked' : '';
  const disabled = v.status === 'broken' ? ' disabled' : '';
  const failed = state.fileStatus[v.path] && state.fileStatus[v.path].state === 'failed';
  const retry = failed
    ? `<button type="button" class="btn row-retry" data-act="retry" data-path="${escapeAttr(v.path)}">${t('btn.retry')}</button>`
    : '';
  return `
    <div class="file-row tone-${s.tone}" data-path="${escapeAttr(v.path)}">
      <div class="file-main">
        <input type="checkbox" class="row-check" data-path="${escapeAttr(v.path)}"${checked}${disabled} aria-label="${escapeAttr(t('list.select'))}" />
        <span class="row-name" title="${escapeAttr(v.path)}">${escapeHtml(relDisplay(v))}</span>
        <span class="badge tone-${s.tone}">${escapeHtml(s.text)}</span>
        ${retry}
        <button type="button" class="btn ghost row-toggle" data-act="toggle" data-path="${escapeAttr(v.path)}">
          ${open ? t('list.collapse') : t('list.expand')}
        </button>
      </div>
      ${open ? `<div class="file-detail">${rowDetailHTML(v)}</div>` : ''}
    </div>`;
}

function rowDetailHTML(v) {
  const st = state.fileStatus[v.path];
  const lines = [
    `<div class="detail-line"><span class="detail-key">${t('detail.path')}</span> <code>${escapeHtml(v.path)}</code></div>`,
  ];
  if (v.reasons && v.reasons.length) {
    lines.push(
      `<div class="detail-line"><span class="detail-key">${t('detail.reasons')}</span> ${escapeHtml(reasonText(v))}</div>`,
    );
  }
  if (v.probe) {
    lines.push(`<div class="detail-line"><span class="detail-key">${t('detail.probe')}</span><pre>${escapeHtml(fmt(v.probe))}</pre></div>`);
  }
  if (st && st.gain) lines.push(`<pre class="gain">${escapeHtml(gainText(st.gain))}</pre>`);
  const rawErr = (st && st.error) || v.error;
  if (rawErr) {
    lines.push(
      `<div class="detail-line"><span class="detail-key">${t('detail.rawError')}</span> <code>${escapeHtml(rawErr)}</code></div>`,
    );
  }
  const retry =
    st && st.state === 'failed'
      ? `<button class="btn" data-act="retry" data-path="${escapeAttr(v.path)}">${t('btn.retry')}</button>`
      : '';
  return `<div class="detail-body">${lines.join('')}</div>${retry ? `<div class="row">${retry}</div>` : ''}`;
}

function remuxFields() {
  return `
    <div class="row">
      <label class="field">
        <span>${t('label.format')} ${info('info.format')}</span>
        <select id="format">
          <option value="progressive"${state.format === 'progressive' ? ' selected' : ''}>${t('opt.progressive')}</option>
          <option value="fmp4"${state.format === 'fmp4' ? ' selected' : ''}>${t('opt.fmp4')}</option>
        </select>
        <span class="hint">${escapeHtml(t('hint.format'))}</span>
      </label>
    </div>
    <details class="advanced" id="advanced-norm"${state.normAdvanced ? ' open' : ''}>
      <summary>${t('advanced.norm')}</summary>
      <div class="row">
        <label class="field"${state.format === 'fmp4' ? ' hidden' : ''}>
          <span>${t('label.window')} ${info('info.window')}</span>
          <input id="window" type="number" value="${escapeAttr(state.window)}" min="200" step="100" />
          <span class="hint">${escapeHtml(t('hint.window'))}</span>
        </label>
        <label class="field"${state.format === 'fmp4' ? '' : ' hidden'}>
          <span>${t('label.frag')} ${info('info.frag')}</span>
          <input id="frag" type="number" value="${escapeAttr(state.frag)}" min="500" step="500" />
          <span class="hint">${escapeHtml(t('hint.frag'))}</span>
        </label>
      </div>
    </details>`;
}

function reencodeFields() {
  return `
    <div class="row">
      <label class="field">
        <span>${t('label.codec')} ${info('info.codec')}</span>
        <select id="vcodec">
          <option value="h264"${state.vcodec === 'h264' ? ' selected' : ''}>H.264</option>
          <option value="h265"${state.vcodec === 'h265' ? ' selected' : ''}>H.265</option>
          <option value="copy"${state.vcodec === 'copy' ? ' selected' : ''}>${t('opt.codec.copy')}</option>
        </select>
        <span class="hint">${escapeHtml(t('hint.codec'))}</span>
      </label>
      <label class="field">
        <span>${t('label.hw')} ${info('info.hw')}</span>
        <select id="hw">
          <option value="auto"${state.hw === 'auto' ? ' selected' : ''}>${t('opt.hw.auto')}</option>
          <option value="on"${state.hw === 'on' ? ' selected' : ''}>${t('opt.hw.on')}</option>
          <option value="off"${state.hw === 'off' ? ' selected' : ''}>${t('opt.hw.off')}</option>
        </select>
        <span class="hint">${escapeHtml(t('hint.hw'))}</span>
      </label>
      <label class="field">
        <span>${t('label.crf')} ${info('info.crf')}</span>
        <input id="crf" type="number" value="${escapeAttr(state.crf)}" min="0" max="51" />
        <span class="hint">${escapeHtml(t('hint.crf'))}</span>
      </label>
      <label class="field">
        <span>${t('label.preset')} ${info('info.preset')}</span>
        <select id="preset">
          ${['ultrafast', 'veryfast', 'medium', 'slow'].map(
            (p) => `<option value="${p}"${state.preset === p ? ' selected' : ''}>${p}</option>`,
          ).join('')}
        </select>
        <span class="hint">${escapeHtml(t('hint.preset'))}</span>
      </label>
      <label class="field">
        <span>${t('label.gop')} ${info('info.gop')}</span>
        <input id="gop" type="number" value="${escapeAttr(state.gop)}" min="0.5" step="0.5" />
        <span class="hint">${escapeHtml(t('hint.gop'))}</span>
      </label>
    </div>`;
}

function inplaceFields() {
  return `
    <div class="row">
      <label class="field">
        <span>${t('output.backupdir')}</span>
        <input id="backup-dir" type="text" placeholder="${escapeAttr(
          t('output.backupdir.placeholder'),
        )}" value="${escapeAttr(state.backupDir)}" />
      </label>
      <button type="button" class="btn ghost" data-act="choose-backup-dir">${t('btn.chooseDir')}</button>
    </div>
    <div class="warning-banner" role="alert">${escapeHtml(
      t('output.inplace.warning', { dir: backupDirDisplay() }),
    )}</div>`;
}

function newFileFields() {
  return `
    <div class="row">
      <label class="field">
        <span>${t('output.outdir')}</span>
        <input id="outdir" type="text" placeholder="${escapeAttr(
          t('output.outdir.placeholder'),
        )}" value="${escapeAttr(state.outdir)}" readonly />
      </label>
      <button type="button" class="btn ghost" data-act="choose-outdir">${t('btn.chooseDir')}</button>
      <label class="field">
        <span>${t('output.namerule')}</span>
        <select id="namerule">
          <option value="suffix"${state.nameRule === 'suffix' ? ' selected' : ''}>${t('output.namerule.suffix')}</option>
          <option value="keep"${state.nameRule === 'keep' ? ' selected' : ''}>${t('output.namerule.keep')}</option>
        </select>
      </label>
    </div>`;
}

function settingsHTML() {
  const count = selectedCount();
  const canFix = !state.scanning && !state.batch.running && count > 0;
  return `
  <div class="settings">
    <fieldset class="setting">
      <legend>${t('settings.process')}</legend>
      <div class="radios">
        <label class="radio"><input type="radio" name="process" value="remux"${state.processMode === 'remux' ? ' checked' : ''} /> ${t('settings.remux')}</label>
        <label class="radio"><input type="radio" name="process" value="reencode"${state.processMode === 'reencode' ? ' checked' : ''} /> ${t('settings.reencode')}</label>
      </div>
      <div class="process-fields">${state.processMode === 'remux' ? remuxFields() : reencodeFields()}</div>
    </fieldset>

    <fieldset class="setting output-mode">
      <legend>${t('output.label')}</legend>
      <div class="radios">
        <label class="radio"><input type="radio" name="output-mode" value="new"${inplace() ? '' : ' checked'} /> ${t('output.new')}</label>
        <label class="radio"><input type="radio" name="output-mode" value="inplace"${inplace() ? ' checked' : ''} /> ${t('output.inplace')}</label>
      </div>
      ${inplace() ? `<span class="hint">${escapeHtml(t('output.inplace.hint'))}</span>` : ''}
      <div class="mode-fields">${inplace() ? inplaceFields() : newFileFields()}</div>
    </fieldset>

    <div class="row actions">
      <button id="fix" class="btn primary big"${canFix ? '' : ' disabled'}>${escapeHtml(t('btn.fix', { n: count }))}</button>
      ${state.batch.running ? `<button class="btn" data-act="cancel">${t('btn.cancel')}</button>` : ''}
    </div>
  </div>`;
}

function logSection() {
  return `
  <section class="card">
    <h2>${t('card.log')}</h2>
    <pre id="log" class="log">${escapeHtml(state.logs.length ? `${state.logs.join('\n')}\n` : '')}</pre>
  </section>`;
}

function backupSection() {
  const locale = getLang() === 'en' ? 'en-US' : 'zh-CN';
  const rows = state.backups
    .map((e, i) => {
      const confirming = state.backupConfirm === i;
      const count = state.backups.filter((x) => x.original === e.original).length;
      const stateLabel = e.originalExists ? t('backup.state.exists') : t('backup.state.missing');
      const missing = e.backupExists ? '' : ` <span class="muted">(${t('backup.state.backupMissing')})</span>`;
      const when = e.createdUnixMs ? new Date(e.createdUnixMs).toLocaleString(locale) : '';
      const confirmHint = confirming
        ? `<div class="muted backup-confirm-hint">${escapeHtml(t('backup.deleteConfirm', { n: count }))}</div>`
        : '';
      return `
        <tr>
          <td class="col-name" title="${escapeAttr(e.original)}">${escapeHtml(e.original)}</td>
          <td>${escapeHtml(e.backup)}${missing}</td>
          <td class="col-size">${humanBytes(e.size)}</td>
          <td class="col-time">${escapeHtml(when)}</td>
          <td class="col-state">${escapeHtml(stateLabel)}</td>
          <td class="col-actions">
            <button type="button" class="btn ghost" data-act="backup-restore" data-index="${i}">${t('btn.restore')}</button>
            <button type="button" class="btn ${confirming ? 'primary' : 'ghost'}" data-act="backup-delete" data-index="${i}">${confirming ? t('btn.confirmDelete') : t('btn.deleteBackup')}</button>
            ${confirmHint}
          </td>
        </tr>`;
    })
    .join('');

  const body = state.backups.length
    ? `<div class="muted backup-resolved">${escapeHtml(t('backup.resolved', { dir: state.backupResolvedDir }))}</div>
       <table class="backup-table">
         <thead><tr>
           <th>${t('backup.col.original')}</th><th>${t('backup.col.backup')}</th><th>${t('backup.col.size')}</th>
           <th>${t('backup.col.time')}</th><th>${t('backup.col.state')}</th><th>${t('backup.col.actions')}</th>
         </tr></thead>
         <tbody>${rows}</tbody>
       </table>`
    : `<div class="muted backup-empty">${escapeHtml(t('backup.empty'))}</div>`;

  return `
    <details class="card" id="backup-card"${state.backupOpen ? ' open' : ''}>
      <summary class="card-summary">
        <h2>${t('backup.card')} <span class="sub muted">${t('backup.card.sub')}</span></h2>
      </summary>
      <div class="row">
        <label class="field">
          <span>${t('backup.dir')}</span>
          <input id="backup-mgmt-dir" type="text" placeholder="${escapeAttr(
            t('backup.dir.placeholder'),
          )}" value="${escapeAttr(state.backupDir)}" />
          <span class="hint">${escapeHtml(t('backup.dir.hint'))}</span>
        </label>
        <button type="button" class="btn ghost" data-act="choose-backup-mgmt">${t('btn.chooseBackupDir')}</button>
        <button type="button" class="btn" data-act="list-backups">${t('btn.listBackups')}</button>
      </div>
      ${body}
    </details>`;
}

function render() {
  const scrollY = window.scrollY;
  document.querySelector('#app').innerHTML = `
    ${headerHTML()}
    <section class="card" id="main-card">
      <div class="dropzone" id="dropzone">
        <div class="dropzone-text">${escapeHtml(t('input.drop'))}</div>
        <div class="row center">
          <button class="btn primary" data-act="choose-files">${t('btn.chooseFiles')}</button>
          <button class="btn" data-act="choose-folder">${t('btn.chooseFolder')}</button>
          <button class="btn ghost" data-act="rescan">${t('btn.rescan')}</button>
          <button class="btn ghost" data-act="clear">${t('btn.clear')}</button>
        </div>
      </div>
      ${globalStatusHTML()}
      ${fileListHTML()}
      ${settingsHTML()}
    </section>
    ${backupSection()}
    ${logSection()}`;
  indexRows();
  const el = $('log');
  if (el) el.scrollTop = el.scrollHeight;
  window.scrollTo(0, scrollY);
}

// ---- Targeted DOM updates (streaming never re-renders the whole list) ----

function indexRows() {
  state.rowEls = {};
  const list = $('file-list');
  if (!list) return;
  list.querySelectorAll('.file-row').forEach((el) => {
    state.rowEls[el.dataset.path] = el;
  });
}

function addRowDOM(v) {
  const list = $('file-list');
  if (!list || state.rowEls[v.path]) return;
  const wrap = document.createElement('div');
  wrap.innerHTML = rowHTML(v);
  const el = wrap.firstElementChild;
  list.appendChild(el);
  state.rowEls[v.path] = el;
}

function updateRowDOM(path) {
  const el = state.rowEls[path];
  const v = state.byPath[path];
  if (!el || !v) return;
  const wrap = document.createElement('div');
  wrap.innerHTML = rowHTML(v);
  const fresh = wrap.firstElementChild;
  el.replaceWith(fresh);
  state.rowEls[path] = fresh;
}

function refreshStatusDOM() {
  const el = $('global-status');
  if (el) el.outerHTML = globalStatusHTML();
}

function refreshFixButton() {
  const btn = $('fix');
  if (!btn) return;
  const count = selectedCount();
  btn.textContent = t('btn.fix', { n: count });
  btn.disabled = state.scanning || state.batch.running || count === 0;
}

// ---- Scanning -------------------------------------------------------------

function verdictToRow(v) {
  return {
    path: v.path, name: v.name, size: v.size, status: v.status,
    reasons: v.reasons || [], probe: v.probe || null, error: v.error || '', phase: '',
  };
}

function mergeVerdict(v) {
  let row = state.byPath[v.path];
  if (!row) {
    row = verdictToRow(v);
    state.files.push(row);
    state.byPath[v.path] = row;
  } else {
    row.name = v.name;
    row.size = v.size;
    row.status = v.status;
    row.reasons = v.reasons || [];
    row.probe = v.probe || null;
    row.error = v.error || '';
    row.phase = '';
  }
  if (!state.touched[v.path]) state.selection[v.path] = v.status === 'needs_work';
  return row;
}

// Scans run one after another so a second drop never interleaves its rows.
let scanChain = Promise.resolve();
function scheduleScan(paths, roots) {
  if (!paths || !paths.length) return scanChain;
  scanChain = scanChain.then(() => doScan(paths, roots)).catch(() => {});
  return scanChain;
}

async function doScan(paths, roots) {
  state.scanning = true;
  state.scanPhase = 'collect';
  for (const r of roots && roots.length ? roots : paths) {
    if (!state.scanRoots.includes(r)) state.scanRoots.push(r);
  }
  render();
  log(t('log.scanning', { n: paths.length }));
  try {
    const verdicts = await ScanStream(paths);
    for (const v of verdicts) mergeVerdict(v);
    state.scanning = false;
    state.scanPhase = '';
    sortFiles();
    const ok = state.files.filter((v) => v.status === 'ok').length;
    const needs = state.files.filter((v) => v.status === 'needs_work').length;
    const broken = state.files.filter((v) => v.status === 'broken').length;
    log(t('log.scanned', { ok, needs, broken }));
  } catch (e) {
    state.scanning = false;
    state.scanPhase = '';
    log(t('log.scanFailed', { err: e }));
  }
  render();
}

// ---- Processing -----------------------------------------------------------

async function startFix(explicitPaths) {
  if (state.batch.running || state.scanning) return;
  let targets;
  let noNeed = 0;
  if (explicitPaths) {
    targets = explicitPaths.map((p) => state.byPath[p]).filter(Boolean);
  } else {
    const selected = selectedRows();
    targets = selected.filter((v) => v.status !== 'ok');
    noNeed = selected.filter((v) => v.status === 'ok').length;
  }

  if (!targets.length && noNeed === 0) {
    log(t('summary.nothing'));
    return;
  }
  if (!targets.length) {
    state.batch = { ...emptyBatch(), finished: true, skipped: noNeed };
    state.lastRun = { outcomes: [], kind: state.processMode, inPlace: inplace(), backupDir: state.backupDir, failedPaths: [] };
    render();
    return;
  }

  state.fileStatus = {};
  state.queue = {};
  targets.forEach((v, i) => {
    state.fileStatus[v.path] = { state: 'queued', pos: i + 1 };
    state.queue[v.path] = i + 1;
  });
  state.batch = { ...emptyBatch(), running: true, total: targets.length };
  state.lastRun = null;
  render();
  log(t('log.fixStart', { n: targets.length }));

  const base = {
    inputs: targets.map((v) => v.path),
    outdir: inplace() ? '' : state.outdir,
    inPlace: inplace(),
    backupDir: inplace() ? state.backupDir : '',
    nameRule: state.nameRule,
  };

  let res;
  try {
    if (state.processMode === 'reencode') {
      res = await BatchReencode({
        ...base,
        videoCodec: state.vcodec,
        hardware: state.hw,
        crf: Number(state.crf),
        preset: state.preset,
        audioBitrate: '',
        gopSeconds: Number(state.gop),
      });
    } else {
      res = await BatchNormalize({
        ...base,
        format: state.format,
        windowMs: Number(state.window),
        fragmentMs: Number(state.frag),
      });
    }
  } catch (e) {
    state.batch.running = false;
    log(t('log.fixFailed', { err: e }));
    render();
    return;
  }

  const results = res.results || [];
  for (const o of results) {
    state.fileStatus[o.path] = { state: o.status, error: o.error, gain: o.gain };
  }
  const seen = new Set(results.map((o) => o.path));
  for (const v of targets) {
    if (!seen.has(v.path)) state.fileStatus[v.path] = { state: 'skipped' };
  }

  const b = state.batch;
  b.running = false;
  b.finished = true;
  b.cancelled = !!res.cancelled;
  b.done = res.done || 0;
  b.failed = res.failed || 0;
  b.skipped = (res.skipped || 0) + noNeed;
  b.saved = results.reduce((a, o) => a + (o.gain ? o.gain.beforeSize - o.gain.afterSize : 0), 0);
  state.lastRun = {
    outcomes: results,
    kind: state.processMode,
    inPlace: inplace(),
    backupDir: state.backupDir,
    failedPaths: results.filter((o) => o.status === 'failed').map((o) => o.path),
  };
  log(res.cancelled ? t('log.fixCancelled') : t('log.fixDone', { ok: b.done, fail: b.failed }));
  render();
}

async function retryFailed() {
  const paths = state.lastRun ? state.lastRun.failedPaths : [];
  if (!paths.length) return;
  for (const p of paths) delete state.fileStatus[p];
  state.lastRun = null;
  await startFix(paths);
}

async function retryOne(path) {
  const v = state.byPath[path];
  if (!v) return;
  delete state.fileStatus[path];
  log(t('log.retry', { name: basename(path) }));
  await startFix([path]);
}

async function undoRun() {
  const lr = state.lastRun;
  if (!lr) return;
  const done = lr.outcomes.filter((o) => o.status === 'done');
  if (!done.length) {
    state.lastRun = null;
    state.batch = emptyBatch();
    render();
    return;
  }
  try {
    const r = await UndoBatch(done);
    log(t('log.undoDone', { restored: r.restored, deleted: r.deleted, failed: r.failed }));
  } catch (e) {
    log(t('log.undoFailed', { err: e }));
  }

  const paths = lr.outcomes.map((o) => o.path);
  for (const p of paths) {
    delete state.fileStatus[p];
    const v = state.byPath[p];
    if (v) {
      v.status = null;
      v.phase = 'waiting';
      v.reasons = [];
      v.error = '';
    }
  }
  state.lastRun = null;
  state.batch = emptyBatch();
  render();
  // Re-check the affected originals so the rows describe reality again.
  if (paths.length) scheduleScan(paths, null);
}

// ---- Backup manager -------------------------------------------------------

async function refreshBackups() {
  if (!state.backupDir) return log(t('backup.empty'));
  try {
    const list = await ListBackups(state.backupDir);
    state.backups = (list && list.entries) || [];
    state.backupResolvedDir = (list && list.dir) || '';
    state.backupConfirm = -1;
    render();
    log(t('log.backupListed', { n: state.backups.length }));
  } catch (e) {
    log(t('backup.failed', { err: e }));
  }
}

async function restoreBackupAt(i) {
  const e = state.backups[i];
  if (!e) return;
  try {
    const out = await RestoreBackup(state.backupDir, e.original);
    log(t('backup.restored', { path: e.original }));
    if (out && out.replacedBackup) log(t('backup.restoreNote', { name: out.replacedBackup }));
  } catch (err) {
    log(t('backup.failed', { err }));
  }
  await refreshBackups();
}

async function deleteBackupAt(i) {
  const e = state.backups[i];
  if (!e) return;
  if (state.backupConfirm !== i) {
    state.backupConfirm = i;
    render();
    return;
  }
  try {
    const n = await DeleteBackups(state.backupDir, e.original);
    log(t('backup.deleted', { n }));
  } catch (err) {
    log(t('backup.failed', { err }));
  }
  state.backupConfirm = -1;
  await refreshBackups();
}

// ---- Events ---------------------------------------------------------------

document.addEventListener('click', (e) => {
  const el = e.target.closest('[data-act]');
  if (!el) return;
  const act = el.dataset.act;
  const path = el.dataset.path;
  switch (act) {
    case 'choose-files':
      ChooseInputs().then((paths) => scheduleScan(paths, null));
      break;
    case 'choose-folder':
      ChooseFolder().then((dir) => {
        if (dir) scheduleScan([dir], [dir]);
      });
      break;
    case 'rescan': {
      const paths = state.files.map((v) => v.path);
      if (!paths.length) {
        if (!state.scanRoots.length) return log(t('log.noFile'));
        scheduleScan(state.scanRoots, state.scanRoots);
      } else {
        scheduleScan(paths, state.scanRoots);
      }
      break;
    }
    case 'clear':
      state.files = [];
      state.byPath = {};
      state.rowEls = {};
      state.selection = {};
      state.touched = {};
      state.expanded = {};
      state.fileStatus = {};
      state.queue = {};
      state.scanRoots = [];
      state.batch = emptyBatch();
      state.lastRun = null;
      render();
      log(t('log.cleared'));
      break;
    case 'toggle': {
      state.expanded[path] = !state.expanded[path];
      updateRowDOM(path);
      break;
    }
    case 'retry':
      retryOne(path);
      break;
    case 'retry-failed':
      retryFailed();
      break;
    case 'undo':
      undoRun();
      break;
    case 'fix':
      startFix();
      break;
    case 'cancel':
      CancelBatch();
      break;
    case 'choose-outdir':
      ChooseOutputDir().then((dir) => {
        if (dir) {
          state.outdir = dir;
          render();
        }
      });
      break;
    case 'choose-backup-dir':
    case 'choose-backup-mgmt':
      ChooseOutputDir().then((dir) => {
        if (dir) {
          state.backupDir = dir;
          render();
        }
      });
      break;
    case 'list-backups':
      refreshBackups();
      break;
    case 'backup-restore':
      restoreBackupAt(Number(el.dataset.index));
      break;
    case 'backup-delete':
      deleteBackupAt(Number(el.dataset.index));
      break;
    default:
      break;
  }
});

document.addEventListener('change', (e) => {
  const el = e.target;
  if (el.id === 'lang') {
    switchLang(el.value);
    return;
  }
  if (el.matches('input[name="process"]')) {
    state.processMode = el.value;
    render();
    return;
  }
  if (el.matches('input[name="output-mode"]')) {
    state.outputMode = el.value;
    render();
    return;
  }
  if (el.id === 'format') {
    state.format = el.value;
    render();
    return;
  }
  if (el.id === 'namerule') {
    state.nameRule = el.value;
    return;
  }
  if (el.classList.contains('row-check')) {
    const p = el.dataset.path;
    state.touched[p] = true;
    state.selection[p] = el.checked;
    refreshStatusDOM();
    refreshFixButton();
  }
});

document.addEventListener('input', (e) => {
  const el = e.target;
  const mirror = { window: 'window', frag: 'frag', crf: 'crf', gop: 'gop' };
  if (el.id === 'backup-dir' || el.id === 'backup-mgmt-dir') state.backupDir = el.value;
  else if (mirror[el.id]) state[mirror[el.id]] = el.value;
});

// `toggle` does not bubble, so it is captured at the document to remember a
// collapsed section's state across re-renders (e.g. on a language switch).
document.addEventListener(
  'toggle',
  (e) => {
    const el = e.target;
    if (!el || !el.id) return;
    if (el.id === 'backup-card') state.backupOpen = el.open;
    else if (el.id === 'advanced-norm') state.normAdvanced = el.open;
  },
  true,
);

// The language <select> is inside a details-less header, but a change on the
// card summary (open/close) needs no full render. Those are handled above.

EventsOn('scan:found', (p) => {
  if (!p || !p.path || state.byPath[p.path]) return;
  const row = {
    path: p.path, name: p.name || basename(p.path), size: 0,
    status: null, reasons: [], probe: null, error: '', phase: 'waiting',
  };
  state.files.push(row);
  state.byPath[p.path] = row;
  if (!state.touched[p.path]) state.selection[p.path] = false;
  addRowDOM(row);
  refreshStatusDOM();
});

EventsOn('scan:phase', (p) => {
  if (p && p.phase) {
    state.scanPhase = p.phase;
    refreshStatusDOM();
  }
});

EventsOn('scan:checking', (p) => {
  if (!p || !p.path) return;
  const row = state.byPath[p.path];
  if (row) {
    row.phase = 'checking';
    updateRowDOM(p.path);
  }
  refreshStatusDOM();
});

EventsOn('scan:checked', (p) => {
  if (!p || !p.verdict) return;
  mergeVerdict(p.verdict);
  updateRowDOM(p.verdict.path);
  refreshStatusDOM();
  refreshFixButton();
});

EventsOn('batch:progress', (p) => {
  if (!p || !p.path || !state.batch.running) return;
  const b = state.batch;
  if (p.status === 'running') {
    state.fileStatus[p.path] = { state: 'running', pos: state.queue[p.path] };
    b.currentIndex = p.index;
    b.currentName = basename(p.path);
  } else if (p.status === 'done') {
    state.fileStatus[p.path] = { state: 'done' };
  } else if (p.status === 'failed') {
    state.fileStatus[p.path] = { state: 'failed', error: p.error };
  }
  const sts = Object.values(state.fileStatus);
  b.done = sts.filter((s) => s.state === 'done').length;
  b.failed = sts.filter((s) => s.state === 'failed').length;
  updateRowDOM(p.path);
  refreshStatusDOM();
});

// ---- Bootstrap ------------------------------------------------------------

const log = (msg) => {
  const locale = getLang() === 'zh' || getLang() === 'zh_Hant' ? 'zh-CN' : getLang() === 'ja' ? 'ja-JP' : getLang() === 'ko' ? 'ko-KR' : 'en-US';
  const line = `${new Date().toLocaleTimeString(locale)}  ${msg}`;
  state.logs.push(line);
  const el = $('log');
  if (el) {
    el.textContent += `${line}\n`;
    el.scrollTop = el.scrollHeight;
  }
};

const ffmpegShort = (v) => {
  const m = /version\s+(\S+)/.exec(String(v));
  if (!m) return String(v);
  const num = /\d+(?:\.\d+)+/.exec(m[1]);
  return `ffmpeg ${num ? num[0] : m[1]}`;
};

const ffmpegText = () => {
  if (state.ffmpeg.status === 'ok') return ffmpegShort(state.ffmpeg.version);
  if (state.ffmpeg.status === 'missing') return t('ffmpeg.missing');
  return t('ffmpeg.checking');
};

const ffmpegTitle = () => (state.ffmpeg.status === 'ok' ? state.ffmpeg.version : '');

async function refreshFFmpeg() {
  try {
    const v = await FFmpegVersion();
    state.ffmpeg = { status: 'ok', version: v.split(' Copyright')[0] };
  } catch (e) {
    state.ffmpeg = { status: 'missing', version: '' };
  }
  const el = $('ffmpeg');
  if (el) {
    el.textContent = ffmpegText();
    el.title = ffmpegTitle();
  }
}

async function openInitialTargets() {
  let targets;
  try {
    targets = await InitialTargets();
  } catch (e) {
    return;
  }
  if (Array.isArray(targets) && targets.length) scheduleScan(targets, null);
}

async function switchLang(lang) {
  if (lang === getLang()) return;
  setLang(lang);
  render();
  try {
    await SetLanguage(getLang());
  } catch (e) {
    // Non-fatal: the UI already switched; native dialogs keep the previous language.
  }
}

OnFileDrop((_x, _y, paths) => {
  if (Array.isArray(paths) && paths.length) scheduleScan(paths, null);
}, false);

render();
refreshFFmpeg();
SetLanguage(getLang());
log(t('log.ready'));
openInitialTargets();
