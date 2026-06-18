export function displayMetadataText(value: string | null | undefined) {
  const raw = (value ?? "").trim();
  if (!raw) return "";
  return decodeMetadataEntities(
    raw
      .replace(/<\s*(br|\/p|\/div|\/li|\/h[1-6])\b[^>]*>/gi, " ")
      .replace(/<[^>]+>/g, " "),
  )
    .replace(/\s+/g, " ")
    .trim();
}

function decodeMetadataEntities(value: string) {
  return value.replace(/&(#x[0-9a-f]+|#\d+|[a-z]+);/gi, (match, entity: string) => {
    const decoded = decodeNumericEntity(entity) ?? namedMetadataEntities[entity.toLowerCase()];
    return decoded ?? match;
  });
}

function decodeNumericEntity(entity: string) {
  const value = entity.toLowerCase();
  const codePoint = value.startsWith("#x") ? Number.parseInt(value.slice(2), 16) : value.startsWith("#") ? Number.parseInt(value.slice(1), 10) : NaN;
  if (!Number.isFinite(codePoint) || codePoint <= 0) return null;
  try {
    return String.fromCodePoint(codePoint);
  } catch {
    return null;
  }
}

const namedMetadataEntities: Record<string, string> = {
  amp: "&",
  apos: "'",
  gt: ">",
  hellip: "\u2026",
  laquo: "\u00ab",
  ldquo: "\u201c",
  lsquo: "\u2018",
  lt: "<",
  mdash: "\u2014",
  nbsp: " ",
  ndash: "\u2013",
  quot: "\"",
  raquo: "\u00bb",
  rdquo: "\u201d",
  rsquo: "\u2019",
};
