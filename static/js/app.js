// TeleCloud - Main App JS
'use strict';

let currentView = 'list';
let currentPath = '/';
let allFiles = [];
let allFolders = [];
let currentShareFileId = null;
let searchTimeout = null;
let player = null;

// ─── Alpine Dialog System ─────────────────────────────────────────────────────
function dialogSystem() {
  return {
    open: false,
    type: 'confirm',   // 'confirm' | 'prompt' | 'alert'
    title: '',
    message: '',
    danger: false,
    showCancel: true,
    confirmLabel: 'Xác nhận',
    cancelLabel: 'Hủy',
    inputVal: '',
    inputPlaceholder: '',
    icon_class: 'info',
    icon_fa: 'fa fa-question',
    _resolve: null,

    confirm() {
      const val = this.type === 'prompt' ? this.inputVal.trim() : true;
      this.open = false;
      this.inputVal = '';
      if (this._resolve) this._resolve(val);
    },
    cancel() {
      this.open = false;
      this.inputVal = '';
      if (this._resolve) this._resolve(this.type === 'prompt' ? null : false);
    },
    show(opts) {
      return new Promise(resolve => {
        this._resolve = resolve;
        this.type        = opts.type || 'confirm';
        this.title       = opts.title || '';
        this.message     = opts.message || '';
        this.danger      = opts.danger || false;
        this.showCancel  = opts.showCancel !== false;
        this.confirmLabel = opts.confirmLabel || 'Xác nhận';
        this.cancelLabel  = opts.cancelLabel  || 'Hủy';
        this.inputVal     = opts.defaultVal   || '';
        this.inputPlaceholder = opts.placeholder || '';
        this.icon_class  = opts.danger ? 'danger' : (this.type === 'prompt' ? 'info' : 'warning');
        this.icon_fa     = opts.icon || (opts.danger ? 'fa fa-trash' : (this.type === 'prompt' ? 'fa fa-pen' : 'fa fa-circle-exclamation'));
        this.open = true;
        if (this.type === 'prompt') {
          this.$nextTick(() => this.$refs.dlgInput?.focus());
        }
      });
    }
  };
}

// Helper global — dùng trong app.js
function dlg() {
  return document.getElementById('alpineDialog')?._x_dataStack?.[0];
}
async function dlgConfirm(opts)  { const d = dlg(); return d ? d.show({type:'confirm', ...opts}) : confirm(opts.title); }
async function dlgPrompt(opts)   { const d = dlg(); return d ? d.show({type:'prompt',  ...opts}) : prompt(opts.title); }
async function dlgAlert(opts)    { const d = dlg(); return d ? d.show({type:'alert', showCancel:false, confirmLabel:'Đóng', ...opts}) : alert(opts.title); }


// ─── Init ─────────────────────────────────────────────────────────────────────
document.addEventListener('DOMContentLoaded', () => {
  setView('list'); // khởi tạo đúng trạng thái view
  loadFiles();
  // Close modals on backdrop click
  ['uploadModal','playerModal','shareModal','settingsModal'].forEach(id => {
    document.getElementById(id).addEventListener('click', function(e) {
      if (e.target !== this) return;
      if (id === 'playerModal') { closePlayer(); return; }
      this.classList.add('hidden');
    });
  });
  // Keyboard shortcuts
  document.addEventListener('keydown', e => {
    if (e.key === 'Escape') closeAllModals();
    if (e.key === 'u' && !isInputFocused()) openUploadModal();
    if (e.key === 'n' && !isInputFocused()) openCreateFolderModal();
  });
});

function isInputFocused() {
  return ['INPUT','TEXTAREA'].includes(document.activeElement?.tagName);
}

function closeAllModals() {
  document.getElementById('uploadModal').classList.add('hidden');
  closePlayer();
  document.getElementById('shareModal').classList.add('hidden');
  document.getElementById('settingsModal').classList.add('hidden');
  removeContextMenu();
}

