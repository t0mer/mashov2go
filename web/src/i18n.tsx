import { createContext, useContext, useMemo, useState, type ReactNode } from 'react';

// Every visible string has an English and a Hebrew form. Hebrew is the default
// (CLAUDE.md §10). The choice is kept in localStorage.
export type Lang = 'he' | 'en';

const S = {
  appName: ['mashov2go', 'mashov2go'],
  activity: ['Activity', 'פעילות'],
  family: ['Family', 'משפחה'],
  accounts: ['Mashov accounts', 'חשבונות משו"ב'],
  channels: ['Channels', 'ערוצים'],
  settings: ['Settings', 'הגדרות'],
  logout: ['Sign out', 'יציאה'],
  language: ['English', 'עברית'],
  theme: ['Theme', 'ערכת נושא'],
  themeSystem: ['System', 'מערכת'],
  themeLight: ['Light', 'בהיר'],
  themeDark: ['Dark', 'כהה'],
  password: ['Password', 'סיסמה'],
  passwordConfirm: ['Confirm password', 'אימות סיסמה'],
  setupTitle: ['Set the admin password', 'הגדרת סיסמת מנהל'],
  setupHint: [
    'Choose a password of at least 12 characters. This instance runs on your LAN.',
    'בחרו סיסמה של לפחות 12 תווים. השירות פועל ברשת הביתית.',
  ],
  setupButton: ['Set password', 'שמירת סיסמה'],
  signInTitle: ['Sign in', 'כניסה'],
  signInButton: ['Sign in', 'כניסה'],
  mismatch: ['The passwords do not match', 'הסיסמאות אינן זהות'],
  save: ['Save', 'שמירה'],
  cancel: ['Cancel', 'ביטול'],
  delete: ['Delete', 'מחיקה'],
  confirmDelete: ['Delete this item?', 'למחוק את הפריט?'],
  add: ['Add', 'הוספה'],
  edit: ['Edit', 'עריכה'],
  close: ['Close', 'סגירה'],
  loading: ['Loading…', 'טוען…'],
  saved: ['Saved', 'נשמר'],
  name: ['Name', 'שם'],
  phone: ['Phone', 'טלפון'],
  channel: ['Channel', 'ערוץ'],
  whatsapp: ['WhatsApp', 'וואטסאפ'],
  telegram: ['Telegram', 'טלגרם'],
  active: ['Active', 'פעיל'],
  inactive: ['Inactive', 'לא פעיל'],
  yes: ['Yes', 'כן'],
  no: ['No', 'לא'],
  children: ['Children', 'ילדים'],
  parents: ['Parents', 'הורים'],
  addChild: ['Add child', 'הוספת ילד'],
  addParent: ['Add parent', 'הוספת הורה'],
  linkedTo: ['Linked to Mashov', 'מקושר למשו"ב'],
  notLinked: ['Not linked', 'לא מקושר'],
  telegramBound: ['Telegram bound', 'טלגרם מחובר'],
  telegramNotBound: ['Telegram not bound', 'טלגרם לא מחובר'],
  planTime: ['Plan time', 'שעת התוכנית'],
  planEnabled: ['Daily plan', 'תוכנית יומית'],
  quietStart: ['Quiet from', 'שקט מ-'],
  quietEnd: ['Quiet until', 'שקט עד'],
  linkTitle: ['Link to a Mashov student', 'קישור לתלמיד במשו"ב'],
  chooseAccount: ['Mashov account', 'חשבון משו"ב'],
  loadStudents: ['Load students', 'טעינת תלמידים'],
  noStudents: ['No students found', 'לא נמצאו תלמידים'],
  unlink: ['Remove link', 'הסרת קישור'],
  recipients: ['Who receives', 'מי מקבל'],
  resetDefaults: ['Reset to defaults', 'איפוס לברירת מחדל'],
  planTitle: ['Daily plan', 'תוכנית יומית'],
  date: ['Date', 'תאריך'],
  preview: ['Preview', 'תצוגה מקדימה'],
  sendNow: ['Send now', 'שליחה עכשיו'],
  sent: ['Queued', 'נוספה לתור'],
  telegramLink: ['Telegram link', 'קישור טלגרם'],
  createLink: ['Create link', 'יצירת קישור'],
  linkNote: ['Open the link on the phone and press Start. It works once and expires in 24 hours.', 'פתחו את הקישור בטלפון ולחצו Start. הקישור חד-פעמי ותקף 24 שעות.'],
  mutes: ['Muted categories', 'קטגוריות מושתקות'],
  accountsTitle: ['Mashov accounts', 'חשבונות משו"ב'],
  addAccount: ['Add account', 'הוספת חשבון'],
  semel: ['School code (semel)', 'סמל מוסד'],
  year: ['School year', 'שנת לימודים'],
  username: ['Username', 'שם משתמש'],
  authState: ['Auth state', 'מצב התחברות'],
  studentsFound: ['Students found', 'תלמידים שנמצאו'],
  connectorsTitle: ['Channels', 'ערוצים'],
  addConnector: ['Add channel', 'הוספת ערוץ'],
  type: ['Type', 'סוג'],
  configJson: ['Configuration (JSON)', 'הגדרות (JSON)'],
  makeDefault: ['Make default', 'קביעה כברירת מחדל'],
  isDefault: ['Default', 'ברירת מחדל'],
  enabled: ['Enabled', 'מופעל'],
  health: ['Health', 'בריאות'],
  testSend: ['Test message', 'הודעת בדיקה'],
  chooseMember: ['Send to', 'שליחה אל'],
  generalTitle: ['General', 'כללי'],
  passwordTitle: ['Change password', 'שינוי סיסמה'],
  currentPassword: ['Current password', 'סיסמה נוכחית'],
  newPassword: ['New password', 'סיסמה חדשה'],
  changePassword: ['Change password', 'שינוי סיסמה'],
  tokensTitle: ['API tokens', 'טוקני API'],
  tokenName: ['Token name', 'שם הטוקן'],
  scope: ['Scope', 'הרשאה'],
  scopeRead: ['Read', 'קריאה'],
  scopeAdmin: ['Admin', 'מנהל'],
  createToken: ['Create token', 'יצירת טוקן'],
  tokenShownOnce: ['Copy this token now. It will not be shown again.', 'העתיקו את הטוקן עכשיו. לא יוצג שוב.'],
  revoke: ['Revoke', 'ביטול'],
  revoked: ['Revoked', 'בוטל'],
  eventsTitle: ['Events', 'אירועים'],
  deliveriesTitle: ['Deliveries', 'הודעות יוצאות'],
  category: ['Category', 'קטגוריה'],
  status: ['Status', 'סטטוס'],
  allStatuses: ['All', 'הכול'],
  allCategories: ['All', 'הכול'],
  retry: ['Retry', 'נסיון חוזר'],
  noRows: ['Nothing here yet', 'אין עדיין פריטים'],
  error: ['Something went wrong', 'משהו השתבש'],
  secondsAgo: ['just now', 'עכשיו'],
  body: ['Message', 'הודעה'],
  attempts: ['Attempts', 'ניסיונות'],
  lastError: ['Last error', 'שגיאה אחרונה'],
  generalSaved: ['General settings saved', 'ההגדרות הכלליות נשמרו'],
  passwordChanged: ['Password changed. Other sessions were signed out.', 'הסיסמה שונתה. שאר ההתחברויות נותקו.'],
  noSchool: ['No school day to plan', 'אין יום לימודים לתכנון'],
  languageField: ['Language', 'שפה'],
  kind: ['Kind', 'סוג'],
  whenDetected: ['Detected', 'זוהה'],
  recipientChild: ['Child', 'ילד/ה'],
  recipientParents: ['Parents', 'הורים'],
  recipientBoth: ['Both', 'שניהם'],
  recipientOff: ['Off', 'כבוי'],
  catDailyPlan: ['Daily plan', 'תוכנית יומית'],
  catHomework: ['Homework', 'שיעורי בית'],
  catTimetableChange: ['Timetable change', 'שינוי במערכת'],
  catGrades: ['Grades', 'ציונים'],
  catBehave: ['Behaviour', 'התנהגות'],
  catMaakav: ['Teacher notes', 'מעקב מורה'],
  catMail: ['Mail', 'דואר'],
  catSchoolNotice: ['School notice', 'הודעת בית ספר'],
  catForms: ['Forms', 'טפסים'],
  catSystemAuth: ['Login problems', 'בעיות התחברות'],
  setTimezone: ['Timezone', 'אזור זמן'],
  setPlanDefaultTime: ['Default plan time', 'שעת תוכנית ברירת מחדל'],
  setActiveHours: ['Active hours', 'שעות פעילות'],
  setFastSync: ['Fast sync interval', 'מרווח סנכרון מהיר'],
  setFullSync: ['Full sync interval', 'מרווח סנכרון מלא'],
  setNightSync: ['Night sync interval', 'מרווח סנכרון לילה'],
  setMinGap: ['Minimum gap between Mashov requests', 'מרווח מינימלי בין בקשות למשו"ב'],
  setRetention: ['Events retention', 'שמירת אירועים'],
  setProfile: ['Mashov app profile', 'פרופיל אפליקציית משו"ב'],
  setKeepUnread: ['Keep mail unread', 'שמירת דואר כלא נקרא'],
  setShabbat: ['Shabbat mode', 'מצב שבת'],
  linkThis: ['Link', 'קישור'],
} as const;

export type Key = keyof typeof S;

interface I18nValue {
  lang: Lang;
  setLang: (l: Lang) => void;
  t: (k: Key) => string;
}

const Ctx = createContext<I18nValue | null>(null);

export function I18nProvider({ children }: { children: ReactNode }) {
  const [lang, setLangState] = useState<Lang>(() => {
    const saved = localStorage.getItem('m2g-lang');
    return saved === 'en' ? 'en' : 'he';
  });
  const value = useMemo<I18nValue>(() => {
    const idx = lang === 'he' ? 1 : 0;
    return {
      lang,
      setLang: (l) => {
        localStorage.setItem('m2g-lang', l);
        document.documentElement.lang = l;
        document.documentElement.dir = l === 'he' ? 'rtl' : 'ltr';
        setLangState(l);
      },
      t: (k) => S[k][idx],
    };
  }, [lang]);
  return <Ctx.Provider value={value}>{children}</Ctx.Provider>;
}

export function useI18n(): I18nValue {
  const v = useContext(Ctx);
  if (!v) throw new Error('useI18n outside I18nProvider');
  return v;
}
