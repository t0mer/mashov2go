import { useEffect, useState, type FormEvent } from 'react';
import { api, ApiError, type GeneralSettings, type Token } from '../api';
import { useI18n } from '../i18n';
import { Flash, useFlash } from './shared';

// Settings: general options, the admin password, and API tokens (CLAUDE.md §9, §10).
export function SettingsPage() {
  return (
    <section className="stack wide">
      <General />
      <ChangePassword />
      <Tokens />
    </section>
  );
}

function General() {
  const { t } = useI18n();
  const flash = useFlash();
  const [s, setS] = useState<GeneralSettings | null>(null);

  useEffect(() => {
    api<GeneralSettings>('GET', '/api/v1/settings/general')
      .then(setS)
      .catch((err) => flash.error(err instanceof ApiError ? err.detail || err.title : t('error')));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  async function save(e: FormEvent) {
    e.preventDefault();
    if (!s) return;
    try {
      setS(await api<GeneralSettings>('PATCH', '/api/v1/settings/general', s));
      flash.ok(t('generalSaved'));
    } catch (err) {
      flash.error(err instanceof ApiError ? err.detail || err.title : t('error'));
    }
  }

  if (!s) return <p className="muted">{t('loading')}</p>;
  const text = (k: keyof GeneralSettings, label: string) => (
    <label key={k}>
      {label}
      <input dir="ltr" value={String(s[k])} onChange={(e) => setS({ ...s, [k]: e.target.value })} />
    </label>
  );
  return (
    <form className="card stack" onSubmit={save}>
      <h2>{t('generalTitle')}</h2>
      <Flash state={flash} />
      {text('timezone', 'Timezone')}
      {text('planDefaultTime', 'Default plan time')}
      {text('activeHours', 'Active hours')}
      {text('syncFastInterval', 'Fast sync interval')}
      {text('syncFullInterval', 'Full sync interval')}
      {text('syncNightInterval', 'Night sync interval')}
      {text('mashovMinGap', 'Minimum gap between Mashov requests')}
      {text('eventsRetention', 'Events retention')}
      <label>
        Mashov profile
        <select value={s.mashovProfile} onChange={(e) => setS({ ...s, mashovProfile: e.target.value })}>
          <option value="android">android</option>
          <option value="web">web</option>
        </select>
      </label>
      <label className="check">
        <input type="checkbox" checked={s.preserveUnread} onChange={(e) => setS({ ...s, preserveUnread: e.target.checked })} />
        Keep mail unread
      </label>
      <label className="check">
        <input type="checkbox" checked={s.shabbatMode} onChange={(e) => setS({ ...s, shabbatMode: e.target.checked })} />
        Shabbat mode
      </label>
      <div>
        <button type="submit" className="primary">
          {t('save')}
        </button>
      </div>
    </form>
  );
}

function ChangePassword() {
  const { t } = useI18n();
  const flash = useFlash();
  const [current, setCurrent] = useState('');
  const [next, setNext] = useState('');
  const [confirm, setConfirm] = useState('');

  async function submit(e: FormEvent) {
    e.preventDefault();
    if (next !== confirm) {
      flash.error(t('mismatch'));
      return;
    }
    try {
      await api('POST', '/api/v1/settings/admin-password', { current, new: next });
      setCurrent('');
      setNext('');
      setConfirm('');
      flash.ok(t('passwordChanged'));
    } catch (err) {
      flash.error(err instanceof ApiError ? err.detail || err.title : t('error'));
    }
  }

  return (
    <form className="card stack" onSubmit={submit}>
      <h2>{t('passwordTitle')}</h2>
      <Flash state={flash} />
      <label>
        {t('currentPassword')}
        <input type="password" autoComplete="current-password" value={current} onChange={(e) => setCurrent(e.target.value)} required />
      </label>
      <label>
        {t('newPassword')}
        <input type="password" autoComplete="new-password" value={next} onChange={(e) => setNext(e.target.value)} required />
      </label>
      <label>
        {t('passwordConfirm')}
        <input type="password" autoComplete="new-password" value={confirm} onChange={(e) => setConfirm(e.target.value)} required />
      </label>
      <div>
        <button type="submit" className="primary">
          {t('changePassword')}
        </button>
      </div>
    </form>
  );
}

function Tokens() {
  const { t } = useI18n();
  const flash = useFlash();
  const [list, setList] = useState<Token[] | null>(null);
  const [name, setName] = useState('');
  const [scope, setScope] = useState<'read' | 'admin'>('read');
  const [shown, setShown] = useState('');

  async function load() {
    try {
      setList(await api<Token[]>('GET', '/api/v1/tokens'));
    } catch (err) {
      flash.error(err instanceof ApiError ? err.detail || err.title : t('error'));
    }
  }

  useEffect(() => {
    void load();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  async function create(e: FormEvent) {
    e.preventDefault();
    try {
      const res = await api<{ token: string }>('POST', '/api/v1/tokens', { name, scope });
      setShown(res.token);
      setName('');
      await load();
    } catch (err) {
      flash.error(err instanceof ApiError ? err.detail || err.title : t('error'));
    }
  }

  async function revoke(id: number) {
    try {
      await api('DELETE', `/api/v1/tokens/${id}`);
      await load();
    } catch (err) {
      flash.error(err instanceof ApiError ? err.detail || err.title : t('error'));
    }
  }

  return (
    <div className="card stack">
      <h2>{t('tokensTitle')}</h2>
      <Flash state={flash} />
      {shown && (
        <div className="notice">
          <p>{t('tokenShownOnce')}</p>
          <input readOnly dir="ltr" value={shown} onFocus={(e) => e.target.select()} />
        </div>
      )}
      <form className="row" onSubmit={create}>
        <input placeholder={t('tokenName')} value={name} onChange={(e) => setName(e.target.value)} required />
        <select value={scope} onChange={(e) => setScope(e.target.value as 'read' | 'admin')}>
          <option value="read">{t('scopeRead')}</option>
          <option value="admin">{t('scopeAdmin')}</option>
        </select>
        <button type="submit" className="primary">
          {t('createToken')}
        </button>
      </form>
      {list && (
        <table>
          <thead>
            <tr>
              <th>{t('name')}</th>
              <th>{t('scope')}</th>
              <th>{t('status')}</th>
              <th />
            </tr>
          </thead>
          <tbody>
            {list.map((k) => (
              <tr key={k.id}>
                <td>{k.name}</td>
                <td>{k.scope}</td>
                <td>{k.revokedAt ? t('revoked') : t('active')}</td>
                <td>{!k.revokedAt && <button className="danger" onClick={() => revoke(k.id)}>{t('revoke')}</button>}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </div>
  );
}