// ─── File Loading ─────────────────────────────────────────────────────────────
async function loadFiles(path = '/') {
  currentPath = path;
  showLoading(true);
  try { updateBreadcrumb(path); } catch(e) { console.error('breadcrumb error:', e); }

  try {
    console.log('[loadFiles] Fetching:', path);
    const res = await fetch(`/api/files?path=${encodeURIComponent(path)}`);
    console.log('[loadFiles] Response status:', res.status);
    const text = await res.text();
    console.log('[loadFiles] Response body:', text);
    if (!res.ok) throw new Error(`HTTP ${res.status}: ${text}`);
    const data = JSON.parse(text);
    allFiles = data.files || [];
    allFolders = data.folders || [];
    renderFiles(allFiles, allFolders);
  } catch (err) {
    console.error('[loadFiles] ERROR:', err);
    showToast('Lỗi tải file: ' + err.message, 'error');
  } finally {
    showLoading(false);
  }
}

function renderFiles(files, folders = []) {
  const grid = document.getElementById('fileGrid');
  const list = document.getElementById('fileList');
  const empty = document.getElementById('emptyState');
  const count = document.getElementById('fileCount');

  grid.innerHTML = '';
  list.innerHTML = '';

  const totalItems = (folders ? folders.length : 0) + files.length;
  const folderTxt = folders && folders.length ? `${folders.length} thư mục, ` : '';
  count.textContent = `${folderTxt}${files.length} file`;

  if (totalItems === 0) {
    empty.classList.remove('hidden');
    return;
  }
  empty.classList.add('hidden');

  // Render folders first
  (folders || []).forEach(folder => {
    grid.appendChild(createFolderCard(folder));
    list.appendChild(createFolderRow(folder));
  });

  // Then files
  files.forEach(file => {
    grid.appendChild(createGridCard(file));
    list.appendChild(createListRow(file));
  });
}

function createFolderCard(folder) {
  const div = document.createElement('div');
  div.className = 'file-card relative';
  div.dataset.folderId = folder.id;
  div.innerHTML = `
    <div class="file-thumb-placeholder" style="font-size:3rem;color:var(--yellow)">
      <i class="fa fa-folder"></i>
    </div>
    <div class="file-info">
      <div class="file-name" title="${escHtml(folder.name)}">${escHtml(folder.name)}</div>
      <div class="file-meta">Thư mục</div>
    </div>
  `;
  div.addEventListener('click', () => loadFiles(folder.full_path));
  div.addEventListener('contextmenu', e => { e.preventDefault(); showFolderContextMenu(e, folder); });
  return div;
}

function createFolderRow(folder) {
  const div = document.createElement('div');
  div.className = 'file-row';
  div.dataset.folderId = folder.id;
  div.innerHTML = `
    <div class="file-row-icon"><i class="fa fa-folder" style="color:var(--yellow)"></i></div>
    <div class="file-row-name" title="${escHtml(folder.name)}">${escHtml(folder.name)}</div>
    <div class="file-row-meta" style="margin-right:8px">Thư mục</div>
    <div class="file-row-meta">${folder.created_at}</div>
  `;
  div.addEventListener('click', () => loadFiles(folder.full_path));
  div.addEventListener('contextmenu', e => { e.preventDefault(); showFolderContextMenu(e, folder); });
  return div;
}

// ─── Breadcrumb ───────────────────────────────────────────────────────────────
function updateBreadcrumb(path) {
  const bc = document.getElementById('breadcrumb');
  if (!bc) return;

  if (path === '/') {
    bc.innerHTML = `<span class="bc-item bc-home" onclick="loadFiles('/')"><i class="fa fa-brands fa-telegram" style="color:var(--blue)"></i> Của tôi</span>`;
    return;
  }

  const parts = path.split('/').filter(Boolean);
  let html = `<span class="bc-item bc-home" onclick="loadFiles('/')"><i class="fa fa-brands fa-telegram" style="color:var(--blue)"></i> Của tôi</span>`;
  let accumulated = '';
  parts.forEach((part, i) => {
    accumulated += '/' + part;
    const fullPath = accumulated;
    if (i < parts.length - 1) {
      html += `<span class="bc-sep"><i class="fa fa-chevron-right"></i></span><span class="bc-item" onclick="loadFiles('${escHtml(fullPath)}')">${escHtml(part)}</span>`;
    } else {
      html += `<span class="bc-sep"><i class="fa fa-chevron-right"></i></span><span class="bc-item bc-current">${escHtml(part)}</span>`;
    }
  });
  bc.innerHTML = html;
}

