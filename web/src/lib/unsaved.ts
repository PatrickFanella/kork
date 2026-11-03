import { useEffect, useId } from "react";

// A registry of editor forms with unsaved changes (#54). Forms report their
// state with useUnsaved; leaving checks it with confirmLeave, and closing or
// reloading the browser tab gets the browser's own warning.

// id → whether leaving within the app should ask. A form that saves itself
// when it closes (the theme tab) only needs the browser's close-tab warning.
const dirty = new Map<string, boolean>();

function onBeforeUnload(e: BeforeUnloadEvent) {
  if (dirty.size === 0) return;
  e.preventDefault();
  // Older browsers need returnValue set to show the prompt.
  e.returnValue = "";
}

let listening = false;

/**
 * Marks the calling form as having unsaved changes while `isDirty` is true.
 * Pass askInApp=false for a form that saves itself when it closes.
 */
export function useUnsaved(isDirty: boolean, askInApp = true): void {
  const id = useId();
  useEffect(() => {
    if (!listening && typeof window !== "undefined") {
      window.addEventListener("beforeunload", onBeforeUnload);
      listening = true;
    }
    if (isDirty) dirty.set(id, askInApp);
    else dirty.delete(id);
    return () => {
      dirty.delete(id);
    };
  }, [id, isDirty, askInApp]);
}

/** Whether a form would lose changes if the user left within the app. */
export function hasUnsaved(): boolean {
  for (const ask of dirty.values()) if (ask) return true;
  return false;
}

/** True when it's fine to leave: nothing is unsaved, or the user agrees. */
export function confirmLeave(ask: (message: string) => boolean = (m) => window.confirm(m)): boolean {
  return !hasUnsaved() || ask("You have unsaved changes. Leave without saving them?");
}

/** For tests. */
export function markForTest(id: string, isDirty: boolean, askInApp = true): void {
  if (isDirty) dirty.set(id, askInApp);
  else dirty.delete(id);
}
