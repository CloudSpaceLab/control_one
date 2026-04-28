import { useCallback, useEffect, useState } from 'react';

export function useLocalStorage<T>(key: string, initial: T): [T, (next: T | ((prev: T) => T)) => void] {
  const [value, setValue] = useState<T>(() => {
    if (typeof window === 'undefined') return initial;
    try {
      const raw = window.localStorage.getItem(key);
      return raw ? (JSON.parse(raw) as T) : initial;
    } catch {
      return initial;
    }
  });

  useEffect(() => {
    if (typeof window === 'undefined') return;
    try {
      window.localStorage.setItem(key, JSON.stringify(value));
    } catch {
      // ignore quota / serialization errors
    }
  }, [key, value]);

  const update = useCallback(
    (next: T | ((prev: T) => T)) =>
      setValue((prev) => (typeof next === 'function' ? (next as (p: T) => T)(prev) : next)),
    [],
  );

  return [value, update];
}
