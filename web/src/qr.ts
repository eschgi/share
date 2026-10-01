// QR codes for invite links. lean-qr only works out the modules; drawing them is ours, as in
// the app (QrCodeView in app/lib/ui/widgets.dart), which also uses correction level M.
import { correction, generate } from 'lean-qr/nano';

/** Dark and light modules, size × size. */
export interface QrModules {
  readonly size: number;
  get(x: number, y: number): boolean;
}

/** The light margin around a QR code that scanners need, in modules. */
export const quietZone = 4;

export function qrCode(text: string): QrModules {
  return generate(text, { minCorrectionLevel: correction.M });
}

/** The dark modules as one SVG path, a rectangle per run in a row, inside the quiet zone. */
export function qrPath(code: QrModules): string {
  let d = '';
  for (let y = 0; y < code.size; y++) {
    for (let x = 0; x < code.size; ) {
      if (!code.get(x, y)) {
        x++;
        continue;
      }
      let end = x + 1;
      while (end < code.size && code.get(end, y)) end++;
      d += `M${x + quietZone} ${y + quietZone}h${end - x}v1h-${end - x}z`;
      x = end;
    }
  }
  return d;
}
