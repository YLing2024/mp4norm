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
  ListBackups,
  RestoreBackup,
  DeleteBackups,
} from '../wailsjs/go/main/App';
import { EventsOn, OnFileDrop } from '../wailsjs/runtime/runtime';
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
  gain: null,
  // Collapsible sections. Kept in memory only (no persistence): every launch
  // starts collapsed, but a re-render (e.g. language switch) must not re-close
  // a section the user just opened.
  singleOpen: false,
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
  // Output mode is shared by the batch list and the single-file card:
  // 'new' writes a new file, 'inplace' replaces originals after a backup.
  outputMode: 'new',
  backupDir: '',
  fileStatus: {}, // path -> { state, error, pct, gain }
  batch: emptyBatch(),

  // Backup management.
  backupOpen: false,
  backups: [], // []main.BackupEntry
  backupResolvedDir: '',
  backupConfirm: -1, // index into state.backups pending delete confirmation
};

// emptyBatch returns a fresh batch-progress record. Every reset (clear, scan,
// run start) shares this shape so the running-file fields can never go stale.
function emptyBatch() {
  return {
    running: false, done: 0, skipped: 0, failed: 0, total: 0,
    finished: false, cancelled: false, beforeFirstPlay: 0, afterFirstPlay: 0,
    currentIndex: 0, currentName: '',
  };
}

const $ = (id) => document.getElementById(id);

// basename keeps only the file name for compact progress / log lines.
const basename = (p) => String(p).split(/[\\/]/).pop();

const escapeHtml = (s) =>
  String(s).replace(/[&<>]/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;' }[c]));
const escapeAttr = (s) => escapeHtml(s).replace(/"/g, '&quot;');

// info renders the ⓘ toggle shown next to a parameter label. The full
// explanation lives in the popover and only appears on hover, keyboard focus
// or click — the always-visible hint stays one short line.
const info = (key) =>
  `<details class="info"><summary aria-label="${escapeAttr(t('info.aria'))}">ⓘ</summary>` +
  `<span class="info-pop" role="tooltip">${escapeHtml(t(key))}</span></details>`;

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

// gainText renders the before/after container improvement exactly like the CLI
// summary. A nil gain yields no lines so callers can skip it entirely.
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

// inplace is true when the shared output-mode choice is "replace originals".
const inplace = () => state.outputMode === 'inplace';

// backupDirDisplay names the in-place backup destination for the warning banner.
const backupDirDisplay = () => state.backupDir || t('batch.inplace.defaultDir');

// backupDirRow is the backup-folder picker shown inside the in-place mode of
// the output-mode group. The element ids are parameterised because both the
// batch and single-file cards render their own copy bound to the shared state.
const backupDirRow = (inputId, buttonId) => `
  <div class="row">
    <label class="field">
      <span>${t('batch.inplace.backupdir')}</span>
      <input id="${inputId}" type="text" placeholder="${escapeAttr(
        t('batch.inplace.backupdir.placeholder'),
      )}" value="${escapeAttr(state.backupDir)}" />
    </label>
    <button id="${buttonId}" class="btn ghost">${t('btn.chooseBackupDir')}</button>
  </div>`;

const inplaceWarning = () =>
  `<div class="warning-banner" role="alert">${escapeHtml(
    t('batch.inplace.warning', { dir: backupDirDisplay() }),
  )}</div>`;

// outputModeGroup is the shared new-file / replace-in-place picker. The radio
// choice and the field it controls live inside ONE border (the dependent field
// is indented below the radios) so they read as a single setting rather than
// two unrelated ones. The replace hint only appears in replace mode; "new file"
// mode shows no explanatory sentence at all.
//   - batch:  output folder (new)       / backup folder + warning (inplace)
//   - single: output path (new)         / backup folder + warning (inplace)
const outputModeGroup = (name, withHint, fields) => `
  <fieldset class="field output-mode">
    <legend>${t('batch.mode.label')}</legend>
    <div class="mode-radios">
      <label class="radio"><input type="radio" name="${name}" value="new"${
        inplace() ? '' : ' checked'
      } /> ${t('batch.mode.new')}</label>
      <label class="radio"><input type="radio" name="${name}" value="inplace"${
        inplace() ? ' checked' : ''
      } /> ${t('batch.mode.inplace')}</label>
    </div>
    ${
      withHint && inplace()
        ? `<span class="hint">${escapeHtml(t('batch.mode.hint'))}</span>`
        : ''
    }
    <div class="mode-fields">${fields}</div>
  </fieldset>`;

// batchModeFields is the output-mode-dependent field of the batch card.
const batchModeFields = () =>
  inplace()
    ? `${backupDirRow('batch-backup-dir', 'choose-batch-backup-dir')}${inplaceWarning()}`
    : `<div class="row">
      <label class="field">
        <span>${t('batch.outdir')}</span>
        <input id="outdir" type="text" placeholder="${escapeAttr(
          t('batch.outdir.placeholder'),
        )}" value="${escapeAttr(state.outdir)}" readonly />
      </label>
      <button id="choose-outdir" class="btn ghost">${t('btn.chooseOutdir')}</button>
    </div>`;

// singleModeFields is the output-mode-dependent field of the single-file card.
const singleModeFields = () =>
  inplace()
    ? `${backupDirRow('single-backup-dir', 'choose-single-backup-dir')}${inplaceWarning()}`
    : `<div class="row">
      <label class="field">
        <span>${t('label.output')}</span>
        <input id="outnorm" type="text" placeholder="${escapeAttr(
          t('out.norm.placeholder'),
        )}" value="${escapeAttr(state.outnorm)}" readonly />
      </label>
      <button id="savenorm" class="btn ghost">${t('btn.saveas')}</button>
    </div>`;

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
  const st = state.fileStatus[v.path];
  const lines = [`${t('batch.detail.path')}: ${v.path}`];
  if (st && st.gain) lines.push('', gainText(st.gain));
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
  // Idle rows (just detected, not yet processed) show a dash so the column
  // never looks like an unfinished/blank load.
  if (!st) return '—';
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
      return '—';
  }
};

