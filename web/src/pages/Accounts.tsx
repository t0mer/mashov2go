import { useEffect, useState, type FormEvent } from 'react';
import { api, ApiError, type Account, type Student } from '../api';
import { useI18n } from '../i18n';
import { Flash, useFlash } from './shared';

// Accounts are the Mashov parent logins. Adding one signs in to Mashov first;
// the response lists the students the login can see (CLAUDE.md §3).
export function Accounts() {
  const { t } = useI18n();
  const flash = useFlash();
  const [accounts, setAccounts] = useState<Account[] | null>(null);
  const [found, setFound] = useState<{ name: string; students: Student[] } | null>(null);
  const [students, setStudents] = useState<Record<number, Student[]>>({});

  async function load() {
    try {
      setAccounts(await api<Account[]>('GET', '/api/v1/accounts'));
    } catch (err) {
      flash.error(err instanceof ApiError ? err.detail || err.title : t('error'));
    }
  }

  useEffect(() => {
    void load();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  async function showStudents(a: Account) {
    try {
      const list = await api<Student[]>('GET', `/api/v1/accounts/${a.id}/students`);
      setStudents({ ...students, [a.id]: list });
      await load();
    } catch (err) {
      flash.error(err instanceof ApiError ? err.detail || err.title : t('error'));
    }
  }

  return (
    <section>
      <Flash state={flash} />
      <h2>{t('accountsTitle')}</h2>
      {accounts === null ? (
        <p className="muted">{t('loading')}</p>
      ) : accounts.length === 0 ? (
        <p className="muted">{t('noRows')}</p>
      ) : (
        <table>
          <thead>
            <tr>
              <th>{t('name')}</th>
              <th>{t('username')}</th>
              <th>{t('semel')}</th>
              <th>{t('year')}</th>
              <th>{t('authState')}</th>
              <th />
            </tr>
          </thead>
          <tbody>
            {accounts.map((a) => (
              <tr key={a.id}>
                <td>{a.name}</td>
                <td dir="ltr">{a.username}</td>
                <td>{a.semel}</td>
                <td>{a.year}</td>
                <td className={a.authState === 'ok' ? '' : 'error'}>{a.authState}</td>
                <td>
                  <button onClick={() => showStudents(a)}>{t('loadStudents')}</button>
                  {students[a.id] && (
                    <ul className="plain small">
                      {students[a.id].map((s) => (
                        <li key={s.childGuid}>
                          {s.name} {s.familyName}
                        </li>
                      ))}
                    </ul>
                  )}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}

      <AddAccount
        onAdded={(name, list) => {
          setFound({ name, students: list });
          void load();
        }}
        flash={flash}
      />
      {found && (
        <div className="card">
          <h4>
            {t('studentsFound')}: {found.name}
          </h4>
          {found.students.length === 0 ? (
            <p className="muted">{t('noStudents')}</p>
          ) : (
            <ul className="plain">
              {found.students.map((s) => (
                <li key={s.childGuid}>
                  {s.name} {s.familyName}
                </li>
              ))}
            </ul>
          )}
        </div>
      )}
    </section>
  );
}

function AddAccount({
  onAdded,
  flash,
}: {
  onAdded: (name: string, students: Student[]) => void;
  flash: ReturnType<typeof useFlash>;
}) {
  const { t } = useI18n();
  const [open, setOpen] = useState(false);
  const [form, setForm] = useState({ name: '', semel: '', year: '', username: '', password: '' });

  async function submit(e: FormEvent) {
    e.preventDefault();
    try {
      const res = await api<{ account: Account; students: Student[] }>('POST', '/api/v1/accounts', {
        name: form.name,
        semel: Number(form.semel),
        year: Number(form.year),
        username: form.username,
        password: form.password,
      });
      setForm({ ...form, password: '' });
      setOpen(false);
      flash.ok(t('saved'));
      onAdded(res.account.name, res.students);
    } catch (err) {
      flash.error(err instanceof ApiError ? err.detail || err.title : t('error'));
    }
  }

  if (!open) {
    return (
      <button className="primary" onClick={() => setOpen(true)}>
        {t('addAccount')}
      </button>
    );
  }
  const set = (k: keyof typeof form) => (e: { target: { value: string } }) => setForm({ ...form, [k]: e.target.value });
  return (
    <form className="card stack" onSubmit={submit}>
      <label>
        {t('name')}
        <input value={form.name} onChange={set('name')} required />
      </label>
      <label>
        {t('semel')}
        <input dir="ltr" inputMode="numeric" value={form.semel} onChange={set('semel')} required />
      </label>
      <label>
        {t('year')}
        <input dir="ltr" inputMode="numeric" value={form.year} onChange={set('year')} required />
      </label>
      <label>
        {t('username')}
        <input dir="ltr" value={form.username} onChange={set('username')} required />
      </label>
      <label>
        {t('password')}
        <input type="password" autoComplete="off" value={form.password} onChange={set('password')} required />
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
