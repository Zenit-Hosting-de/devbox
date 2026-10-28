/**
 * Shortens the home directory in a text to `~`, wherever a path of the user's home appears
 * (`not set in /home/dev/.npmrc` becomes `not set in ~/.npmrc`). Another user name that merely
 * starts like the home (`/home/developer`) stays as it is. Without a home the text is returned
 * unchanged.
 */
export function shortenHome(text: string, home: string | undefined | null): string {
  const base = home?.replace(/\/+$/, "") ?? "";
  if (base === "" || !text.includes(base)) {
    return text;
  }
  const escaped = base.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  return text.replace(new RegExp(`(?<![\\w./~-])${escaped}(?![\\w.-])`, "g"), "~");
}
