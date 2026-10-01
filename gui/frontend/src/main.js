import './style.css';

import { Probe, Normalize, Reencode, FFmpegVersion, ChooseInput, ChooseOutput, SetLanguage } from '../wailsjs/go/main/App';
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

function render() {
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

  <section class="card">
    <div class="row">
      <label class="field">
        <span>${t('label.input')}</span>
        <input id="input" type="text" placeholder="${escapeAttr(t('input.placeholder'))}" value="${escapeAttr(state.input)}" readonly />
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
        <input id="outnorm" type="text" placeholder="${escapeAttr(t('out.norm.placeholder'))}" value="${escapeAttr(state.outnorm)}" readonly />
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
          <option${state.preset === 'ultrafast' ? ' selected' : ''}>ultrafast</option><option${state.preset === 'veryfast' ? ' selected' : ''}>veryfast</option>
          <option${state.preset === 'medium' ? ' selected' : ''}>medium</option><option${state.preset === 'slow' ? ' selected' : ''}>slow</option>
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
        <input id="outreenc" type="text" placeholder="${escapeAttr(t('out.reenc.placeholder'))}" value="${escapeAttr(state.outreenc)}" readonly />
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

render();
refreshFFmpeg();
SetLanguage(getLang());
log(t('log.ready'));
