import { useMemo } from 'preact/hooks';
import { qrCode, qrPath, quietZone } from '../qr';

/** A QR code, dark on light whatever the theme, with its quiet zone; CSS sets the size. */
export function QrCode({ text, label }: { text: string; label: string }) {
  const { size, d } = useMemo(() => {
    const code = qrCode(text);
    return { size: code.size + 2 * quietZone, d: qrPath(code) };
  }, [text]);
  return (
    <svg class="qr" viewBox={`0 0 ${size} ${size}`} shape-rendering="crispEdges" role="img" aria-label={label}>
      <rect width={size} height={size} fill="#fff" />
      <path d={d} fill="#17120f" />
    </svg>
  );
}
