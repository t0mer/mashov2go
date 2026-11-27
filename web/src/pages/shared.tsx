import { useState } from 'react';

// A transient message line for a page: success or error, cleared on the next action.
export interface FlashState {
  message: string;
  tone: 'ok' | 'error' | '';
  ok: (m: string) => void;
  error: (m: string) => void;
  clear: () => void;
}

export function useFlash(): FlashState {
  const [message, setMessage] = useState('');
  const [tone, setTone] = useState<'ok' | 'error' | ''>('');
  return {
    message,
    tone,
    ok: (m) => {
      setTone('ok');
      setMessage(m);
    },
    error: (m) => {
      setTone('error');
      setMessage(m);
    },
    clear: () => {
      setTone('');
      setMessage('');
    },
  };
}

export function Flash({ state }: { state: FlashState }) {
  if (!state.message) return null;
  return (
    <p className={state.tone === 'error' ? 'error' : 'ok'} role={state.tone === 'error' ? 'alert' : 'status'}>
      {state.message}
    </p>
  );
}
