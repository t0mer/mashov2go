import { useEffect, useState } from 'react';
import { api, setCsrf, type Me } from './api';
import { useI18n } from './i18n';
import { Login } from './pages/Login';
import { Activity } from './pages/Activity';
import { Family } from './pages/Family';
import { Accounts } from './pages/Accounts';
import { Channels } from './pages/Channels';
import { SettingsPage } from './pages/SettingsPage';

type Phase = 'loading' | 'setup' | 'login' | 'ready';
type Tab = 'activity' | 'family' | 'accounts' | 'channels' | 'settings';
type ThemeChoice = 'system' | 'light' | 'dark';

function applyTheme(choice: ThemeChoice): void {
  if (choice === 'system') delete document.documentElement.dataset.theme;
  else document.documentElement.dataset.theme = choice;
}

export function App() {
  const { t, lang, setLang } = useI18n();
  const [phase, setPhase] = useState<Phase>('loading');
  const [tab, setTab] = useState<Tab>('activity');
  const [me, setMe] = useState<Me | null>(null);
  const [theme, setTheme] = useState<ThemeChoice>(
    () => (localStorage.getItem('m2g-theme') as ThemeChoice | null) ?? 'system',
  );

  useEffect(() => applyTheme(theme), [theme]);

  // On load: a live session resumes; otherwise the app is either not yet set up or needs sign-in.
  useEffect(() => {
    let cancelled = false;
    (async () => {
      try {
        const who = await api<Me>('GET', '/api/v1/auth/me');
        if (cancelled) return;
        setCsrf(who.csrfToken ?? '');
        setMe(who);
        setPhase('ready');
      } catch {
        try {
          const s = await api<{ configured: boolean }>('GET', '/api/v1/setup');
          if (!cancelled) setPhase(s.configured ? 'login' : 'setup');
        } catch {
          if (!cancelled) setPhase('login');
        }
      }
    })();
    return () => {
      cancelled = true;
    };
  }, []);

  function signedIn(csrf: string): void {
    setCsrf(csrf);
    setMe({ kind: 'session', scope: 'admin', csrfToken: csrf });
    setPhase('ready');
  }

  async function signOut(): Promise<void> {
    try {
      await api('POST', '/api/v1/auth/logout');
    } finally {
      setCsrf('');
      setMe(null);
      setPhase('login');
    }
  }

  if (phase === 'loading') return <p className="center muted">{t('loading')}</p>;
  if (phase === 'setup' || phase === 'login') {
    return <Login mode={phase} onSignedIn={signedIn} />;
  }

  const tabs: { key: Tab; label: string }[] = [
    { key: 'activity', label: t('activity') },
    { key: 'family', label: t('family') },
    { key: 'accounts', label: t('accounts') },
    { key: 'channels', label: t('channels') },
    { key: 'settings', label: t('settings') },
  ];

  return (
    <div className="shell">
      <header className="topbar">
        <strong className="brand">{t('appName')}</strong>
        <nav aria-label="main">
          {tabs.map((x) => (
            <button
              key={x.key}
              className={x.key === tab ? 'tab active' : 'tab'}
              onClick={() => setTab(x.key)}
            >
              {x.label}
            </button>
          ))}
        </nav>
        <div className="controls">
          <select
            aria-label={t('theme')}
            value={theme}
            onChange={(e) => {
              const v = e.target.value as ThemeChoice;
              localStorage.setItem('m2g-theme', v);
              setTheme(v);
            }}
          >
            <option value="system">{t('themeSystem')}</option>
            <option value="light">{t('themeLight')}</option>
            <option value="dark">{t('themeDark')}</option>
          </select>
          <button className="ghost" onClick={() => setLang(lang === 'he' ? 'en' : 'he')}>
            {t('language')}
          </button>
          {me?.kind === 'session' && (
            <button className="ghost" onClick={signOut}>
              {t('logout')}
            </button>
          )}
        </div>
      </header>
      <main>
        {tab === 'activity' && <Activity />}
        {tab === 'family' && <Family />}
        {tab === 'accounts' && <Accounts />}
        {tab === 'channels' && <Channels />}
        {tab === 'settings' && <SettingsPage />}
      </main>
    </div>
  );
}
