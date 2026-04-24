package main

import "net/http"

// serveRegisterPage serves the minimal registration HTML per
// docs/specs/foundations/03-identity-auth.md § Player-facing experience.
// This lives on the game server until the separate lobby service exists.
func serveRegisterPage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(registerHTML))
}

const registerHTML = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>VIBECAT 404 — Register</title>
<style>
  :root {
    --midnight: #0d1222;
    --deep-navy: #1a2338;
    --navy: #2b3d5c;
    --mid-blue: #5c8fbf;
    --bone: #f5f0e6;
    --muted: #8a8679;
    --accent: #ffdd33;
    --success: #8de77a;
    --error: #c4303b;
  }
  * { box-sizing: border-box; }
  html, body { margin: 0; height: 100%; }
  body {
    background: var(--midnight);
    color: var(--bone);
    font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
    font-size: 16px;
    display: flex;
    align-items: center;
    justify-content: center;
    padding: 24px;
  }
  main {
    width: 100%;
    max-width: 460px;
    background: var(--deep-navy);
    border: 2px solid var(--mid-blue);
    padding: 28px;
  }
  h1 {
    margin: 0 0 4px;
    font-size: 28px;
    letter-spacing: 1px;
  }
  p.lead {
    margin: 0 0 24px;
    color: var(--muted);
    font-size: 14px;
    line-height: 1.5;
  }
  label {
    display: block;
    margin: 14px 0 6px;
    font-size: 14px;
  }
  input {
    width: 100%;
    font: inherit;
    color: var(--bone);
    background: var(--midnight);
    border: 1px solid var(--mid-blue);
    padding: 8px 10px;
    outline: none;
  }
  input:focus { border-color: var(--accent); }
  button {
    margin-top: 22px;
    width: 100%;
    font: inherit;
    font-size: 20px;
    color: var(--bone);
    background: var(--navy);
    border: none;
    padding: 10px 16px;
    cursor: pointer;
  }
  button:hover:not(:disabled) { color: var(--accent); }
  button:disabled { opacity: 0.6; cursor: not-allowed; }
  .status {
    min-height: 1.4em;
    margin-top: 16px;
    font-size: 14px;
    color: var(--muted);
  }
  .status.error { color: var(--error); }
  .status.success { color: var(--success); }
  .done {
    margin-top: 18px;
    padding: 14px;
    background: var(--midnight);
    border: 1px dashed var(--accent);
    color: var(--bone);
    font-size: 14px;
    line-height: 1.6;
    display: none;
  }
  .done.visible { display: block; }
  .done a {
    color: var(--accent);
    text-decoration: none;
  }
  .done a:hover { color: var(--bone); }
</style>
</head>
<body>
<main>
  <h1>Register</h1>
  <p class="lead">Create an account. After registering, return to the game and log in with your username and password.</p>

  <form id="f" autocomplete="off" novalidate>
    <label for="display_name">Username</label>
    <input id="display_name" name="display_name" maxlength="32" required autocomplete="username">

    <label for="contact">Contact (email / phone / discord)</label>
    <input id="contact" name="contact" maxlength="256" required>

    <label for="password">Password</label>
    <input id="password" name="password" type="password" minlength="6" maxlength="128" required autocomplete="new-password">

    <button type="submit" id="submit">[ Register ]</button>
  </form>

  <div class="status" id="status"></div>

  <div class="done" id="done">
    <strong>Registered.</strong> Open the game and log in as
    <span id="done-username"></span> with the password you just set.
    <br><br>
    <a href="/">→ Return to the game</a>
  </div>
</main>

<script>
  const form = document.getElementById('f');
  const statusEl = document.getElementById('status');
  const doneBox = document.getElementById('done');
  const doneUser = document.getElementById('done-username');
  const submitBtn = document.getElementById('submit');

  function setStatus(text, kind) {
    statusEl.textContent = text;
    statusEl.className = 'status' + (kind ? ' ' + kind : '');
  }

  form.addEventListener('submit', async (ev) => {
    ev.preventDefault();
    const payload = {
      display_name: form.display_name.value.trim(),
      contact: form.contact.value.trim(),
      password: form.password.value,
    };
    if (!payload.display_name || !payload.contact || payload.password.length < 6) {
      setStatus('Fill every field. Password needs 6+ chars.', 'error');
      return;
    }
    submitBtn.disabled = true;
    setStatus('Registering...');
    try {
      const res = await fetch('/api/register', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(payload),
      });
      const data = await res.json().catch(() => ({}));
      if (!res.ok) {
        setStatus(data.error || ('Error: HTTP ' + res.status), 'error');
        submitBtn.disabled = false;
        return;
      }
      setStatus('Registered.', 'success');
      doneUser.textContent = payload.display_name;
      doneBox.classList.add('visible');
      form.password.value = '';
    } catch (err) {
      setStatus('Network error. Try again.', 'error');
      submitBtn.disabled = false;
    }
  });
</script>
</body>
</html>
`
