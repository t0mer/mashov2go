import { useCallback, useEffect, useState, type FormEvent } from 'react';
import {
  api,
  ApiError,
  CATEGORIES,
  RECIPIENTS,
  type Account,
  type Member,
  type Plan,
  type Student,
} from '../api';
import { useI18n } from '../i18n';
import { Flash, useFlash, type FlashState } from './shared';

type Role = 'child' | 'parent';

function errText(err: unknown, fallback: string): string {
  return err instanceof ApiError ? err.detail || err.title : fallback;
}

// Family manages children and parents (CLAUDE.md §6). A child has a Mashov link,
// recipients per category, a plan preview and a Telegram link; a parent has mutes.
export function Family() {
  const { t } = useI18n();
  const flash = useFlash();
  const [role, setRole] = useState<Role>('child');
  const [members, setMembers] = useState<Member[] | null>(null);
  const [selected, setSelected] = useState<Member | null>(null);

  const load = useCallback(async () => {
    try {
      const list = await api<Member[]>('GET', `/api/v1/${role === 'child' ? 'children' : 'parents'}`);
      setMembers(list);
    } catch (err) {
      flash.error(errText(err, t('error')));
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [role, t]);

  useEffect(() => {
    setSelected(null);
    void load();
  }, [load]);

  return (
    <section>
      <Flash state={flash} />
      <div className="row between">
        <div className="segmented">
          <button className={role === 'child' ? 'active' : ''} onClick={() => setRole('child')}>
            {t('children')}
          </button>
          <button className={role === 'parent' ? 'active' : ''} onClick={() => setRole('parent')}>
            {t('parents')}
          </button>
        </div>
      </div>

      {members === null ? (
        <p className="muted">{t('loading')}</p>
      ) : (
        <table>
          <thead>
            <tr>
              <th>{t('name')}</th>
              <th>{t('phone')}</th>
              <th>{t('channel')}</th>
              <th>{t('active')}</th>
              {role === 'child' && <th>{t('linkedTo')}</th>}
            </tr>
          </thead>
          <tbody>
            {members.map((m) => (
              <tr key={m.id} className={selected?.id === m.id ? 'selected' : ''} onClick={() => setSelected(m)}>
                <td>{m.name}</td>
                <td dir="ltr">{m.whatsappPhone}</td>
                <td>{m.channel}</td>
                <td>{m.active ? t('yes') : t('no')}</td>
                {role === 'child' && <td>{m.link ? t('linkedTo') : t('notLinked')}</td>}
              </tr>
            ))}
          </tbody>
        </table>
      )}

      <AddMember role={role} onAdded={() => void load()} flash={flash} />

      {selected && (
        <MemberPanel
          key={selected.id}
          member={selected}
          allMembers={members ?? []}
          onChanged={(m) => {
            setSelected(m);
            void load();
          }}
          onDeleted={() => {
            setSelected(null);
            void load();
          }}
          flash={flash}
        />
      )}
    </section>
  );
}

function AddMember({ role, onAdded, flash }: { role: Role; onAdded: () => void; flash: FlashState }) {
  const { t } = useI18n();
  const [name, setName] = useState('');
  const [phone, setPhone] = useState('');
  const [language, setLanguage] = useState<'he' | 'en'>('he');
  const [open, setOpen] = useState(false);

  async function submit(e: FormEvent) {
    e.preventDefault();
    try {
      await api('POST', `/api/v1/${role === 'child' ? 'children' : 'parents'}`, {
        name,
        whatsappPhone: phone,
        language,
      });
      setName('');
      setPhone('');
      flash.ok(t('saved'));
      onAdded();
    } catch (err) {
      flash.error(errText(err, t('error')));
    }
  }

  if (!open) {
    return (
      <button className="primary" onClick={() => setOpen(true)}>
        {role === 'child' ? t('addChild') : t('addParent')}
      </button>
    );
  }
  return (
    <form className="card stack" onSubmit={submit}>
      <label>
        {t('name')}
        <input value={name} onChange={(e) => setName(e.target.value)} required />
      </label>
      <label>
        {t('phone')}
        <input dir="ltr" value={phone} onChange={(e) => setPhone(e.target.value)} placeholder="050-1234567" required />
      </label>
      <label>
        {t('languageField')}
        <select value={language} onChange={(e) => setLanguage(e.target.value as 'he' | 'en')}>
          <option value="he">עברית</option>
          <option value="en">English</option>
        </select>
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

function MemberPanel({
  member,
  allMembers,
  onChanged,
  onDeleted,
  flash,
}: {
  member: Member;
  allMembers: Member[];
  onChanged: (m: Member) => void;
  onDeleted: () => void;
  flash: FlashState;
}) {
  const { t } = useI18n();
  const [draft, setDraft] = useState({
    name: member.name,
    phone: member.whatsappPhone,
    planTime: member.planTime,
    planEnabled: member.planEnabled,
    quietStart: member.quietStart,
    quietEnd: member.quietEnd,
    active: member.active,
  });

  async function save(e: FormEvent) {
    e.preventDefault();
    try {
      const updated = await api<Member>('PATCH', `/api/v1/${member.role === 'child' ? 'children' : 'parents'}/${member.id}`, {
        name: draft.name,
        whatsappPhone: draft.phone,
        planTime: draft.planTime,
        planEnabled: draft.planEnabled,
        quietStart: draft.quietStart,
        quietEnd: draft.quietEnd,
        active: draft.active,
      });
      flash.ok(t('saved'));
      onChanged(updated);
    } catch (err) {
      flash.error(errText(err, t('error')));
    }
  }

  async function remove() {
    if (!window.confirm(t('confirmDelete'))) return;
    try {
      await api('DELETE', `/api/v1/${member.role === 'child' ? 'children' : 'parents'}/${member.id}`);
      onDeleted();
    } catch (err) {
      flash.error(errText(err, t('error')));
    }
  }

  return (
    <div className="card stack panel">
      <h3>{member.name}</h3>
      <form className="stack" onSubmit={save}>
        <label>
          {t('name')}
          <input value={draft.name} onChange={(e) => setDraft({ ...draft, name: e.target.value })} required />
        </label>
        <label>
          {t('phone')}
          <input dir="ltr" value={draft.phone} onChange={(e) => setDraft({ ...draft, phone: e.target.value })} />
        </label>
        {member.role === 'child' && (
          <>
            <label>
              {t('planTime')}
              <input
                type="time"
                value={draft.planTime}
                onChange={(e) => setDraft({ ...draft, planTime: e.target.value })}
              />
            </label>
            <label className="check">
              <input
                type="checkbox"
                checked={draft.planEnabled}
                onChange={(e) => setDraft({ ...draft, planEnabled: e.target.checked })}
              />
              {t('planEnabled')}
            </label>
          </>
        )}
        <label>
          {t('quietStart')}
          <input type="time" value={draft.quietStart} onChange={(e) => setDraft({ ...draft, quietStart: e.target.value })} />
        </label>
        <label>
          {t('quietEnd')}
          <input type="time" value={draft.quietEnd} onChange={(e) => setDraft({ ...draft, quietEnd: e.target.value })} />
        </label>
        <label className="check">
          <input type="checkbox" checked={draft.active} onChange={(e) => setDraft({ ...draft, active: e.target.checked })} />
          {t('active')}
        </label>
        <div className="row">
          <button type="submit" className="primary">
            {t('save')}
          </button>
          <button type="button" className="danger" onClick={remove}>
            {t('delete')}
          </button>
        </div>
      </form>

      <TelegramLink member={member} flash={flash} />

      {member.role === 'child' ? (
        <>
          <ChildLink member={member} onChanged={onChanged} flash={flash} />
          <Recipients member={member} flash={flash} />
          <PlanTools member={member} flash={flash} />
        </>
      ) : (
        <Mutes parent={member} children={allMembers.filter((m) => m.role === 'child')} flash={flash} />
      )}
    </div>
  );
}

function TelegramLink({ member, flash }: { member: Member; flash: FlashState }) {
  const { t } = useI18n();
  const [url, setUrl] = useState('');
  async function create() {
    try {
      const base = member.role === 'child' ? 'children' : 'parents';
      const res = await api<{ url: string }>('POST', `/api/v1/${base}/${member.id}/telegram-link`);
      setUrl(res.url);
    } catch (err) {
      flash.error(errText(err, t('error')));
    }
  }
  return (
    <div className="stack">
      <strong>{member.telegramBound ? t('telegramBound') : t('telegramNotBound')}</strong>
      <button onClick={create}>{t('createLink')}</button>
      {url && (
        <>
          <p className="muted">{t('linkNote')}</p>
          <input readOnly dir="ltr" value={url} onFocus={(e) => e.target.select()} />
        </>
      )}
    </div>
  );
}

function ChildLink({ member, onChanged, flash }: { member: Member; onChanged: (m: Member) => void; flash: FlashState }) {
  const { t } = useI18n();
  const [accounts, setAccounts] = useState<Account[]>([]);
  const [accountId, setAccountId] = useState<number>(member.link?.accountId ?? 0);
  const [students, setStudents] = useState<Student[] | null>(null);
  const base = `/api/v1/children/${member.id}`;

  useEffect(() => {
    api<Account[]>('GET', '/api/v1/accounts')
      .then((list) => {
        setAccounts(list);
        if (!accountId && list.length) setAccountId(list[0].id);
      })
      .catch((err) => flash.error(errText(err, t('error'))));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  async function loadStudents() {
    try {
      setStudents(await api<Student[]>('GET', `/api/v1/accounts/${accountId}/students`));
    } catch (err) {
      flash.error(errText(err, t('error')));
    }
  }

  async function link(guid: string) {
    try {
      const updated = await api<Member>('POST', `${base}/link`, { accountId, childGuid: guid });
      flash.ok(t('saved'));
      onChanged(updated);
    } catch (err) {
      flash.error(errText(err, t('error')));
    }
  }

  async function unlink() {
    try {
      await api('DELETE', `${base}/link`);
      flash.ok(t('saved'));
      onChanged({ ...member, link: undefined });
    } catch (err) {
      flash.error(errText(err, t('error')));
    }
  }

  return (
    <div className="stack">
      <h4>{t('linkTitle')}</h4>
      {member.link && <p>{t('linkedTo')}: <span dir="ltr">{member.link.childGuid}</span></p>}
      {accounts.length === 0 ? (
        <p className="muted">{t('accounts')}</p>
      ) : (
        <div className="row">
          <select value={accountId} onChange={(e) => setAccountId(Number(e.target.value))}>
            {accounts.map((a) => (
              <option key={a.id} value={a.id}>
                {a.name} ({a.username})
              </option>
            ))}
          </select>
          <button onClick={loadStudents}>{t('loadStudents')}</button>
        </div>
      )}
      {students !== null &&
        (students.length === 0 ? (
          <p className="muted">{t('noStudents')}</p>
        ) : (
          <ul className="plain">
            {students.map((s) => (
              <li key={s.childGuid} className="row between">
                <span>
                  {s.name} {s.familyName}
                </span>
                <button onClick={() => link(s.childGuid)}>{t('linkThis')}</button>
              </li>
            ))}
          </ul>
        ))}
      {member.link && <button className="danger" onClick={unlink}>{t('unlink')}</button>}
    </div>
  );
}

function Recipients({ member, flash }: { member: Member; flash: FlashState }) {
  const { t } = useI18n();
  const base = `/api/v1/children/${member.id}/recipients`;
  const [values, setValues] = useState<Record<string, string> | null>(null);

  useEffect(() => {
    api<Record<string, string>>('GET', base)
      .then(setValues)
      .catch((err) => flash.error(errText(err, t('error'))));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [member.id]);

  async function save(category: string, value: string) {
    try {
      setValues(await api<Record<string, string>>('PUT', base, { [category]: value }));
    } catch (err) {
      flash.error(errText(err, t('error')));
    }
  }

  async function reset() {
    try {
      setValues(await api<Record<string, string>>('POST', `${base}/reset`));
    } catch (err) {
      flash.error(errText(err, t('error')));
    }
  }

  if (!values) return <p className="muted">{t('loading')}</p>;
  return (
    <div className="stack">
      <h4>{t('recipients')}</h4>
      <table>
        <tbody>
          {CATEGORIES.map((c) => (
            <tr key={c}>
              <td>{c}</td>
              <td>
                <select value={values[c]} onChange={(e) => void save(c, e.target.value)}>
                  {RECIPIENTS.map((r) => (
                    <option key={r} value={r}>
                      {r}
                    </option>
                  ))}
                </select>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
      <button onClick={reset}>{t('resetDefaults')}</button>
    </div>
  );
}

function PlanTools({ member, flash }: { member: Member; flash: FlashState }) {
  const { t } = useI18n();
  const [date, setDate] = useState('');
  const [plan, setPlan] = useState<Plan | null>(null);
  const base = `/api/v1/children/${member.id}/plan`;

  async function preview() {
    try {
      const q = date ? `?date=${date}` : '';
      setPlan(await api<Plan>('GET', `${base}${q}`));
    } catch (err) {
      flash.error(errText(err, t('error')));
    }
  }

  async function sendNow() {
    try {
      await api('POST', `${base}/send`);
      flash.ok(t('sent'));
    } catch (err) {
      flash.error(err instanceof ApiError && err.status === 409 ? t('noSchool') : errText(err, t('error')));
    }
  }

  return (
    <div className="stack">
      <h4>{t('planTitle')}</h4>
      <div className="row">
        <input type="date" value={date} onChange={(e) => setDate(e.target.value)} />
        <button onClick={preview}>{t('preview')}</button>
        <button onClick={sendNow}>{t('sendNow')}</button>
      </div>
      {plan && (
        <div className="card">
          <p>
            {plan.date}
            {plan.skip ? ` — ${plan.skip}${plan.holidayName ? ` (${plan.holidayName})` : ''}` : ''}
          </p>
          <ol>
            {plan.lessons.map((l) => (
              <li key={l.num} className={l.cancelled ? 'cancelled' : ''}>
                {l.start}–{l.end} {l.subject}
                {l.room ? ` · ${l.room}` : ''}
                {l.note ? ` ${l.note}` : ''}
              </li>
            ))}
          </ol>
        </div>
      )}
    </div>
  );
}

function Mutes({ parent, children, flash }: { parent: Member; children: Member[]; flash: FlashState }) {
  const { t } = useI18n();
  const base = `/api/v1/parents/${parent.id}/mutes`;
  const [muted, setMuted] = useState<Set<string> | null>(null);

  useEffect(() => {
    api<{ childId: number; category: string }[]>('GET', base)
      .then((list) => setMuted(new Set(list.map((m) => `${m.childId}|${m.category}`))))
      .catch((err) => flash.error(errText(err, t('error'))));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [parent.id]);

  async function toggle(childId: number, category: string) {
    if (!muted) return;
    const key = `${childId}|${category}`;
    const next = new Set(muted);
    if (next.has(key)) next.delete(key);
    else next.add(key);
    const mutes = [...next].map((k) => {
      const [c, cat] = k.split('|');
      return { childId: Number(c), category: cat };
    });
    try {
      await api('PUT', base, { mutes });
      setMuted(next);
    } catch (err) {
      flash.error(errText(err, t('error')));
    }
  }

  if (!muted) return <p className="muted">{t('loading')}</p>;
  return (
    <div className="stack">
      <h4>{t('mutes')}</h4>
      <div className="scroll">
        <table>
          <thead>
            <tr>
              <th />
              {CATEGORIES.map((c) => (
                <th key={c} className="small">
                  {c}
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {children.map((k) => (
              <tr key={k.id}>
                <td>{k.name}</td>
                {CATEGORIES.map((c) => (
                  <td key={c}>
                    <input
                      type="checkbox"
                      aria-label={`${k.name} ${c}`}
                      checked={muted.has(`${k.id}|${c}`)}
                      onChange={() => void toggle(k.id, c)}
                    />
                  </td>
                ))}
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  );
}
