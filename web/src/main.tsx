import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import { App } from './App';
import { I18nProvider } from './i18n';
import './styles.css';

const savedLang = localStorage.getItem('m2g-lang') === 'en' ? 'en' : 'he';
document.documentElement.lang = savedLang;
document.documentElement.dir = savedLang === 'he' ? 'rtl' : 'ltr';

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <I18nProvider>
      <App />
    </I18nProvider>
  </StrictMode>,
);
