// The API client. Every call goes to /api/v1 with the session cookie; state
// changing calls also send the session's CSRF token (CLAUDE.md §9).

export class ApiError extends Error {
  status: number;
  title: string;
  detail: string;
  constructor(status: number, title: string, detail: string) {
    super(detail || title);
    this.status = status;
    this.title = title;
    this.detail = detail;
  }
}

let csrfToken = '';

export function setCsrf(token: string): void {
  csrfToken = token;
}

export async function api<T>(method: string, path: string, body?: unknown): Promise<T> {
  const headers: Record<string, string> = { Accept: 'application/json' };
  if (body !== undefined) headers['Content-Type'] = 'application/json';
  if (method !== 'GET' && csrfToken) headers['X-CSRF-Token'] = csrfToken;
  const res = await fetch(path, {
    method,
    headers,
    credentials: 'same-origin',
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  if (res.status === 204) return undefined as T;
  const text = await res.text();
  let data: unknown = undefined;
  if (text) {
    try {
      data = JSON.parse(text);
    } catch {
      data = undefined;
    }
  }
  if (!res.ok) {
    const p = (data ?? {}) as { title?: string; detail?: string };
    throw new ApiError(res.status, p.title ?? res.statusText, p.detail ?? '');
  }
  return data as T;
}

// Types mirror the JSON the API returns (internal/api).

export interface Me {
  kind: string;
  scope: string;
  csrfToken?: string;
}

export interface Member {
  id: number;
  role: 'parent' | 'child';
  name: string;
  language: 'he' | 'en';
  whatsappPhone: string;
  telegramBound: boolean;
  channel: 'whatsapp' | 'telegram';
  whatsappConnectorId: number;
  fallbackChannel: string;
  quietStart: string;
  quietEnd: string;
  planEnabled: boolean;
  planTime: string;
  includeHomeworkToday: boolean;
  active: boolean;
  link?: { accountId: number; childGuid: string };
}

export interface Account {
  id: number;
  name: string;
  semel: number;
  year: number;
  username: string;
  authState: string;
}

export interface Student {
  childGuid: string;
  name: string;
  familyName: string;
}

export interface Connector {
  id: number;
  type: 'greenapi' | 'openwa' | 'telegram';
  name: string;
  isDefault: boolean;
  enabled: boolean;
}

export interface GeneralSettings {
  timezone: string;
  mashovMinGap: string;
  mashovProfile: string;
  syncFastInterval: string;
  syncFullInterval: string;
  syncNightInterval: string;
  activeHours: string;
  planDefaultTime: string;
  eventsRetention: string;
  preserveUnread: boolean;
  shabbatMode: boolean;
}

export interface EventRow {
  id: number;
  category: string;
  kind: string;
  occurredAt: string;
  detectedAt: string;
  source: string;
  data: string;
}

export interface DeliveryRow {
  id: number;
  eventId: number;
  memberId: number;
  channel: string;
  status: string;
  attempts: number;
  nextAt: string;
  lastError: string;
  body: string;
}

export interface PlanLesson {
  num: number;
  start: string;
  end: string;
  subject: string;
  room: string;
  note: string;
  cancelled: boolean;
}

export interface Plan {
  date: string;
  skip: string;
  holidayName: string;
  lessons: PlanLesson[];
  homework: { subject: string; text: string }[];
}

export interface Token {
  id: number;
  name: string;
  scope: string;
  createdAt: string;
  lastUsedAt: string | null;
  revokedAt: string | null;
}

export const CATEGORIES = [
  'daily_plan',
  'homework',
  'timetable_change',
  'grades',
  'behave',
  'maakav',
  'mail',
  'school_notice',
  'forms',
  'system.auth',
] as const;

export const RECIPIENTS = ['child', 'parents', 'both', 'off'] as const;

// Message category keys (CLAUDE.md §6.2) and recipient values map to these
// translation keys; the raw keys are not shown to users.
export const CATEGORY_KEY: Record<string, 'catDailyPlan' | 'catHomework' | 'catTimetableChange' | 'catGrades' | 'catBehave' | 'catMaakav' | 'catMail' | 'catSchoolNotice' | 'catForms' | 'catSystemAuth'> = {
  daily_plan: 'catDailyPlan',
  homework: 'catHomework',
  timetable_change: 'catTimetableChange',
  grades: 'catGrades',
  behave: 'catBehave',
  maakav: 'catMaakav',
  mail: 'catMail',
  school_notice: 'catSchoolNotice',
  forms: 'catForms',
  'system.auth': 'catSystemAuth',
};

export const RECIPIENT_KEY: Record<string, 'recipientChild' | 'recipientParents' | 'recipientBoth' | 'recipientOff'> = {
  child: 'recipientChild',
  parents: 'recipientParents',
  both: 'recipientBoth',
  off: 'recipientOff',
};
