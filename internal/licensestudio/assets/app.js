(() => {
  'use strict';

  const pathParts = window.location.pathname.split('/').filter(Boolean);
  const sessionToken = pathParts[0] || '';
  const basePath = `/${sessionToken}/`;
  const toast = document.querySelector('#toast');

  const api = async (endpoint, payload) => {
    const response = await fetch(`${basePath}api/${endpoint}`, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        'X-UniRouter-Studio-Token': sessionToken,
      },
      body: JSON.stringify(payload),
    });
    const body = await response.json().catch(() => ({}));
    if (!response.ok) {
      const error = new Error(body?.error?.message || `操作失败（HTTP ${response.status}）`);
      error.code = body?.error?.code || 'request_failed';
      throw error;
    }
    return body;
  };

  const showToast = (message, kind = 'info') => {
    toast.textContent = message;
    toast.dataset.kind = kind;
    toast.classList.remove('hidden');
    window.clearTimeout(showToast.timer);
    showToast.timer = window.setTimeout(() => toast.classList.add('hidden'), 5000);
  };

  const setBusy = (button, busy, busyText) => {
    if (!button.dataset.label) button.dataset.label = button.textContent;
    button.disabled = busy;
    button.textContent = busy ? busyText : button.dataset.label;
  };

  const escapeHTML = (value) => String(value ?? '')
    .replaceAll('&', '&amp;')
    .replaceAll('<', '&lt;')
    .replaceAll('>', '&gt;')
    .replaceAll('"', '&quot;')
    .replaceAll("'", '&#039;');

  const activateTab = (name) => {
    document.querySelectorAll('.tab').forEach((tab) => {
      const active = tab.dataset.tab === name;
      tab.classList.toggle('active', active);
      tab.setAttribute('aria-selected', String(active));
    });
    document.querySelectorAll('.panel').forEach((panel) => {
      const active = panel.id === `panel-${name}`;
      panel.classList.toggle('active', active);
      panel.hidden = !active;
    });
    window.scrollTo({ top: document.querySelector('.tabs').offsetTop - 20, behavior: 'smooth' });
  };

  document.querySelectorAll('.tab').forEach((tab) => {
    tab.addEventListener('click', () => activateTab(tab.dataset.tab));
  });

  const isoDate = (date) => [date.getFullYear(), String(date.getMonth() + 1).padStart(2, '0'), String(date.getDate()).padStart(2, '0')].join('-');
  const today = new Date();
  const nextYear = new Date(today.getFullYear() + 1, today.getMonth(), today.getDate());
  document.querySelector('#issued-date').value = isoDate(today);
  document.querySelector('#not-before-date').value = isoDate(today);
  document.querySelector('#expires-date').value = isoDate(nextYear);

  const randomHex = (bytes = 6) => {
    const value = new Uint8Array(bytes);
    window.crypto.getRandomValues(value);
    return Array.from(value, (part) => part.toString(16).padStart(2, '0')).join('');
  };
  document.querySelector('#license-id').value = `lic-${isoDate(today).replaceAll('-', '')}-${randomHex(4)}`;

  const keyPurpose = document.querySelector('#key-purpose');
  const keyID = document.querySelector('#key-id');
  const resetSuggestedKeyID = () => {
    const prefix = keyPurpose.value === 'upgrade' ? 'upgrade' : 'license';
    keyID.value = `${prefix}-${today.getFullYear()}-a`;
  };
  keyPurpose.addEventListener('change', resetSuggestedKeyID);
  resetSuggestedKeyID();

  const allFeatures = document.querySelector('input[name="features"][value="*"]');
  const individualFeatures = Array.from(document.querySelectorAll('#feature-grid input[name="features"]'));
  const syncFeatureState = () => {
    individualFeatures.forEach((input) => {
      input.disabled = allFeatures.checked;
      if (allFeatures.checked) input.checked = false;
    });
  };
  allFeatures.addEventListener('change', syncFeatureState);
  syncFeatureState();

  document.querySelector('#keygen-form').addEventListener('submit', async (event) => {
    event.preventDefault();
    const button = document.querySelector('#keygen-submit');
    const result = document.querySelector('#keygen-result');
    setBusy(button, true, '等待选择文件夹…');
    result.classList.add('hidden');
    try {
      const payload = Object.fromEntries(new FormData(event.currentTarget));
      const body = await api('keygen', payload);
      result.innerHTML = `
        <div class="result-title success"><span>✓</span><div><strong>密钥已安全生成</strong><p>请立即备份私钥；公开验签文件可以交给发布构建。</p></div></div>
        <dl>
          <div><dt>私钥文件</dt><dd>${escapeHTML(body.private_file)}</dd></div>
          <div><dt>公开验签文件</dt><dd>${escapeHTML(body.public_file)}</dd></div>
          <div><dt>公钥指纹（SHA-256）</dt><dd class="mono wrap">${escapeHTML(body.public_fingerprint)}</dd></div>
        </dl>
        <div class="build-value">
          <span>发布构建公开参数</span>
          <code>${escapeHTML(body.build_argument)}=${escapeHTML(body.build_value)}</code>
          <button type="button" class="secondary copy-build">复制公开参数</button>
        </div>`;
      result.classList.remove('hidden');
      result.querySelector('.copy-build').addEventListener('click', async () => {
        await navigator.clipboard.writeText(`${body.build_argument}=${body.build_value}`);
        showToast('公开构建参数已复制。它不是私钥，可以进入受控发布配置。', 'success');
      });
      document.querySelector('#issue-key-id').value = body.purpose === 'license' ? body.key_id : document.querySelector('#issue-key-id').value;
      showToast('密钥生成完成。请确认私钥备份后再签发。', 'success');
    } catch (error) {
      showToast(error.message, error.code === 'cancelled' ? 'info' : 'error');
    } finally {
      setBusy(button, false);
    }
  });

  const optionalPositiveInteger = (form, name) => {
    const value = form.get(name)?.trim();
    if (!value) return null;
    const parsed = Number(value);
    if (!Number.isSafeInteger(parsed) || parsed <= 0) throw new Error(`${name} 必须是正整数`);
    return parsed;
  };

  document.querySelector('#issue-form').addEventListener('submit', async (event) => {
    event.preventDefault();
    const button = document.querySelector('#issue-submit');
    const result = document.querySelector('#issue-result');
    setBusy(button, true, '等待选择私钥…');
    result.classList.add('hidden');
    try {
      const form = new FormData(event.currentTarget);
      const features = Array.from(event.currentTarget.querySelectorAll('input[name="features"]:checked:not(:disabled)'), (input) => input.value);
      if (!features.length) throw new Error('至少选择一个授权功能');
      const limits = {};
      ['active_api_keys', 'active_users', 'active_projects', 'active_channels'].forEach((name) => {
        const value = optionalPositiveInteger(form, name);
        if (value !== null) limits[name] = value;
      });
      const metadata = {};
      const contractReference = form.get('contract_reference').trim();
      if (contractReference) metadata.contract_reference = contractReference;
      const issuedDate = form.get('issued_date');
      const notBeforeDate = form.get('not_before_date');
      const expiresDate = form.get('expires_date');
      const payload = {
        license_id: form.get('license_id').trim(),
        customer: form.get('customer').trim(),
        deployment_id: form.get('deployment_id').trim(),
        tier: form.get('tier').trim(),
        issued_at: `${issuedDate}T00:00:00Z`,
        not_before: `${notBeforeDate}T00:00:00Z`,
        expires_at: `${expiresDate}T23:59:59Z`,
        features,
      };
      if (Object.keys(limits).length) payload.limits = limits;
      if (Object.keys(metadata).length) payload.metadata = metadata;
      setBusy(button, true, '等待选择保存位置…');
      const body = await api('issue', { key_id: form.get('key_id').trim(), payload });
      result.innerHTML = `
        <div class="result-title success"><span>✓</span><div><strong>License 已签发并完成自校验</strong><p>可以把 License 文件交付客户；私钥文件不得交付。</p></div></div>
        <dl>
          <div><dt>文件</dt><dd>${escapeHTML(body.license_file)}</dd></div>
          <div><dt>License ID</dt><dd>${escapeHTML(body.payload.license_id)}</dd></div>
          <div><dt>客户 / 部署</dt><dd>${escapeHTML(body.payload.customer)} · ${escapeHTML(body.payload.deployment_id)}</dd></div>
          <div><dt>文件 SHA-256</dt><dd class="mono wrap">${escapeHTML(body.license_sha256)}</dd></div>
        </dl>`;
      result.classList.remove('hidden');
      showToast('License 已生成。建议切换到“验证 License”再做一次交付前复核。', 'success');
    } catch (error) {
      showToast(error.message, error.code === 'cancelled' ? 'info' : 'error');
    } finally {
      setBusy(button, false);
    }
  });

  const statusLabels = {
    active: '有效', expiring: '即将到期', expired: '已到期', invalid: '无效',
    not_yet_valid: '尚未生效', deployment_mismatch: '部署不匹配', missing: '缺失',
  };

  document.querySelector('#verify-form').addEventListener('submit', async (event) => {
    event.preventDefault();
    const button = document.querySelector('#verify-submit');
    const result = document.querySelector('#verify-result');
    setBusy(button, true, '等待选择验签文件…');
    result.classList.add('hidden');
    try {
      const form = new FormData(event.currentTarget);
      const body = await api('verify', { deployment_id: form.get('deployment_id').trim() });
      const good = body.valid && body.enforceable && body.signature_valid && body.deployment_match;
      const license = body.license || {};
      result.innerHTML = `
        <div class="result-title ${good ? 'success' : 'danger'}"><span>${good ? '✓' : '×'}</span><div><strong>${escapeHTML(statusLabels[body.status] || body.status || '无法验证')}</strong><p>${good ? '签名、部署绑定和有效期均通过。' : escapeHTML(body.error || 'License 不可执行。')}</p></div></div>
        <dl>
          <div><dt>签名</dt><dd>${body.signature_valid ? '有效' : '未通过'}</dd></div>
          <div><dt>部署绑定</dt><dd>${body.deployment_match ? '匹配' : '不匹配'}</dd></div>
          <div><dt>License ID</dt><dd>${escapeHTML(license.license_id || '—')}</dd></div>
          <div><dt>客户</dt><dd>${escapeHTML(license.customer || '—')}</dd></div>
          <div><dt>有效期</dt><dd>${escapeHTML(license.not_before || '—')} → ${escapeHTML(license.expires_at || '—')}</dd></div>
          <div><dt>Key ID</dt><dd>${escapeHTML(body.key_id || '—')}</dd></div>
        </dl>`;
      result.classList.remove('hidden');
      showToast(good ? 'License 验证通过。' : 'License 未通过验证，请勿交付或导入。', good ? 'success' : 'error');
    } catch (error) {
      showToast(error.message, error.code === 'cancelled' ? 'info' : 'error');
    } finally {
      setBusy(button, false);
    }
  });

  document.querySelector('#shutdown-button').addEventListener('click', async () => {
    try {
      await api('shutdown', {});
      document.body.innerHTML = '<main class="shutdown"><div><h1>License Studio 已安全退出</h1><p>现在可以关闭这个页面。</p></div></main>';
    } catch (error) {
      showToast(error.message, 'error');
    }
  });
})();
