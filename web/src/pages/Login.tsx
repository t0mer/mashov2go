import { useState, type FormEvent } from 'react';
import { api, ApiError } from '../api';
import { useI18n } from '../i18n';

// Login is used for both first-run setup and sign-in. Setup sets the admin
// password once (the API refuses a second setup).
export function Login({
  mode,
  onSignedIn,
}: {
  mode: 'setup' | 'login';
  onSignedIn: (csrf: string) => void;
}) {
  const { t } = useI18n();
  const [password, setPassword] = useState('');
  const [confirm, setConfirm] = useState('');
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);

  async function submit(e: FormEvent) {
    e.preventDefault();
    setError('');
    if (mode === 'setup' && password !== confirm) {
      setError(t('mismatch'));
      return;
    }
    setBusy(true);
    try {
      const path = mode === 'setup' ? '/api/v1/setup' : '/api/v1/auth/login';
      const res = await api<{ csrfToken: string }>('POST', path, { password });
      onSignedIn(res.csrfToken);
    } catch (err) {
      setError(err instanceof ApiError ? err.detail || err.title : t('error'));
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="login">
      <h1>{mode === 'setup' ? t('setupTitle') : t('signInTitle')}</h1>
      {mode === 'setup' && <p className="muted">{t('setupHint')}</p>}
      <form onSubmit={submit} className="stack">
        <label>
          {t('password')}
          <input
            type="password"
            autoComplete={mode === 'setup' ? 'new-password' : 'current-password'}
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            required
          />
        </label>
        {mode === 'setup' && (
          <label>
            {t('passwordConfirm')}
            <input
              type="password"
              autoComplete="new-password"
              value={confirm}
              onChange={(e) => setConfirm(e.target.value)}
              required
            />
          </label>
        )}
        {error && <p className="error" role="alert">{error}</p>}
        <button type="submit" className="primary" disabled={busy}>
          {mode === 'setup' ? t('setupButton') : t('signInButton')}
        </button>
      </form>
    </div>
  );
}
