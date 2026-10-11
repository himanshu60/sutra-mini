// Server-side access to the Sutra Go API. SUTRA_API_URL has no NEXT_PUBLIC_ prefix,
// so it is never sent to the browser.

export type VoucherStatus = "pending" | "pushed" | "failed";

export type Voucher = {
  id: number;
  type: string;
  source_id: number;
  amount_paise: number;
  status: VoucherStatus;
  attempts: number;
  last_error: string | null;
  client_ref: string;
  external_ref: string | null;
  next_attempt_at: string;
  locked_by: string | null;
  locked_until: string | null;
  created_at: string;
  updated_at: string;
};

export function apiURL(): string {
  return process.env.SUTRA_API_URL ?? "http://localhost:8080";
}

// unreachable describes a failed fetch by its underlying cause, such as ECONNREFUSED.
export function unreachable(err: unknown): string {
  const cause = err instanceof Error && err.cause ? err.cause : err;
  const code = (cause as { code?: string }).code;
  return `Sutra API unreachable at ${apiURL()}: ${code ?? String(cause)}`;
}

// listVouchers returns the vouchers, or an Error describing why they could not be loaded.
export async function listVouchers(): Promise<Voucher[] | Error> {
  try {
    const res = await fetch(`${apiURL()}/vouchers`);
    if (!res.ok) {
      return new Error(`GET /vouchers returned ${res.status}: ${await res.text()}`);
    }
    return (await res.json()) as Voucher[];
  } catch (err) {
    return new Error(unreachable(err));
  }
}