// ─── Folder Management ────────────────────────────────────────────────────────
async function openCreateFolderModal() {
  const name = await dlgPrompt({
    title: 'Tạo thư mục mới',
    message: 'Nhập tên thư mục bạn muốn tạo.',
    placeholder: 'Tên thư mục...',
    confirmLabel: 'Tạo',
    icon: 'fa fa-folder-plus',
    icon_class: 'info',
  });
  if (!name) return;
  createFolder(name);
}

async function createFolder(name) {
  try {
    const form = new FormData();
    form.append('path', currentPath);
    form.append('name', name);
    const res = await fetch('/api/folders', { method: 'POST', body: form });
    const data = await res.json();
    if (res.ok) {
      showToast(`Đã tạo thư mục "${name}"`, 'success');
      await loadFiles(currentPath);
    } else {
      showToast(data.error || 'Lỗi tạo thư mục', 'error');
    }
  } catch {
    showToast('Lỗi kết nối', 'error');
  }
}

async function deleteFolder(folder) {
  const ok = await dlgConfirm({
    title: 'Xóa thư mục',
    message: `Bạn chắc chắn muốn xóa <b>"${escHtml(folder.name)}"</b>?<br><br>Toàn bộ thư mục con và file bên trong sẽ bị xóa khỏi database.`,
    danger: true,
    confirmLabel: 'Xóa',
    icon: 'fa fa-folder-minus',
  });
  if (!ok) return;
  try {
    const res = await fetch(`/api/folders/${folder.id}`, { method: 'DELETE' });
    if (res.ok) {
      showToast('Đã xóa thư mục', 'success');
      await loadFiles(currentPath);
    } else {
      const data = await res.json();
      showToast(data.error || 'Lỗi xóa thư mục', 'error');
    }
  } catch {
    showToast('Lỗi kết nối', 'error');
  }
}

function showFolderContextMenu(e, folder) {
  removeContextMenu();
  const menu = document.createElement('div');
  menu.className = 'ctx-menu';
  menu.id = 'ctxMenu';

  const items = [
    { icon: 'fa fa-folder-open', label: 'Mở thư mục', action: () => loadFiles(folder.full_path) },
    { icon: 'fa fa-trash', label: 'Xóa thư mục', danger: true, action: () => deleteFolder(folder) },
  ];

  items.forEach(item => {
    const el = document.createElement('div');
    el.className = 'ctx-item' + (item.danger ? ' danger' : '');
    el.innerHTML = `<i class="${item.icon}" style="width:16px;text-align:center"></i> ${item.label}`;
    el.onclick = () => { removeContextMenu(); item.action(); };
    menu.appendChild(el);
  });

  let x = e.clientX, y = e.clientY;
  document.body.appendChild(menu);
  const rect = menu.getBoundingClientRect();
  if (x + rect.width > window.innerWidth) x = window.innerWidth - rect.width - 8;
  if (y + rect.height > window.innerHeight) y = window.innerHeight - rect.height - 8;
  menu.style.left = x + 'px';
  menu.style.top = y + 'px';

  setTimeout(() => document.addEventListener('click', removeContextMenu, { once: true }), 0);
}

