import { useCallback, useEffect, useState } from 'react';
import { api, ApiError, CATEGORIES, CATEGORY_KEY, type DeliveryRow, type EventRow } from '../api';
import { useI18n } from '../i18n';
import { useFlash, Flash } from './shared';

// Activity shows what happened (events) and what was sent or queued (deliveries).
// Failed and skipped deliveries can be retried (CLAUDE.md §8.5).
export function Activity() {
  const { t } = useI18n();
  const flash = useFlash();
  const [category, setCategory] = useState('');
  const [status, setStatus] = useState('');
  const [events, setEvents] = useState<EventRow[] | null>(null);
  const [deliveries, setDeliveries] = useState<DeliveryRow[] | null>(null);

  const load = useCallback(async () => {
    try {
      const q = category ? `?category=${encodeURIComponent(category)}` : '';
      setEvents(await api<EventRow[]>('GET', `/api/v1/events${q}`));
      const dq = status ? `?status=${encodeURIComponent(status)}` : '';
      setDeliveries(await api<DeliveryRow[]>('GET', `/api/v1/deliveries${dq}`));
    } catch (err) {
      flash.error(err instanceof ApiError ? err.detail || err.title : t('error'));
    }
    // flash is stable per mount; the filters drive the reload.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [category, status, t]);

  useEffect(() => {
    void load();
    // A new event arrives on the live stream and refreshes the lists at once. The
    // browser reconnects on its own; the minute refresh covers any gap.
    const stream = new EventSource('/api/v1/events/stream');
    stream.onmessage = () => void load();
    stream.addEventListener('homework', () => void load());
    stream.addEventListener('grades', () => void load());
    stream.addEventListener('behave', () => void load());
    stream.addEventListener('daily_plan', () => void load());
    const timer = window.setInterval(() => void load(), 60_000);
    return () => {
      stream.close();
      window.clearInterval(timer);
    };
  }, [load]);

  async function retry(id: number) {
    try {
      await api('POST', `/api/v1/deliveries/${id}/retry`);
      flash.ok(t('saved'));
      await load();
    } catch (err) {
      flash.error(err instanceof ApiError ? err.detail || err.title : t('error'));
    }
  }

  return (
    <section>
      <Flash state={flash} />
      <div className="row between">
        <h2>{t('eventsTitle')}</h2>
        <label>
          {t('category')}
          <select value={category} onChange={(e) => setCategory(e.target.value)}>
            <option value="">{t('allCategories')}</option>
            {CATEGORIES.map((c) => (
              <option key={c} value={c}>
                {t(CATEGORY_KEY[c])}
              </option>
            ))}
          </select>
        </label>
      </div>
      {events === null ? (
        <p className="muted">{t('loading')}</p>
      ) : events.length === 0 ? (
        <p className="muted">{t('noRows')}</p>
      ) : (
        <table>
          <thead>
            <tr>
              <th>{t('category')}</th>
              <th>{t('kind')}</th>
              <th>{t('whenDetected')}</th>
            </tr>
          </thead>
          <tbody>
            {events.map((e) => (
              <tr key={e.id}>
                <td>{CATEGORY_KEY[e.category] ? t(CATEGORY_KEY[e.category]) : e.category}</td>
                <td>{e.kind}</td>
                <td>{new Date(e.detectedAt).toLocaleString()}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}

      <div className="row between">
        <h2>{t('deliveriesTitle')}</h2>
        <label>
          {t('status')}
          <select value={status} onChange={(e) => setStatus(e.target.value)}>
            <option value="">{t('allStatuses')}</option>
            <option value="pending">pending</option>
            <option value="sent">sent</option>
            <option value="failed">failed</option>
            <option value="skipped_no_channel">skipped_no_channel</option>
          </select>
        </label>
      </div>
      {deliveries === null ? (
        <p className="muted">{t('loading')}</p>
      ) : deliveries.length === 0 ? (
        <p className="muted">{t('noRows')}</p>
      ) : (
        <table>
          <thead>
            <tr>
              <th>#</th>
              <th>{t('channel')}</th>
              <th>{t('status')}</th>
              <th>{t('attempts')}</th>
              <th>{t('lastError')}</th>
              <th>{t('body')}</th>
              <th />
            </tr>
          </thead>
          <tbody>
            {deliveries.map((d) => (
              <tr key={d.id}>
                <td>{d.id}</td>
                <td>{d.channel}</td>
                <td>{d.status}</td>
                <td>{d.attempts}</td>
                <td className="small">{d.lastError}</td>
                <td className="pre small">{d.body}</td>
                <td>
                  {(d.status === 'failed' || d.status === 'skipped_no_channel') && (
                    <button onClick={() => retry(d.id)}>{t('retry')}</button>
                  )}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </section>
  );
}
