import {
  GetState,
  OpenFolder,
  OpenLogs,
  ChangeFolder,
  QuitApp,
} from '../wailsjs/go/main/App';
import { EventsOn } from '../wailsjs/runtime/runtime';

// -- Icons (inline SVG strings to avoid asset round-trips)
const ICON_COPY = '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="9" y="9" width="13" height="13" rx="2"/><path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"/></svg>';
const ICON_CHECK = '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><polyline points="20 6 9 17 4 12"/></svg>';

// -- DOM handles
const $ = (id) => document.getElementById(id);
const pill = $('pill-label');
const banner = $('banner');
const errDetail = $('err-detail');
const rows = $('rows');
const ipHint = $('ip-hint');
const folderPath = $('folder-path');
const activityEl = $('activity');
const versionEl = $('version');
const toast = $('toast');

// -- Toast helper
let toastTimer = null;
function showToast(msg) {
  toast.textContent = msg;
  toast.classList.add('show');
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => toast.classList.remove('show'), 1400);
}

// -- Copy button factory
function copyButton(value) {
  const btn = document.createElement('button');
  btn.className = 'btn-copy';
  btn.setAttribute('aria-label', 'Copier ' + value);
  btn.dataset.value = value;
  btn.innerHTML = ICON_COPY;
  btn.addEventListener('click', async () => {
    try {
      await navigator.clipboard.writeText(value);
      btn.classList.add('ok');
      btn.innerHTML = ICON_CHECK;
      showToast('Copié : ' + value);
      setTimeout(() => {
        btn.classList.remove('ok');
        btn.innerHTML = ICON_COPY;
      }, 1400);
    } catch {
      showToast('Copie échouée');
    }
  });
  return btn;
}

// -- Build the credentials grid from backend state
function renderRows(state) {
  rows.innerHTML = '';
  const ips = (state.ips && state.ips.length) ? state.ips : ['127.0.0.1'];
  const ipHeading = ips.length > 1 ? 'Adresses IP' : 'Adresse IP';
  ips.forEach((ip, i) => {
    appendRow(i === 0 ? ipHeading : '', ip);
  });
  appendRow('Port', String(state.port));
  appendRow('Utilisateur', state.user);
  appendRow('Mot de passe', state.pass);

  ipHint.hidden = ips.length <= 1;
}

function appendRow(label, value) {
  const l = document.createElement('div');
  l.className = 'row-label';
  l.textContent = label;

  const v = document.createElement('div');
  v.className = 'row-value';
  v.textContent = value;

  rows.appendChild(l);
  rows.appendChild(v);
  rows.appendChild(copyButton(value));
}

// -- Humanized "il y a …"
function humanize(ms) {
  if (!ms) return null;
  const d = Date.now() - ms;
  const s = Math.floor(d / 1000);
  if (s < 10) return "à l'instant";
  if (s < 60) return `il y a ${s}s`;
  const min = Math.floor(s / 60);
  if (min < 60) return `il y a ${min} min`;
  const h = Math.floor(min / 60);
  if (h < 24) return `il y a ${h}h`;
  return `il y a ${Math.floor(h / 24)}j`;
}

// -- Activity rendering
let lastActivity = { name: '', at: 0 };
function renderActivity() {
  if (!lastActivity.at) {
    activityEl.textContent = 'Aucun fichier reçu depuis le démarrage.';
    return;
  }
  const when = humanize(lastActivity.at);
  activityEl.innerHTML = `Dernier fichier reçu : <span class="name"></span> (${when})`;
  activityEl.querySelector('.name').textContent = lastActivity.name;
}

function flashActivity() {
  activityEl.classList.remove('flash');
  void activityEl.offsetWidth;
  activityEl.classList.add('flash');
}

// -- Status rendering
function applyStatus({ online, badge, detail }) {
  document.body.className = online ? 'state-online' : 'state-error';
  pill.textContent = online ? 'SERVEUR EN LIGNE' : (badge || 'SERVEUR ARRÊTÉ');
  if (online) {
    banner.hidden = true;
  } else {
    banner.hidden = false;
    errDetail.textContent = detail || '';
  }
}

// -- Initial load
async function boot() {
  let state;
  try {
    state = await GetState();
  } catch (e) {
    console.error('GetState failed', e);
    return;
  }

  folderPath.textContent = state.folder || '';
  versionEl.textContent = state.version || '';
  renderRows(state);

  lastActivity = { name: state.lastFile || '', at: state.lastAtMs || 0 };
  renderActivity();

  applyStatus({
    online: state.online,
    badge: state.errorBadge,
    detail: state.errorDetail,
  });

  // Refresh "il y a X" every 30s
  setInterval(renderActivity, 30_000);
}

// -- Event subscriptions (Go -> JS)
EventsOn('activity', (data) => {
  lastActivity = { name: data.name, at: data.atMs };
  renderActivity();
  flashActivity();
});

EventsOn('status', (data) => {
  applyStatus(data);
});

// -- Buttons
$('btn-open').addEventListener('click', () => OpenFolder());
$('btn-logs').addEventListener('click', () => OpenLogs());
$('btn-change').addEventListener('click', async () => {
  try {
    const path = await ChangeFolder();
    if (path) {
      showToast('Dossier modifié — relancez pour appliquer');
    }
  } catch (e) {
    showToast('Erreur : ' + e);
  }
});
$('btn-quit').addEventListener('click', () => QuitApp());

boot();
