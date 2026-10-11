// formatRupees uses integer arithmetic on paise, so no floating-point rounding is involved.
export function formatRupees(paise: number): string {
  const rupees = Math.trunc(paise / 100).toLocaleString("en-IN");
  return `₹${rupees}.${String(paise % 100).padStart(2, "0")}`;
}

const timeFormat = new Intl.DateTimeFormat("en-IN", {
  timeZone: "Asia/Kolkata",
  hour: "2-digit",
  minute: "2-digit",
  second: "2-digit",
  hour12: false,
});

// formatTime shows a timestamp as IST wall-clock time, e.g. 20:43:51.
export function formatTime(iso: string): string {
  return timeFormat.format(new Date(iso));
}

export function typeLabel(type: string): string {
  return type === "jobwork_receipt" ? "Job-work receipt" : type;
}
