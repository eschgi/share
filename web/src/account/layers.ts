// Dialogs and the viewer stack up; a toast belongs on the top one, since a modal dialog keeps
// everything outside it out of reach. Each open layer has a place here, the newest last.
import { createContext, type VNode } from 'preact';
import { useEffect, useState } from 'preact/hooks';

let stack: number[] = [];
let lastId = 0;
const listeners = new Set<() => void>();
const changed = () => listeners.forEach((l) => l());

function useStack(): number[] {
  const [, redraw] = useState(0);
  useEffect(() => {
    const l = () => redraw((n) => n + 1);
    listeners.add(l);
    return () => void listeners.delete(l);
  }, []);
  return stack;
}

/** Registers an open dialog; says whether it is the one on top. */
export function useLayer(): boolean {
  const [id] = useState(() => ++lastId);
  const now = useStack();
  useEffect(() => {
    stack = [...stack, id];
    changed();
    return () => {
      stack = stack.filter((x) => x !== id);
      changed();
    };
  }, []);
  return now[now.length - 1] === id;
}

/** Whether any dialog is open. */
export function useAnyLayer(): boolean {
  return useStack().length > 0;
}

/** The toast being shown, for whichever layer is on top to draw. */
export const ToastContext = createContext<VNode | null>(null);
