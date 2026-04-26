package main

import "net/http"

// serveRegisterPage serves the minimal registration HTML per
// docs/specs/foundations/03-identity-auth.md § Player-facing experience.
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
    --mid-blue: #5c8fbf;
    --bone: #f5f0e6;
    --muted: #8a8679;
    --accent: #ffdd33;
    --success: #8de77a;
    --error: #e64a55;
  }
  * { box-sizing: border-box; }
  html, body { margin: 0; min-height: 100%; }
  body {
    min-height: 100vh;
    background:
      linear-gradient(rgba(13, 18, 34, 0.38), rgba(13, 18, 34, 0.55)),
      url('/assets/title.png') center center / cover no-repeat,
      var(--midnight);
    color: var(--bone);
    font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
    font-size: 16px;
    padding: 40px;
  }
  .wrap {
    min-height: calc(100vh - 80px);
    display: flex;
    align-items: flex-end;
    justify-content: flex-end;
  }
  main {
    width: 100%;
    max-width: 460px;
    background: rgba(26, 35, 56, 0.86);
    border: 2px solid var(--mid-blue);
    box-shadow: 0 18px 60px rgba(0, 0, 0, 0.42);
    padding: 22px 20px 20px;
  }
  h1 {
    margin: 0 0 6px;
    font-size: 28px;
    letter-spacing: 1px;
  }
  p.lead {
    margin: 0 0 18px;
    color: var(--muted);
    font-size: 14px;
    line-height: 1.5;
  }
  .nav {
    margin: 0 0 18px;
    color: var(--muted);
    font-size: 14px;
  }
  a {
    color: var(--accent);
    text-decoration: none;
  }
  a:hover { color: var(--bone); }
  label {
    display: block;
    margin: 12px 0 6px;
    color: var(--bone);
    font-size: 18px;
  }
  input {
    width: 100%;
    font: inherit;
    color: var(--bone);
    background: var(--deep-navy);
    border: 1px solid var(--mid-blue);
    padding: 9px 10px;
    outline: none;
  }
  input::placeholder { color: var(--muted); }
  input:focus {
    border-color: var(--accent);
    box-shadow: 0 0 0 1px rgba(255, 221, 51, 0.28);
  }
  button {
    margin-top: 18px;
    font: inherit;
    font-size: 16px;
    color: var(--bone);
    background: transparent;
    border: none;
    padding: 0;
    cursor: pointer;
    display: inline-block;
  }
  button:hover:not(:disabled),
  button:focus-visible {
    color: var(--accent);
  }
  button:disabled { opacity: 0.6; cursor: not-allowed; }
  .status {
    min-height: 1.4em;
    margin-top: 14px;
    font-size: 14px;
    color: var(--muted);
  }
  .status.error { color: var(--error); }
  .status.success { color: var(--success); }
  .done {
    margin-top: 18px;
    padding: 14px;
    background: rgba(13, 18, 34, 0.75);
    border: 1px dashed var(--accent);
    color: var(--bone);
    font-size: 14px;
    line-height: 1.6;
    display: none;
  }
  .done.visible { display: block; }
  .actions {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    margin-top: 6px;
  }
  .hint {
    color: var(--muted);
    font-size: 12px;
    line-height: 1.4;
  }
  @media (max-width: 800px) {
    body { padding: 18px; }
    .wrap {
      min-height: calc(100vh - 36px);
      align-items: center;
      justify-content: center;
    }
    main { max-width: 100%; }
  }
</style>
</head>
<body>
<div class="wrap">
  <main>
    <h1>Register</h1>
    <p class="lead">Create an account, then return to the game and log in with the same username and password.</p>
    <div class="nav">
      <a href="/">[ Back to Game ]</a>
      &nbsp;·&nbsp;
      <a href="/leaderboard">[ Leaderboard ]</a>
      &nbsp;·&nbsp;
      <a href="/source.tar.gz" download>[ Source ]</a>
    </div>

    <form id="f" autocomplete="off" novalidate>
      <label for="display_name">Username:</label>
      <input id="display_name" name="display_name" maxlength="32" required autocomplete="username" placeholder="username">

      <label for="contact">Contact:</label>
      <input id="contact" name="contact" maxlength="256" required placeholder="email / phone / discord">

      <label for="password">Password:</label>
      <input id="password" name="password" type="password" minlength="6" maxlength="128" required autocomplete="new-password" placeholder="password">

      <div class="actions">
        <button type="submit" id="submit">[ Register ]</button>
        <div class="hint">Password must be at least 6 characters.</div>
      </div>
    </form>

    <div class="status" id="status"></div>

    <div class="done" id="done">
      <strong>Registered.</strong> Return to the game and log in as
      <span id="done-username"></span> with the password you just set.
      <br><br>
      <a href="/">[ Return to Game ]</a>
    </div>
  </main>
</div>

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