// ─── Move File ────────────────────────────────────────────────────────────────
async function moveFile(fileId, fileName) {
  const dest = await dlgPrompt({
    title: 'Di chuyển file',
    message: `Di chuyển <b>"${escHtml(fileName)}"</b> đến thư mục:<br><small style="color:var(--text-tertiary)">Ví dụ: / &nbsp;·&nbsp; /Videos &nbsp;·&nbsp; /Videos/2024</small>`,
    placeholder: '/ hoặc /TênThưMục',
    defaultVal: currentPath,
    confirmLabel: 'Di chuyển',
    icon: 'fa fa-folder-open',
  });
  if (dest === null) return;
  const newPath = dest.trim() === '' ? '/' : dest.trim();

  try {
    const form = new FormData();
    form.append('path', newPath);
    const res = await fetch(`/api/files/${fileId}/move`, { method: 'POST', body: form });
    const data = await res.json();
    if (res.ok) {
      showToast('Đã di chuyển file', 'success');
      await loadFiles(currentPath);
    } else {
      showToast(data.error || 'Lỗi di chuyển', 'error');
    }
  } catch {
    showToast('Lỗi kết nối', 'error');
  }
}

function createGridCard(file) {
  const div = document.createElement('div');
  div.className = 'file-card relative';
  div.dataset.id = file.id;

  const isVideo = file.is_video;
  const icon = getMimeIcon(file.mime_type);

  let thumbHTML;
  if (file.thumb_url) {
    thumbHTML = `<img src="${file.thumb_url}" class="file-thumb" loading="lazy" onerror="this.style.display='none';this.nextElementSibling.style.display='flex'">
      <div class="file-thumb-placeholder" style="display:none"><i class="${icon}"></i></div>`;
  } else {
    thumbHTML = `<div class="file-thumb-placeholder"><i class="${icon}"></i></div>`;
  }

  const playOverlay = isVideo ? `<div class="play-overlay"><div class="play-btn"><i class="fa fa-play" style="margin-left:3px"></i></div></div>` : '';

  div.innerHTML = `
    <div class="relative">
      ${thumbHTML}
      ${playOverlay}
    </div>
    <div class="file-info">
      <div class="file-name" title="${escHtml(file.name)}">${escHtml(file.name)}</div>
      <div class="file-meta">${file.size_str}${file.duration ? ' · ' + fmtDuration(file.duration) : ''}</div>
    </div>
    ${file.share_token ? '<div style="position:absolute;top:6px;right:6px;background:rgba(37,99,235,0.8);border-radius:99px;padding:2px 8px;font-size:10px;color:#fff"><i class="fa fa-link"></i></div>' : ''}
  `;

  div.addEventListener('click', () => isVideo ? openPlayer(file) : openShare(file.id));
  div.addEventListener('contextmenu', e => { e.preventDefault(); showContextMenu(e, file); });
  return div;
}

function createListRow(file) {
  const div = document.createElement('div');
  div.className = 'file-row';
  div.dataset.id = file.id;
  const icon = getMimeIcon(file.mime_type);

  div.innerHTML = `
    <div class="file-row-icon"><i class="${icon}"></i></div>
    <div class="file-row-name" title="${escHtml(file.name)}">${escHtml(file.name)}</div>
    <div class="file-row-meta" style="margin-right:8px">${file.size_str}</div>
    <div class="file-row-meta">${file.uploaded_at}</div>
  `;

  div.addEventListener('click', () => file.is_video ? openPlayer(file) : openShare(file.id));
  div.addEventListener('contextmenu', e => { e.preventDefault(); showContextMenu(e, file); });
  return div;
}

// ─── View Toggle ──────────────────────────────────────────────────────────────
function setView(view) {
  currentView = view;
  document.getElementById('fileGrid').classList.toggle('hidden', view !== 'grid');
  document.getElementById('fileList').classList.toggle('hidden', view !== 'list');
  const btnGrid = document.getElementById('btnGrid');
  const btnList = document.getElementById('btnList');
  if (btnGrid && btnList) {
    btnGrid.classList.toggle('active', view === 'grid');
    btnGrid.classList.toggle('text-blue-400', view === 'grid');
    btnList.classList.toggle('active', view === 'list');
    btnList.classList.toggle('text-blue-400', view === 'list');
  }
}

