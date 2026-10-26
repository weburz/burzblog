import { encode } from "uqr";

// A base32 secret in groups of four, which is easier to read and type into an
// authenticator app. Apps ignore the spaces.
export const groupSecret = (secret: string): string =>
  secret.replace(/\s+/g, "").replace(/(.{4})(?=.)/g, "$1 ");

// qrPath draws text as a QR code: the SVG path of its dark modules, one unit
// per module, and the side length including a quiet zone of `border` modules
// on each edge. Medium error correction keeps an otpauth link compact enough
// for phone cameras at small sizes.
export const qrPath = (text: string, border = 2): { d: string; size: number } => {
  const { data, size } = encode(text, { ecc: "M", border });
  let d = "";
  for (let y = 0; y < size; y++) {
    for (let x = 0; x < size; x++) {
      if (data[y]![x]) d += `M${x} ${y}h1v1h-1z`;
    }
  }
  return { d, size };
};
