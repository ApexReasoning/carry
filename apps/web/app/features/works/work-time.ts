const dateFormat = new Intl.DateTimeFormat("en", {
  dateStyle: "medium",
});
const timeFormat = new Intl.DateTimeFormat("en", {
  dateStyle: "medium",
  timeStyle: "short",
});

export function formatWorkDate(iso: string): string {
  return format(iso, dateFormat);
}

export function formatWorkTime(iso: string): string {
  return format(iso, timeFormat);
}

function format(iso: string, formatter: Intl.DateTimeFormat): string {
  const time = Date.parse(iso);
  if (Number.isNaN(time)) {
    return iso;
  }
  return formatter.format(new Date(time));
}