// ─── Search ───────────────────────────────────────────────────────────────────
function handleSearch(query) {
  clearTimeout(searchTimeout);
  searchTimeout = setTimeout(async () => {
    if (!query.trim()) {
      renderFiles(allFiles, allFolders);
      return;
    }
    try {
      const res = await fetch(`/api/search?q=${encodeURIComponent(query)}`);
      const data = await res.json();
      renderFiles(data.files || [], []);
    } catch {}
  }, 300);
}

// ─── Upload ───────────────────────────────────────────────────────────────────
function openUploadModal() {
  document.getElementById('uploadModal').classList.remove('hidden');
  document.getElementById('uploadList').innerHTML = '';
  document.getElementById('uploadSummary').classList.add('hidden');
}

function closeUploadModal() {
  document.getElementById('uploadModal').classList.add('hidden');
  loadFiles(currentPath);
}

function handleDrop(e) {
  e.preventDefault();
  e.currentTarget.classList.remove('border-blue-400');
  handleFileSelect(e.dataTransfer.files);
}

function handleFileSelect(files) {
  if (!files.length) return;
  uploadFiles(Array.from(files));
}

// Sinh 1 uploadId ngẫu nhiên dùng làm khóa theo dõi tiến trình qua SSE.
function genUploadId() {
  return 'u' + Date.now() + '_' + Math.random().toString(36).slice(2, 10);
}

// Upload 1 file, theo dõi tiến trình THẬT xuyên suốt 2 giai đoạn:
//   Giai đoạn 1 (0-50%): browser gửi file lên server của bạn (đo qua XHR upload event)
//   Giai đoạn 2 (50-100%): server gửi file lên Telegram (đo qua SSE, server báo về)
// onUpdate(pct, label) được gọi mỗi khi có cập nhật ở 1 trong 2 giai đoạn.
function uploadFileWithProgress(file, path, onUpdate) {
  return new Promise((resolve, reject) => {
    const uploadId = genUploadId();
    let settled = false;

    // Mở SSE TRƯỚC khi gửi file, để không bỏ lỡ event nào khi server bắt đầu
    // gửi lên Telegram (giai đoạn này có thể bắt đầu rất nhanh sau khi
    // browser gửi xong).
    const es = new EventSource(`/api/upload/progress/${uploadId}`);

    es.addEventListener('telegram', (e) => {
      try {
        const data = JSON.parse(e.data);
        // Map 0-100 (phía server) -> 50-99 (phía UI)
        const pct = 50 + Math.round((data.pct || 0) * 0.49);
        onUpdate(pct, data.message || 'Đang gửi lên Telegram...');
      } catch {}
    });

    es.addEventListener('done', (e) => {
      onUpdate(100, 'Hoàn thành');
    });

    es.addEventListener('error', (e) => {
      // Có thể là lỗi nghiệp vụ (server báo qua event 'error' với JSON data)
      // hoặc lỗi kết nối SSE bình thường khi stream đã đóng — bỏ qua nếu đã settled.
      if (settled) return;
    });

    function cleanup() {
      settled = true;
      es.close();
    }

    const xhr = new XMLHttpRequest();
    const formData = new FormData();
    formData.append('files', file);
    formData.append('path', path);
    formData.append('uploadId', uploadId);

    xhr.upload.addEventListener('progress', (e) => {
      if (e.lengthComputable) {
        // Map 0-100 (browser gửi) -> 0-50 (phía UI)
        const pct = Math.round((e.loaded / e.total) * 50);
        onUpdate(pct, 'Đang gửi lên server...');
      }
    });

    xhr.addEventListener('load', () => {
      try {
        const data = JSON.parse(xhr.responseText);
        const result = data.results?.[0];
        // Đợi thêm chút để SSE kịp nhận event 'done' cuối cùng trước khi đóng,
        // tránh trường hợp đóng XHR xong nhưng UI chưa kịp lên 100%.
        setTimeout(() => { cleanup(); resolve(result); }, 150);
      } catch (err) {
        cleanup();
        reject(err);
      }
    });

    xhr.addEventListener('error', () => { cleanup(); reject(new Error('Network error')); });
    xhr.addEventListener('abort', () => { cleanup(); reject(new Error('Aborted')); });

    xhr.open('POST', '/api/upload');
    xhr.send(formData);
  });
}

