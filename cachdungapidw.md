<!DOCTYPE html>
<html lang="vi">
<head>
<meta charset="UTF-8">
<title>Test Upload API + WS Progress</title>
</head>
<body>
  <h3>Test TeleCloud Upload API (WebSocket progress)</h3>
  <p>
    Host: <input id="host" value="localhost:8080" size="20">
    Token: <input id="token" size="40" placeholder="dán API token ở trang /settings">
  </p>
  <p>
    Path: <input id="path" value="/">
    File: <input id="file" type="file">
  </p>
  <p>
    <label><input id="shareCheck" type="checkbox"> Public share (lấy share_url/direct_url)</label>
  </p>
  <button onclick="doUpload()">Upload</button>
  <div id="bar" style="width:300px;border:1px solid #333;margin-top:10px;">
    <div id="fill" style="width:0%;background:#4caf50;height:20px;"></div>
  </div>
  <pre id="log"></pre>

<script>
function log(msg) {
  document.getElementById('log').textContent += msg + "\n";
}

function doUpload() {
  const host = document.getElementById('host').value;
  const token = document.getElementById('token').value;
  const path = document.getElementById('path').value;
  const fileInput = document.getElementById('file');
  if (!fileInput.files.length) { alert('Chọn file trước'); return; }
  const file = fileInput.files[0];

  const uploadId = crypto.randomUUID();
  document.getElementById('log').textContent = '';
  document.getElementById('fill').style.width = '0%';

  // 1. Mở WebSocket TRƯỚC khi POST
  const ws = new WebSocket(`ws://${host}/api/upload-api/progress/${uploadId}?token=${encodeURIComponent(token)}`);

  ws.onopen = () => log('[ws] connected, uploadId=' + uploadId);
  ws.onerror = (e) => log('[ws] error: ' + JSON.stringify(e));
  ws.onclose = () => log('[ws] closed');
  ws.onmessage = (e) => {
    const data = JSON.parse(e.data);
    log(`[ws] stage=${data.stage} pct=${data.pct} msg=${data.message}`);
    document.getElementById('fill').style.width = data.pct + '%';
  };

  // 2. POST file kèm uploadId
  setTimeout(() => {
    const form = new FormData();
    form.append('file', file);
    form.append('path', path);
    form.append('uploadId', uploadId);
    if (document.getElementById('shareCheck').checked) {
      form.append('share', 'public');
    }

    fetch(`http://${host}/api/upload-api/upload`, {
      method: 'POST',
      headers: { 'Authorization': 'Bearer ' + token },
      body: form,
    })
      .then(r => r.json())
      .then(j => log('[http] response: ' + JSON.stringify(j)))
      .catch(err => log('[http] error: ' + err));
  }, 200); // đợi WS handshake xong rồi mới POST cho chắc
}
</script>
</body>
</html>