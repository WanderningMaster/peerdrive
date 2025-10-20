export function bytesToHex(bytes: number[]) {
  if (!Array.isArray(bytes) || bytes.length === 0) return "";

  return bytes
    .map((b) =>
      Math.max(0, Math.min(255, Number(b)))
        .toString(16)
        .padStart(2, "0"),
    )
    .join("");
}