async function uploadFiles(files) {
  const listEl = document.getElementById('uploadList');
  const summaryEl = document.getElementById('uploadSummary');
  listEl.innerHTML = '';
  summaryEl.classList.add('hidden');

  let succeeded = 0, failed = 0;

  // Tạo progress items
  const items = files.map((f, i) => {
    const div = document.createElement('div');
    div.className = 'upload-item';
    div.innerHTML = `
      <div style="flex:1;min-width:0">
        <div style="font-size:0.8rem;color:#d0d0e0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;margin-bottom:6px">${escHtml(f.name)}</div>
        <div class="upload-progress-bar"><div class="upload-progress-fill" id="prog-${i}" style="width:0%"></div></div>
      </div>
      <div id="status-${i}" style="font-size:0.75rem;color:#505068;flex-shrink:0;margin-left:10px">Chờ...</div>
    `;
    listEl.appendChild(div);
    return { file: f, index: i };
  });

  // Upload từng file (có thể parallel với Promise.all nếu muốn)
  for (const { file, index } of items) {
    const progEl = document.getElementById(`prog-${index}`);
    const statusEl = document.getElementById(`status-${index}`);

    statusEl.textContent = 'Đang upload...';
    statusEl.style.color = '#60a5fa';

    try {
      const result = await uploadFileWithProgress(file, currentPath, (pct, label) => {
        progEl.style.width = pct + '%';
        statusEl.textContent = label;
      });

      if (result?.success) {
        progEl.style.width = '100%';
        progEl.style.background = 'linear-gradient(90deg, #059669, #34d399)';
        statusEl.textContent = 'Hoàn thành';
        statusEl.style.color = '#34d399';
        succeeded++;
      } else {
        progEl.style.width = '100%';
        progEl.style.background = '#ef4444';
        statusEl.textContent = 'Lỗi';
        statusEl.style.color = '#f87171';
        failed++;
      }
    } catch (err) {
      progEl.style.width = '100%';
      progEl.style.background = '#ef4444';
      statusEl.textContent = 'Lỗi kết nối';
      statusEl.style.color = '#f87171';
      failed++;
    }
  }


  // Summary
  summaryEl.classList.remove('hidden');
  const txt = document.getElementById('uploadSummaryText');
  txt.innerHTML = `<span style="color:#34d399"><i class="fa fa-check-circle mr-1"></i>${succeeded} thành công</span>` +
    (failed ? ` · <span style="color:#f87171"><i class="fa fa-times-circle mr-1"></i>${failed} lỗi</span>` : '');

  // Reload files
  await loadFiles(currentPath);
}

// ─── Video Player ─────────────────────────────────────────────────────────────
function openPlayer(file) {
  const modal = document.getElementById('playerModal');
  document.getElementById('playerTitle').textContent = file.name;

  // Destroy old player
  if (player) { try { player.destroy(); } catch {} player = null; }

  modal.classList.remove('hidden');

  player = new Artplayer({
    container: '#videoPlayer',
    url: `/stream/${file.id}`,
    title: file.name,
    lang: 'vi',
    volume: 1,
    autoplay: true,
    autoSize: false,
    autoMini: false,
    setting: true,
    fullscreen: true,
    fullscreenWeb: true,
    playbackRate: true,
    aspectRatio: true,
    pip: true,
    autoPlayback: false,
    airplay: true,
    theme: '#3b82f6',
  });
}

function closePlayer() {
  document.getElementById('playerModal').classList.add('hidden');
  if (player) { try { player.destroy(); } catch {} player = null; }
}

