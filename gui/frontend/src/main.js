import './style.css';

import { Probe, Normalize, Reencode, FFmpegVersion, ChooseInput, ChooseOutput } from '../wailsjs/go/main/App';
import { EventsOn } from '../wailsjs/runtime/runtime';

const state = { input: '', output: '' };

document.querySelector('#app').innerHTML = `
  <header>
    <h1>mp4norm</h1>
    <span id="ffmpeg" class="muted">checking ffmpeg…</span>
  </header>

  <section class="card">
    <div class="row">
      <input id="input" type="text" placeholder="choose an MP4 file…" readonly />
      <button id="choose" class="btn">Choose file…</button>
      <button id="inspect" class="btn">Inspect</button>
    </div>
  </section>

  <section class="card" id="report-card" hidden>
    <h2>Diagnosis</h2>
    <pre id="report"></pre>
  </section>

  <section class="card">
    <h2>Normalize <span class="muted">lossless — faststart + interleave</span></h2>
    <div class="row">
      <label>Format
        <select id="format">
          <option value="progressive">progressive (faststart + interleave)</option>
          <option value="fmp4">fragmented (fMP4 + sidx)</option>
        </select>
      </label>
      <label id="window-label">Window (ms)
        <input id="window" type="number" value="1000" min="200" step="100" />
      </label>
      <label id="frag-label" hidden>Fragment (ms)
        <input id="frag" type="number" value="2000" min="500" step="500" />
      </label>
    </div>
    <div class="row">
      <input id="outnorm" type="text" placeholder="output (default: <input>.norm.mp4)" readonly />
      <button id="savenorm" class="btn ghost">Save as…</button>
      <button id="normalize" class="btn primary">Normalize</button>
    </div>
  </section>

  <section class="card">
    <h2>Re-encode <span class="muted">optional — fix sparse keyframes / VFR</span></h2>
    <div class="row">
      <label>Codec
        <select id="vcodec">
          <option value="h264">H.264</option>
          <option value="h265">H.265</option>
          <option value="copy">copy</option>
        </select>
      </label>
      <label>Hardware
        <select id="hw">
          <option value="auto">auto</option>
          <option value="on">on</option>
          <option value="off">off</option>
        </select>
      </label>
      <label>CRF
        <input id="crf" type="number" value="23" min="0" max="51" />
      </label>
      <label>Preset
        <select id="preset">
          <option>ultrafast</option><option>veryfast</option>
          <option selected>medium</option><option>slow</option>
        </select>
      </label>
      <label>GOP (s)
        <input id="gop" type="number" value="2" min="0.5" step="0.5" />
      </label>
    </div>
    <div class="row">
      <input id="outreenc" type="text" placeholder="output (default: <input>.reenc.mp4)" readonly />
      <button id="savetreenc" class="btn ghost">Save as…</button>
      <button id="reencode" class="btn">Re-encode</button>
    </div>
    <progress id="progress" max="1" value="0"></progress>
  </section>

  <section class="card">
    <h2>Log</h2>
    <pre id="log" class="log"></pre>
  </section>
`;

const $ = (id) => document.getElementById(id);
const log = (msg) => {
  const el = $('log');
  el.textContent += `${new Date().toLocaleTimeString()}  ${msg}\n`;
  el.scrollTop = el.scrollHeight;
};

const fmt = (report) => {
  if (!report) return '';
  const lines = [
    `file        ${report.Path}`,
    `size        ${report.FileSize} bytes`,
    `brand       ${report.MajorBrand}`,
    `moov        ${report.MoovPosition}`,
    `fragmented  ${report.Fragmented}`,
    `mdat boxes  ${report.MdatCount}`,
  ];
  for (const f of report.Findings || []) {
    lines.push(`[${f.Severity}] ${f.Code}: ${f.Message}`);
  }
  return lines.join('\n');
};

const showResult = (res) => {
  $('report-card').hidden = false;
  $('report').textContent = fmt(res.report || res.Report);
  log(`done -> ${res.output || res.Output}`);
};

async function refreshFFmpeg() {
  try {
    const v = await FFmpegVersion();
    $('ffmpeg').textContent = v.split(' Copyright')[0];
  } catch (e) {
    $('ffmpeg').textContent = `ffmpeg not found (${e})`;
  }
}

$('choose').onclick = async () => {
  const path = await ChooseInput();
  if (!path) return;
  state.input = path;
  state.output = '';
  $('input').value = path;
  $('outnorm').value = '';
  $('outreenc').value = '';
  log(`selected ${path}`);
};

$('inspect').onclick = async () => {
  if (!state.input) return log('no file selected');
  try {
    const rep = await Probe(state.input);
    $('report-card').hidden = false;
    $('report').textContent = fmt(rep);
    log('inspected');
  } catch (e) {
    log(`inspect failed: ${e}`);
  }
};

$('format').onchange = () => {
  const fmp4 = $('format').value === 'fmp4';
  $('frag-label').hidden = !fmp4;
  $('window-label').hidden = fmp4;
};

$('savenorm').onclick = async () => {
  const p = await ChooseOutput('output.mp4');
  if (p) $('outnorm').value = p;
};

$('savetreenc').onclick = async () => {
  const p = await ChooseOutput('output.reenc.mp4');
  if (p) $('outreenc').value = p;
};

$('normalize').onclick = async () => {
  if (!state.input) return log('no file selected');
  log('normalizing…');
  try {
    const res = await Normalize({
      input: state.input,
      output: $('outnorm').value,
      format: $('format').value,
      windowMs: Number($('window').value),
      fragmentMs: Number($('frag').value),
    });
    showResult(res);
  } catch (e) {
    log(`normalize failed: ${e}`);
  }
};

$('reencode').onclick = async () => {
  if (!state.input) return log('no file selected');
  log('re-encoding…');
  $('progress').value = 0;
  try {
    const res = await Reencode({
      input: state.input,
      output: $('outreenc').value,
      videoCodec: $('vcodec').value,
      hardware: $('hw').value,
      crf: Number($('crf').value),
      preset: $('preset').value,
      gopSeconds: Number($('gop').value),
    });
    showResult(res);
  } catch (e) {
    log(`re-encode failed: ${e}`);
  }
};

EventsOn('reencode:progress', (p) => {
  if (!p) return;
  const secs = (p.OutTime || 0) / 1e6;
  $('progress').value = Math.min(0.99, secs / 100);
  $('progress').title = `${secs.toFixed(1)}s  frame ${p.Frame}  ${p.Speed}`;
});

refreshFFmpeg();
log('ready');
