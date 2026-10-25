// The server enforces the real rules (auth.MinPassword); this only coaches.
export const MIN_PASSWORD = 12;
const STRONG_LENGTH = 16;

export type PasswordStrength = {
  length: number;
  level: "empty" | "short" | "weak" | "fair" | "strong";
  hint: string;
};

// True for "aaaaaaaaaaaa", "abababababab" and "123123123123": a run of one
// character, or a short unit repeated to fill the whole password.
const isRepetitive = (chars: string[]) => {
  const text = chars.join("");
  if (new Set(chars).size <= 2) return true;
  if (/(.)\1{3,}/u.test(text)) return true;
  for (let size = 1; size <= chars.length / 2; size += 1) {
    if (chars.length % size !== 0) continue;
    if (
      chars
        .slice(0, size)
        .join("")
        .repeat(chars.length / size) === text
    )
      return true;
  }
  return false;
};

export const passwordStrength = (value: string): PasswordStrength => {
  const chars = [...value];
  const length = chars.length;
  if (length === 0) return { length, level: "empty", hint: "" };
  if (length < MIN_PASSWORD) {
    return {
      length,
      level: "short",
      hint: `${MIN_PASSWORD - length} more to reach the ${MIN_PASSWORD}-character minimum.`,
    };
  }
  if (isRepetitive(chars)) {
    return {
      length,
      level: "weak",
      hint: "Weak: repeated characters are easy to guess.",
    };
  }
  if (length < STRONG_LENGTH) {
    return {
      length,
      level: "fair",
      hint: "Fair: a few more characters make it stronger.",
    };
  }
  return { length, level: "strong", hint: "Strong: long and not repetitive." };
};