// ─── Share ────────────────────────────────────────────────────────────────────
async function openShare(fileId) {
  currentShareFileId = fileId;
  try {
    const res = await fetch(`/api/files/${fileId}/share`, { method: 'POST' });
    const data = await res.json();
    document.getElementById('shareURL').value = data.share_url;
    document.getElementById('directURL').value = data.direct_url;
    document.getElementById('shareModal').classList.remove('hidden');
  } catch (err) {
    showToast('Lỗi tạo link chia sẻ', 'error');
  }
}

async function unshareFile() {
  if (!currentShareFileId) return;
  try {
    await fetch(`/api/files/${currentShareFileId}/share`, { method: 'DELETE' });
    document.getElementById('shareModal').classList.add('hidden');
    showToast('Đã tắt chia sẻ', 'success');
    await loadFiles(currentPath);
  } catch {
    showToast('Lỗi', 'error');
  }
}

// ─── Context Menu ─────────────────────────────────────────────────────────────
function showContextMenu(e, file) {
  removeContextMenu();
  const menu = document.createElement('div');
  menu.className = 'ctx-menu';
  menu.id = 'ctxMenu';

  const items = [
    { icon: 'fa fa-play text-blue-400', label: 'Phát video', show: file.is_video, action: () => openPlayer(file) },
    { icon: 'fa fa-link text-green-400', label: 'Chia sẻ', action: () => openShare(file.id) },
    { icon: 'fa fa-download text-blue-400', label: 'Tải xuống', action: () => { window.open(`/stream/${file.id}`, '_blank'); } },
    { icon: 'fa fa-folder-open', label: 'Di chuyển vào thư mục', action: () => moveFile(file.id, file.name) },
    { icon: 'fa fa-trash text-red-400', label: 'Xóa', danger: true, action: () => deleteFile(file.id, file.name) },
  ];

  items.filter(i => i.show !== false).forEach(item => {
    const el = document.createElement('div');
    el.className = 'ctx-item' + (item.danger ? ' danger' : '');
    el.innerHTML = `<i class="${item.icon}" style="width:16px;text-align:center"></i> ${item.label}`;
    el.onclick = () => { removeContextMenu(); item.action(); };
    menu.appendChild(el);
  });

  // Position
  let x = e.clientX, y = e.clientY;
  document.body.appendChild(menu);
  const rect = menu.getBoundingClientRect();
  if (x + rect.width > window.innerWidth) x = window.innerWidth - rect.width - 8;
  if (y + rect.height > window.innerHeight) y = window.innerHeight - rect.height - 8;
  menu.style.left = x + 'px';
  menu.style.top = y + 'px';

  setTimeout(() => document.addEventListener('click', removeContextMenu, { once: true }), 0);
}

function removeContextMenu() {
  document.getElementById('ctxMenu')?.remove();
}

async function deleteFile(id, name) {
  const ok = await dlgConfirm({
    title: 'Xóa file',
    message: `Bạn chắc chắn muốn xóa <b>"${escHtml(name)}"</b>?<br><br>File sẽ bị xóa khỏi database nhưng vẫn còn trên Telegram.`,
    danger: true,
    confirmLabel: 'Xóa',
    icon: 'fa fa-trash',
  });
  if (!ok) return;
  try {
    const res = await fetch(`/api/files/${id}`, { method: 'DELETE' });
    if (res.ok) {
      showToast('Đã xóa file', 'success');
      await loadFiles(currentPath);
    } else {
      showToast('Lỗi xóa file', 'error');
    }
  } catch {
    showToast('Lỗi kết nối', 'error');
  }
}

// ─── Settings ─────────────────────────────────────────────────────────────────
async function openSettings() {
  document.getElementById('settingsModal').classList.remove('hidden');
  try {
    const res = await fetch('/api/settings');
    const data = await res.json();
    document.getElementById('apiToken').value = data.api_token;
    document.getElementById('apiEndpoint').value = data.upload_url;
    document.getElementById('curlEndpoint').textContent = data.upload_url;
    document.getElementById('curlToken').textContent = data.api_token.substring(0, 8) + '...';
  } catch {}
}