// needsWorkFiles counts the files the batch button would still process. A file
// that just failed normalization is excluded (re-scan the list to retry it), so
// a finished batch with only failures and successes disables the button.
const needsWorkFiles = () =>
  state.files.filter(
    (v) => v.status === 'needs_work' && state.fileStatus[v.path]?.state !== 'failed',
  );

// verdictRank orders the default listing by how much a file needs the user:
// broken first, then needs-work, then fine. Unknown statuses sort last.
const VERDICT_RANK = { broken: 0, needs_work: 1, ok: 2 };
const verdictRank = (status) => VERDICT_RANK[status] ?? 3;

const visibleFiles = () => {
  const list = state.files.filter((v) => {
    if (state.filter === 'needs') return v.status === 'needs_work';
    if (state.filter === 'broken') return v.status === 'broken';
    return true;
  });
  // The size checkbox overrides the default priority order entirely: "sort by
  // size" means exactly that, with no verdict grouping.
  if (state.sortBySize) return [...list].sort((a, b) => b.size - a.size);
  // Otherwise group by verdict priority and keep each group in file-name order.
  return [...list].sort(
    (a, b) => verdictRank(a.status) - verdictRank(b.status) || a.name.localeCompare(b.name),
  );
};

// ffmpegShort renders the header label as just "ffmpeg <version>". The full
// `ffmpeg -version` banner also carries build-vendor noise (e.g.
// "9.0.2-essentials_build-www.gyan.dev") that means nothing to users and
// stretches the top bar, so the header keeps only the bare version number.
const ffmpegShort = (v) => {
  const m = /version\s+(\S+)/.exec(String(v));
  if (!m) return String(v);
  // Keep only the dotted release number ("9.0.2"), dropping vendor/build
  // suffixes such as "-essentials_build-www.gyan.dev"; git builds without a
  // dotted number keep their full token so we never invent a version.
  const num = /\d+(?:\.\d+)+/.exec(m[1]);
  return `ffmpeg ${num ? num[0] : m[1]}`;
};

const ffmpegText = () => {
  if (state.ffmpeg.status === 'ok') return ffmpegShort(state.ffmpeg.version);
  if (state.ffmpeg.status === 'missing') return t('ffmpeg.missing');
  return t('ffmpeg.checking');
};

