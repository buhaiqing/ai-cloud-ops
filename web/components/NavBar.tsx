'use client';
import Link from 'next/link';
import { useEffect, useState } from 'react';

// Top navigation. M2-10: dark-mode toggle persists in localStorage.
export default function NavBar() {
  const [dark, setDark] = useState(false);
  useEffect(() => {
    setDark(document.documentElement.classList.contains('dark'));
  }, []);
  const toggle = () => {
    const next = !dark;
    setDark(next);
    document.documentElement.classList.toggle('dark', next);
    try {
      localStorage.setItem('theme', next ? 'dark' : 'light');
    } catch {}
  };
  return (
    <nav className="bg-white dark:bg-gray-900 border-b border-gray-200 dark:border-gray-800">
      <div className="max-w-7xl mx-auto px-4 flex items-center gap-4 h-14">
        <Link href="/" className="font-bold text-lg">AI Cloud Ops</Link>
        <div className="flex gap-1 text-sm">
          <Link href="/alerts" className="px-3 py-1.5 rounded hover:bg-gray-100 dark:hover:bg-gray-800">Alerts</Link>
          <Link href="/resources" className="px-3 py-1.5 rounded hover:bg-gray-100 dark:hover:bg-gray-800">Resources</Link>
          <Link href="/rules" className="px-3 py-1.5 rounded hover:bg-gray-100 dark:hover:bg-gray-800">Rules</Link>
        </div>
        <div className="ml-auto flex items-center gap-2">
          <button
            aria-label="Toggle dark mode"
            onClick={toggle}
            className="px-2 py-1 text-xs rounded border border-gray-300 dark:border-gray-700"
          >
            {dark ? '☀ Light' : '☾ Dark'}
          </button>
        </div>
      </div>
    </nav>
  );
}