async function regenerateToken() {
  const ok = await dlgConfirm({
    title: 'Tạo mới API token',
    message: 'Token cũ sẽ <b>không còn hoạt động</b> ngay lập tức. Bạn chắc chắn muốn tiếp tục?',
    danger: true,
    confirmLabel: 'Tạo mới',
    icon: 'fa fa-key',
  });
  if (!ok) return;
  try {
    const res = await fetch('/api/settings/regenerate-token', { method: 'POST' });
    const data = await res.json();
    document.getElementById('apiToken').value = data.api_token;
    document.getElementById('curlToken').textContent = data.api_token.substring(0, 8) + '...';
    showToast('Đã tạo token mới', 'success');
  } catch {
    showToast('Lỗi', 'error');
  }
}

async function changePassword() {
  const oldPass = document.getElementById('oldPassword').value;
  const newPass = document.getElementById('newPassword').value;
  const msgEl = document.getElementById('pwMsg');

  msgEl.textContent = '';
  if (!oldPass || !newPass) { msgEl.textContent = 'Vui lòng điền đầy đủ'; msgEl.style.color = '#f87171'; return; }

  try {
    const form = new FormData();
    form.append('old_password', oldPass);
    form.append('new_password', newPass);
    const res = await fetch('/api/settings/change-password', { method: 'POST', body: form });
    const data = await res.json();
    if (res.ok) {
      msgEl.textContent = '✓ ' + data.message;
      msgEl.style.color = '#34d399';
      document.getElementById('oldPassword').value = '';
      document.getElementById('newPassword').value = '';
    } else {
      msgEl.textContent = data.error;
      msgEl.style.color = '#f87171';
    }
  } catch {
    msgEl.textContent = 'Lỗi kết nối';
    msgEl.style.color = '#f87171';
  }
}

// ─── Utils ────────────────────────────────────────────────────────────────────
function copyText(elId) {
  const el = document.getElementById(elId);
  navigator.clipboard.writeText(el.value).then(() => showToast('Đã copy!', 'success'));
}

function showToast(msg, type = 'info') {
  const toast = document.getElementById('toast');
  const content = document.getElementById('toastContent');
  const colors = { success: '#059669', error: '#dc2626', info: '#2563eb' };
  const icons = { success: 'fa-check-circle', error: 'fa-times-circle', info: 'fa-info-circle' };
  content.style.background = colors[type];
  content.innerHTML = `<i class="fa ${icons[type]}"></i> ${msg}`;
  toast.classList.remove('hidden');
  clearTimeout(toast._t);
  toast._t = setTimeout(() => toast.classList.add('hidden'), 3000);
}

function showLoading(show) {
  document.getElementById('loadingState').classList.toggle('hidden', !show);
}

function getMimeIcon(mime) {
  if (!mime) return 'fa fa-file';
  if (mime.startsWith('video/')) return 'fa fa-film text-blue-400';
  if (mime.startsWith('audio/')) return 'fa fa-music text-purple-400';
  if (mime.startsWith('image/')) return 'fa fa-image text-green-400';
  if (mime.includes('pdf')) return 'fa fa-file-pdf text-red-400';
  if (mime.includes('zip') || mime.includes('rar') || mime.includes('7z')) return 'fa fa-file-archive text-yellow-400';
  if (mime.includes('word') || mime.includes('document')) return 'fa fa-file-word text-blue-400';
  if (mime.includes('sheet') || mime.includes('excel')) return 'fa fa-file-excel text-green-400';
  return 'fa fa-file text-gray-500';
}

function fmtDuration(secs) {
  const h = Math.floor(secs / 3600);
  const m = Math.floor((secs % 3600) / 60);
  const s = secs % 60;
  if (h > 0) return `${h}:${pad(m)}:${pad(s)}`;
  return `${m}:${pad(s)}`;
}

function pad(n) { return n.toString().padStart(2, '0'); }

function escHtml(str) {
  return (str || '').replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/>/g,'&gt;').replace(/"/g,'&quot;');
}