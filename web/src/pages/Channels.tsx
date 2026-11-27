import { useEffect, useState, type FormEvent } from 'react';
import { api, ApiError, type Connector, type Member } from '../api';
import { useI18n } from '../i18n';
import { Flash, useFlash } from './shared';

// Channels are the WhatsApp (Green-API, OpenWA) and Telegram connectors. Config
// is sent once and never shown back (CLAUDE.md §8).
const TEMPLATES: Record<Connector['type'], string> = {
  greenapi: JSON.stringify({ api_url: 'https://api.green-api.com', instance_id: '', token: '' }, null, 2),
  openwa: JSON.stringify({ base_url: 'https://wa.example.com', instance_id: '', api_key: '' }, null, 2),
  telegram: JSON.stringify({ bot_token: '' }, null, 2),
};

function errText(err: unknown, fallback: string): string {
  return err instanceof ApiError ? err.detail || err.title : fallback;
}

export function Channels() {
  const { t } = useI18n();
  const flash = useFlash();
  const [list, setList] = useState<Connector[] | null>(null);
  const [members, setMembers] = useState<Member[]>([]);
  const [health, setHealth] = useState<Record<number, string>>({});

  async function load() {
    try {
      setList(await api<Connector[]>('GET', '/api/v1/connectors'));
      const kids = await api<Member[]>('GET', '/api/v1/children');
      const parents = await api<Member[]>('GET', '/api/v1/parents');
      setMembers([...kids, ...parents]);
    } catch (err) {
      flash.error(errText(err, t('error')));
    }
  }

  useEffect(() => {
    void load();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  async function checkHealth(c: Connector) {
    try {
      const res = await api<{ status: string }>('GET', `/api/v1/connectors/${c.id}/health`);
      setHealth({ ...health, [c.id]: res.status });
    } catch (err) {
      flash.error(errText(err, t('error')));
    }
  }

  async function toggle(c: Connector) {
    try {
      await api('PATCH', `/api/v1/connectors/${c.id}`, { enabled: !c.enabled });
      await load();
    } catch (err) {
      flash.error(errText(err, t('error')));
    }
  }

  async function makeDefault(c: Connector) {
    try {
      await api('POST', `/api/v1/connectors/${c.id}/default`);
      await load();
    } catch (err) {
      flash.error(errText(err, t('error')));
    }
  }

  async function remove(c: Connector) {
    if (!window.confirm(t('confirmDelete'))) return;
    try {
      await api('DELETE', `/api/v1/connectors/${c.id}`);
      await load();
    } catch (err) {
      flash.error(errText(err, t('error')));
    }
  }

  return (
    <section>
      <Flash state={flash} />
      <h2>{t('connectorsTitle')}</h2>
      {list === null ? (
        <p className="muted">{t('loading')}</p>
      ) : list.length === 0 ? (
        <p className="muted">{t('noRows')}</p>
      ) : (
        <table>
          <thead>
            <tr>
              <th>{t('name')}</th>
              <th>{t('type')}</th>
              <th>{t('enabled')}</th>
              <th>{t('isDefault')}</th>
              <th>{t('health')}</th>
              <th />
            </tr>
          </thead>
          <tbody>
            {list.map((c) => (
              <tr key={c.id}>
                <td>{c.name}</td>
                <td>{c.type}</td>
                <td>
                  <button onClick={() => toggle(c)}>{c.enabled ? t('yes') : t('no')}</button>
                </td>
                <td>
                  {c.isDefault ? (
                    t('isDefault')
                  ) : c.type !== 'telegram' ? (
                    <button onClick={() => makeDefault(c)}>{t('makeDefault')}</button>
                  ) : null}
                </td>
                <td>
                  <button onClick={() => checkHealth(c)}>{t('health')}</button>{' '}
                  <span>{health[c.id] ?? ''}</span>
                </td>
                <td className="row">
                  <TestSend connector={c} members={members} flash={flash} />
                  <button className="danger" onClick={() => remove(c)}>
                    {t('delete')}
                  </button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
      <AddConnector onAdded={() => void load()} flash={flash} />
    </section>
  );
}

function TestSend({ connector, members, flash }: { connector: Connector; members: Member[]; flash: ReturnType<typeof useFlash> }) {
  const { t } = useI18n();
  const [memberId, setMemberId] = useState<number>(0);
  async function send() {
    if (!memberId) return;
    try {
      await api('POST', `/api/v1/connectors/${connector.id}/test`, { memberId });
      flash.ok(t('sent'));
    } catch (err) {
      flash.error(errText(err, t('error')));
    }
  }
  return (
    <span className="row">
      <select aria-label={t('chooseMember')} value={memberId} onChange={(e) => setMemberId(Number(e.target.value))}>
        <option value={0}>{t('chooseMember')}</option>
        {members.map((m) => (
          <option key={m.id} value={m.id}>
            {m.name}
          </option>
        ))}
      </select>
      <button onClick={send}>{t('testSend')}</button>
    </span>
  );
}

function AddConnector({ onAdded, flash }: { onAdded: () => void; flash: ReturnType<typeof useFlash> }) {
  const { t } = useI18n();
  const [open, setOpen] = useState(false);
  const [type, setType] = useState<Connector['type']>('greenapi');
  const [name, setName] = useState('');
  const [isDefault, setIsDefault] = useState(false);
  const [config, setConfig] = useState(TEMPLATES.greenapi);

  function pickType(next: Connector['type']) {
    setType(next);
    setConfig(TEMPLATES[next]);
  }

  async function submit(e: FormEvent) {
    e.preventDefault();
    let parsed: unknown;
    try {
      parsed = JSON.parse(config);
    } catch {
      flash.error(t('configJson'));
      return;
    }
    try {
      await api('POST', '/api/v1/connectors', { type, name, isDefault: type !== 'telegram' && isDefault, config: parsed });
      setName('');
      setConfig(TEMPLATES[type]);
      setOpen(false);
      flash.ok(t('saved'));
      onAdded();
    } catch (err) {
      flash.error(errText(err, t('error')));
    }
  }

  if (!open) {
    return (
      <button className="primary" onClick={() => setOpen(true)}>
        {t('addConnector')}
      </button>
    );
  }
  return (
    <form className="card stack" onSubmit={submit}>
      <label>
        {t('type')}
        <select value={type} onChange={(e) => pickType(e.target.value as Connector['type'])}>
          <option value="greenapi">Green-API (WhatsApp)</option>
          <option value="openwa">OpenWA (WhatsApp)</option>
          <option value="telegram">Telegram</option>
        </select>
      </label>
      <label>
        {t('name')}
        <input value={name} onChange={(e) => setName(e.target.value)} required />
      </label>
      {type !== 'telegram' && (
        <label className="check">
          <input type="checkbox" checked={isDefault} onChange={(e) => setIsDefault(e.target.checked)} />
          {t('makeDefault')}
        </label>
      )}
      <label>
        {t('configJson')}
        <textarea dir="ltr" rows={6} value={config} onChange={(e) => setConfig(e.target.value)} />
      </label>
      <div className="row">
        <button type="submit" className="primary">
          {t('add')}
        </button>
        <button type="button" onClick={() => setOpen(false)}>
          {t('cancel')}
        </button>
      </div>
    </form>
  );
}
