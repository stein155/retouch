import type * as React from 'react';

// SVG icon set. Kept verbatim from the original Icons.jsx — pure presentational
// atoms, no styling beyond the inline SVG attributes.
export const Icon: Record<string, (props: React.SVGProps<SVGSVGElement>) => React.ReactElement> = {
  search: (p) => (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" {...p}>
      <circle cx="11" cy="11" r="6.5" /><path d="m16 16 4 4" />
    </svg>
  ),
  plus: (p) => (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" {...p}>
      <path d="M12 5v14M5 12h14" />
    </svg>
  ),
  play: (p) => (
    <svg viewBox="0 0 24 24" fill="currentColor" {...p}>
      <path d="M7 5.5v13a.5.5 0 0 0 .77.42l10.5-6.5a.5.5 0 0 0 0-.84L7.77 5.08A.5.5 0 0 0 7 5.5Z"/>
    </svg>
  ),
  stop: (p) => (
    <svg viewBox="0 0 24 24" fill="currentColor" {...p}>
      <rect x="6" y="6" width="12" height="12" rx="1.5"/>
    </svg>
  ),
  more: (p) => (
    <svg viewBox="0 0 24 24" fill="currentColor" {...p}>
      <circle cx="6" cy="12" r="1.6"/><circle cx="12" cy="12" r="1.6"/><circle cx="18" cy="12" r="1.6"/>
    </svg>
  ),
  close: (p) => (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" {...p}>
      <path d="M6 6l12 12M18 6 6 18"/>
    </svg>
  ),
  back: (p) => (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round" {...p}>
      <path d="m14 6-6 6 6 6"/>
    </svg>
  ),
  volume: (p) => (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round" {...p}>
      <path d="M4 10v4h3l4 3.5V6.5L7 10H4Z"/><path d="M15 9.5a3.5 3.5 0 0 1 0 5"/>
    </svg>
  ),
  mute: (p) => (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round" {...p}>
      <path d="M4 10v4h3l4 3.5V6.5L7 10H4Z"/><path d="m15 10 5 4M20 10l-5 4"/>
    </svg>
  ),
  speaker: (p) => (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round" {...p}>
      <rect x="6" y="3" width="12" height="18" rx="2.5"/>
      <circle cx="12" cy="14" r="3.5"/><circle cx="12" cy="6.5" r="0.5" fill="currentColor"/>
    </svg>
  ),
  refresh: (p) => (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round" {...p}>
      <path d="M20 11a8 8 0 1 0-.6 4"/><path d="M20 5v6h-6"/>
    </svg>
  ),
  settings: (p) => (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round" {...p}>
      <circle cx="12" cy="12" r="3"/>
      <path d="M19.1 12.9a7.6 7.6 0 0 0 .06-1.8l1.86-1.45-1.86-3.22-2.2.88a7.5 7.5 0 0 0-1.56-.9L15 4h-3.7l-.4 2.4a7.5 7.5 0 0 0-1.56.9l-2.2-.88-1.86 3.22 1.86 1.45a7.6 7.6 0 0 0 0 1.82l-1.86 1.45 1.86 3.22 2.2-.88c.48.37 1 .67 1.56.9L11.3 20H15l.4-2.4c.56-.23 1.08-.53 1.56-.9l2.2.88 1.86-3.22-1.86-1.45Z"/>
    </svg>
  ),
  chevron: (p) => (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round" {...p}>
      <path d="m9 6 6 6-6 6"/>
    </svg>
  ),
  trash: (p) => (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round" {...p}>
      <path d="M4 7h16M9 7V5a1 1 0 0 1 1-1h4a1 1 0 0 1 1 1v2M6 7l1 13a1 1 0 0 0 1 1h8a1 1 0 0 0 1-1l1-13"/>
    </svg>
  ),
  check: (p) => (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" {...p}>
      <path d="m5 12 5 5L20 7"/>
    </svg>
  ),
  download: (p) => (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round" {...p}>
      <path d="M12 4v10m0 0 4-4m-4 4-4-4M5 18h14"/>
    </svg>
  ),
  wifi: (p) => (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round" {...p}>
      <path d="M2.5 8.5a15 15 0 0 1 19 0"/><path d="M5.5 12a10.5 10.5 0 0 1 13 0"/><path d="M8.5 15.5a6 6 0 0 1 7 0"/><circle cx="12" cy="19" r="0.7" fill="currentColor"/>
    </svg>
  ),
  shield: (p) => (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round" {...p}>
      <path d="M12 3 5 6v5c0 4.4 3 7.6 7 9 4-1.4 7-4.6 7-9V6l-7-3Z"/>
    </svg>
  ),
  lock: (p) => (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round" {...p}>
      <rect x="5" y="10.5" width="14" height="9.5" rx="2.6"/>
      <path d="M8 10.5V8a4 4 0 0 1 8 0v2.5"/>
    </svg>
  ),
  globe: (p) => (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round" {...p}>
      <circle cx="12" cy="12" r="9"/><path d="M3 12h18"/><path d="M12 3c2.4 2.4 3.5 5.7 3.5 9s-1.1 6.6-3.5 9c-2.4-2.4-3.5-5.7-3.5-9S9.6 5.4 12 3Z"/>
    </svg>
  ),
  layers: (p) => (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round" {...p}>
      <path d="m12 3 9 5-9 5-9-5 9-5Z"/><path d="m3 13 9 5 9-5"/>
    </svg>
  ),
};

export default Icon;
