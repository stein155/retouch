import type * as React from 'react';
import { useMemo } from 'react';
import { encode } from '../../../lib/qr';

type Props = {
  value: string;
  size?: number;
  quiet?: number;
  color?: string;
};

// Renders `value` as a QR-code SVG (dark modules on a transparent ground, so it
// sits on any light card). All modules are drawn as one <path> of 1×1 squares in
// module coordinates, scaled by the viewBox — crisp at any size. Renders nothing
// if the value is empty or can't be encoded.
export function QRCode({
  value,
  size = 150,
  quiet = 4,
  color = 'var(--ink)',
}: Props): React.ReactElement | null {
  const qr = useMemo(() => {
    if (!value) return null;
    try {
      const { size: n, modules } = encode(value);
      let d = '';
      for (let r = 0; r < n; r++) {
        for (let c = 0; c < n; c++) {
          if (modules[r][c]) d += `M${c + quiet} ${r + quiet}h1v1h-1z`;
        }
      }
      return { d, dim: n + quiet * 2 };
    } catch {
      return null;
    }
  }, [value, quiet]);

  if (!qr) return null;
  return (
    <svg
      width={size}
      height={size}
      viewBox={`0 0 ${qr.dim} ${qr.dim}`}
      shapeRendering="crispEdges"
      role="img"
      aria-hidden="true"
    >
      <path d={qr.d} fill={color} />
    </svg>
  );
}
