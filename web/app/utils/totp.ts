// A base32 secret in groups of four, which is easier to read and type into an
// authenticator app. Apps ignore the spaces.
export const groupSecret = (secret: string): string =>
  secret.replace(/\s+/g, "").replace(/(.{4})(?=.)/g, "$1 ");