// ffmpegTitle exposes the full version banner as the header tooltip.
const ffmpegTitle = () => (state.ffmpeg.status === 'ok' ? state.ffmpeg.version : '');

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
      const st = state.fileStatus[v.path];
      const retry =
        st && st.state === 'failed'
          ? ` <button type="button" class="btn ghost retry-btn" data-retry="${escapeAttr(
              v.path,
            )}">${t('btn.retry')}</button>`
          : '';
      const main = `
        <tr class="batch-row verdict-${v.status}" data-path="${escapeAttr(v.path)}">
          <td class="col-name" title="${escapeAttr(v.path)}">${escapeHtml(relDisplay(v))}</td>
          <td class="col-size">${humanBytes(v.size)}</td>
          <td class="col-verdict">${statusText(v.status)}</td>
          <td class="col-reason">${escapeHtml(reasonText(v))}</td>
          <td class="col-state">${escapeHtml(stateText(v))}${retry}</td>
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
  const current =
    b.running && b.currentName
      ? `<div class="muted batch-progress-text">${escapeHtml(
          t('batch.progress.current', { index: b.currentIndex, total: b.total, name: b.currentName }),
        )}</div>`
      : '';
  const progress = b.running || b.finished
    ? `${current}
       <progress id="batch-progress" max="${Math.max(b.total, 1)}" value="${b.done + b.failed}"></progress>
       <div class="muted batch-progress-text">${escapeHtml(
         t('batch.progress', { done: b.done + b.failed, total: b.total }),
       )} ${escapeHtml(summary)}</div>`
    : '';
  const gainSummary =
    b.finished && !b.cancelled && b.done > 0 && b.beforeFirstPlay > 0
      ? `<div class="muted batch-progress-text">${escapeHtml(
          t('gain.summary', {
            n: b.done,
            before: humanBytes(b.beforeFirstPlay),
            after: humanBytes(b.afterFirstPlay),
          }),
        )}</div>`
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
      <span class="batch-count muted">${escapeHtml(t('batch.count', { total, needs }))}</span>
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
    ${gainSummary}
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

// backupSection renders the backup manager: a directory picker, the resolved
// store path and one row per backup with restore / delete actions.
const backupSection = () => {
  const locale = getLang() === 'zh' ? 'zh-CN' : 'en-US';
  const rows = state.backups
    .map((e, i) => {
      const confirming = state.backupConfirm === i;
      const count = state.backups.filter((x) => x.original === e.original).length;
      const stateLabel = e.originalExists ? t('backup.state.exists') : t('backup.state.missing');
      const missing = e.backupExists
        ? ''
        : ` <span class="muted">(${t('backup.state.backupMissing')})</span>`;
      const when = e.createdUnixMs ? new Date(e.createdUnixMs).toLocaleString(locale) : '';
      const confirmHint = confirming
        ? `<div class="muted backup-confirm-hint">${escapeHtml(
            t('backup.deleteConfirm', { n: count }),
          )}</div>`
        : '';
      return `
        <tr>
          <td class="col-name" title="${escapeAttr(e.original)}">${escapeHtml(e.original)}</td>
          <td>${escapeHtml(e.backup)}${missing}</td>
          <td class="col-size">${humanBytes(e.size)}</td>
          <td class="col-time">${escapeHtml(when)}</td>
          <td class="col-state">${escapeHtml(stateLabel)}</td>
          <td class="col-actions">
            <button type="button" class="btn ghost" data-backup-restore="${i}">${t('btn.restore')}</button>
            <button type="button" class="btn ${
              confirming ? 'primary' : 'ghost'
            }" data-backup-delete="${i}">${
              confirming ? t('btn.confirmDelete') : t('btn.deleteBackup')
            }</button>
            ${confirmHint}
          </td>
        </tr>`;
    })
    .join('');

  const body = state.backups.length
    ? `
      <div class="muted backup-resolved">${escapeHtml(
        t('backup.resolved', { dir: state.backupResolvedDir }),
      )}</div>
      <table class="backup-table">
        <thead>
          <tr>
            <th>${t('backup.col.original')}</th>
            <th>${t('backup.col.backup')}</th>
            <th>${t('backup.col.size')}</th>
            <th>${t('backup.col.time')}</th>
            <th>${t('backup.col.state')}</th>
            <th>${t('backup.col.actions')}</th>
          </tr>
        </thead>
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
        <button id="choose-backup-mgmt-dir" class="btn ghost">${t('btn.chooseBackupDir')}</button>
        <button id="list-backups" class="btn">${t('btn.listBackups')}</button>
      </div>
      ${body}
    </details>`;
};

function render() {
  const scrollY = window.scrollY;
  document.querySelector('#app').innerHTML = `
  <header>
    <h1>${t('app.name')}</h1>
    <span id="ffmpeg" class="muted" title="${escapeAttr(ffmpegTitle())}">${escapeHtml(ffmpegText())}</span>
    <div id="lang-switch" class="lang-switch">
      ${langButton('zh', '中文')}
      ${langButton('en', 'English')}
    </div>
  </header>

  <section class="card batch-card" id="batch-card">
    <h2>${t('batch.card')}</h2>
    <div class="row">
      <button id="choose-folder" class="btn primary">${t('btn.chooseFolder')}</button>
      <button id="choose-files" class="btn">${t('btn.chooseFiles')}</button>
      <button id="clear-list" class="btn">${t('btn.clear')}</button>
      <button id="scan-again" class="btn">${t('btn.scan')}</button>
    </div>
    <div class="row">
      ${outputModeGroup('output-mode-batch', true, batchModeFields())}
    </div>
    ${batchSection()}
  </section>

  <details class="card single-card" id="single-card"${state.singleOpen ? ' open' : ''}>
    <summary class="card-summary"><h2>${t('single.card')}</h2></summary>

    <div class="row">
      <label class="field">
        <span>${t('label.input')}</span>
        <input id="input" type="text" placeholder="${escapeAttr(t('input.placeholder'))}" value="${escapeAttr(
          state.input,
        )}" readonly />
      </label>
      <button id="choose" class="btn">${t('btn.choose')}</button>
      <button id="inspect" class="btn">${t('btn.inspect')}</button>
    </div>

    <div class="subpane" id="report-card" ${state.showReport ? '' : 'hidden'}>
      <h3>${t('card.diagnosis')}</h3>
      ${state.gain ? `<pre class="gain">${escapeHtml(gainText(state.gain))}</pre>` : ''}
      <pre id="report">${escapeHtml(state.report ? fmt(state.report) : '')}</pre>
    </div>

    <div class="subpane">
      <h3>${t('card.normalize')}</h3>
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
      <div class="row">
        ${outputModeGroup('output-mode-single', false, singleModeFields())}
      </div>
      <div class="row">
        <button id="normalize" class="btn primary">${t('btn.normalize')}</button>
      </div>
      <details class="advanced" id="advanced-norm"${state.normAdvanced ? ' open' : ''}>
        <summary>${t('advanced.norm')}</summary>
        <div class="row">
          <label id="window-label" class="field"${state.format === 'fmp4' ? ' hidden' : ''}>
            <span>${t('label.window')} ${info('info.window')}</span>
            <input id="window" type="number" value="${escapeAttr(state.window)}" min="200" step="100" />
            <span class="hint">${escapeHtml(t('hint.window'))}</span>
          </label>
          <label id="frag-label" class="field"${state.format === 'fmp4' ? '' : ' hidden'}>
            <span>${t('label.frag')} ${info('info.frag')}</span>
            <input id="frag" type="number" value="${escapeAttr(state.frag)}" min="500" step="500" />
            <span class="hint">${escapeHtml(t('hint.frag'))}</span>
          </label>
        </div>
      </details>
    </div>

    <details class="subdetails" id="reencode-card"${state.reencodeOpen ? ' open' : ''}>
      <summary>
        <h3>${t('card.reencode')} <span class="hint">${escapeHtml(t('card.reencode.sub'))}</span></h3>
      </summary>
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
          <span>${t('label.gop')} ${info('info.gop')}</span>
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
        </label>
        <button id="savetreenc" class="btn ghost">${t('btn.saveas')}</button>
        <button id="reencode" class="btn">${t('btn.reencode')}</button>
      </div>
      <progress id="progress" max="1" value="0"></progress>
    </details>
  </details>

  ${backupSection()}

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
  const single = $('single-card');
  if (single) single.ontoggle = () => { state.singleOpen = single.open; };
  const adv = $('advanced-norm');
  if (adv) adv.ontoggle = () => { state.normAdvanced = adv.open; };
  const re = $('reencode-card');
  if (re) re.ontoggle = () => { state.reencodeOpen = re.open; };
  const bk = $('backup-card');
  if (bk) bk.ontoggle = () => { state.backupOpen = bk.open; };

  // The output-mode radios appear in both cards but edit one shared state.
  document
    .querySelectorAll('input[name="output-mode-batch"], input[name="output-mode-single"]')
    .forEach((el) => {
      el.onchange = () => {
        state.outputMode = el.value;
        render();
      };
    });

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
    state.batch = emptyBatch();
    render();
    log(t('log.cleared'));
  };
  const sc = $('scan-again');
  if (sc) sc.onclick = () => {
    // Re-scan exactly what the list currently holds, keeping the original
    // scan roots so a folder-relative list still shows subpaths afterwards.
    const listPaths = state.files.map((v) => v.path);
    const paths = listPaths.length ? listPaths : state.scanRoots;
    if (!paths.length) return log(t('log.noFile'));
    scanPaths(paths, state.scanRoots);
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
      const retryBtn = e.target.closest('button[data-retry]');
      if (retryBtn) {
        e.stopPropagation();
        retryOne(retryBtn.dataset.retry);
        return;
      }
      const row = e.target.closest('tr[data-path]');
      if (!row) return;
      const p = row.dataset.path;
      state.expanded[p] = !state.expanded[p];
      render();
    };
  }

  // ---- Backup manager ----------------------------------------------------
  const chooseBackupFt = async () => {
    const dir = await ChooseOutputDir();
    if (dir) {
      state.backupDir = dir;
      render();
    }
  };
  const cbb = $('choose-batch-backup-dir');
  if (cbb) cbb.onclick = chooseBackupFt;
  const csb = $('choose-single-backup-dir');
  if (csb) csb.onclick = chooseBackupFt;
  const cbm = $('choose-backup-mgmt-dir');
  if (cbm) cbm.onclick = chooseBackupFt;
  const lb = $('list-backups');
  if (lb) lb.onclick = () => refreshBackups();

  const btable = document.querySelector('.backup-table');
  if (btable) {
    btable.onclick = (e) => {
      const restoreBtn = e.target.closest('button[data-backup-restore]');
      if (restoreBtn) {
        restoreBackupAt(Number(restoreBtn.dataset.backupRestore));
        return;
      }
      const delBtn = e.target.closest('button[data-backup-delete]');
      if (delBtn) {
        deleteBackupAt(Number(delBtn.dataset.backupDelete));
        return;
      }
      if (state.backupConfirm !== -1) {
        state.backupConfirm = -1;
        render();
      }
    };
  }

  $('choose').onclick = async () => {
    const path = await ChooseInput();
    if (!path) return;
    state.input = path;
    state.outnorm = '';
    state.outreenc = '';
    state.gain = null;
    render();
    log(t('log.selected', { path }));
  };

  $('inspect').onclick = async () => {
    if (!state.input) return log(t('log.noFile'));
    try {
      state.report = await Probe(state.input);
      state.gain = null;
      state.showReport = true;
      state.singleOpen = true;
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
  mirror('batch-backup-dir', 'backupDir');
  mirror('single-backup-dir', 'backupDir');
  mirror('backup-mgmt-dir', 'backupDir');

  const sn = $('savenorm');
  if (sn) sn.onclick = async () => {
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
        output: inplace() ? '' : state.outnorm,
        format: state.format,
        windowMs: Number(state.window),
        fragmentMs: Number(state.frag),
        inPlace: inplace(),
        backupDir: inplace() ? state.backupDir : '',
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
    // Successful rows are keyed by their (possibly new) output path and keep
    // the before/after gain so the detail block can show the numbers.
    else if (o.status === 'done') status[o.output || o.path] = { state: 'done', gain: o.gain };
  }
  state.fileStatus = status;
}

async function scanPaths(paths, roots) {
  if (!paths || !paths.length) return;
  state.scanning = true;
  // roots (when provided) preserve the original folder context for relative
  // display; a re-scan of concrete file paths passes the previous roots.
  state.scanRoots = roots && roots.length ? [...roots] : [...paths];
  render();
  log(t('log.scanning', { n: paths.length }));
  try {
    const verdicts = await ScanPaths(paths);
    state.files = verdicts || [];
    state.fileStatus = {};
    state.expanded = {};
    state.scanning = false;
    state.batch = emptyBatch();
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
  state.batch = { ...emptyBatch(), running: true, total: targets.length };
  render();
  log(t('log.batchStart', { n: targets.length }));

  let res;
  try {
    res = await BatchNormalize({
      inputs: targets.map((v) => v.path),
      outdir: inplace() ? '' : state.outdir,
      format: state.format,
      windowMs: Number(state.window),
      fragmentMs: Number(state.frag),
      inPlace: inplace(),
      backupDir: inplace() ? state.backupDir : '',
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
    state.fileStatus[o.output || o.path] = { state: o.status, error: o.error, gain: o.gain };
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
  state.batch.beforeFirstPlay = res.beforeFirstPlay || 0;
  state.batch.afterFirstPlay = res.afterFirstPlay || 0;
  log(
    res.cancelled
      ? t('log.batchCancelled')
      : t('log.batchDone', { ok: state.batch.done, fail: state.batch.failed }),
  );
  await refreshAfterBatch(res);
  log(t('log.batchRefreshed'));
  render();
}

// retryOne re-runs a single failed file through the same batch pipeline and
// updates just that row (successful retries swap in the freshly scanned
// output), so the rest of the list keeps its existing state.
async function retryOne(path) {
  const v = state.files.find((x) => x.path === path);
  if (!v) return;
  state.fileStatus[path] = { state: 'queued' };
  state.batch = { ...emptyBatch(), running: true, total: 1, currentIndex: 1, currentName: basename(path) };
  render();
  log(t('log.retry', { name: basename(path) }));

  let res;
  try {
    res = await BatchNormalize({
      inputs: [path],
      outdir: inplace() ? '' : state.outdir,
      format: state.format,
      windowMs: Number(state.window),
      fragmentMs: Number(state.frag),
      inPlace: inplace(),
      backupDir: inplace() ? state.backupDir : '',
    });
  } catch (e) {
    state.batch = { ...emptyBatch(), finished: true, failed: 1, total: 1 };
    state.fileStatus[path] = { state: 'failed', error: String(e) };
    log(t('log.retryFailed', { err: e }));
    render();
    return;
  }

  const o = (res.results || [])[0] || { status: 'failed', error: '' };
  state.batch = {
    ...emptyBatch(),
    finished: true,
    total: 1,
    done: o.status === 'done' ? 1 : 0,
    failed: o.status === 'failed' ? 1 : 0,
  };

  if (o.status === 'done') {
    const out = o.output || path;
    let refreshed;
    try {
      refreshed = await ScanPaths([out]);
    } catch (e) {
      refreshed = [];
    }
    const nv = (refreshed || [])[0];
    if (nv) {
      state.files = state.files.map((x) => (x.path === path ? nv : x));
      const seen = new Set();
      state.files = state.files.filter((x) => {
        const key = toSlashes(x.path).toLowerCase();
        if (seen.has(key)) return false;
        seen.add(key);
        return true;
      });
      delete state.fileStatus[path];
      state.fileStatus[nv.path] = { state: 'done', gain: o.gain };
    } else {
      state.fileStatus[path] = { state: 'done', gain: o.gain };
    }
    log(t('log.retryDone', { name: basename(out) }));
  } else {
    state.fileStatus[path] = { state: 'failed', error: o.error };
    log(t('log.retryFailed', { err: o.error }));
  }
  render();
}

// ---- Backup manager actions ---------------------------------------------

// refreshBackups lists the backups for the chosen directory and re-renders.
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

// deleteBackupAt is two-step: the first click arms the row, the second deletes
// every backup of that original (forget), matching the CLI's semantics.
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

const showResult = (res) => {
  state.report = res.report || res.Report;
  state.gain = res.gain || res.Gain || null;
  state.showReport = true;
  state.singleOpen = true;
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
  if (el) {
    el.textContent = ffmpegText();
    el.title = ffmpegTitle();
  }
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

// Batch progress: update the per-row status column, the running-file line and
// the overall bar.
EventsOn('batch:progress', (p) => {
  if (!p) return;
  if (p.status === 'running') {
    const pct = p.total > 0 ? Math.round(((p.index - 1) / p.total) * 100) : 0;
    state.fileStatus[p.path] = { state: 'running', pct };
    state.batch.currentIndex = p.index;
    state.batch.currentName = basename(p.path);
  } else if (p.status === 'done') {
    state.batch.done++;
    state.fileStatus[p.path] = { state: 'done' };
  } else if (p.status === 'failed') {
    state.batch.failed++;
    state.fileStatus[p.path] = { state: 'failed', error: p.error };
  }
  render();
});

// Native drag & drop: register the Wails drop handler so dropping files or
// folders anywhere in the window goes through the same scan path as the
// "choose" buttons. useDropTarget=false means any drop is accepted.
OnFileDrop((_x, _y, paths) => {
  if (Array.isArray(paths) && paths.length) scanPaths(paths);
}, false);

render();
refreshFFmpeg();
SetLanguage(getLang());
log(t('log.ready'));
openInitialTargets();
