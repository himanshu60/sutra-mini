"use server";

import { refresh } from "next/cache";
import { apiURL, unreachable } from "./api";

// retryVoucher asks the Go API to send a failed voucher back to the worker.
// It returns an error message to show, or null when the retry was accepted.
export async function retryVoucher(id: number): Promise<string | null> {
  if (!Number.isSafeInteger(id) || id <= 0) {
    return "invalid voucher id";
  }
  let res: Response;
  try {
    res = await fetch(`${apiURL()}/vouchers/${id}/retry`, { method: "POST" });
  } catch (err) {
    return unreachable(err);
  }
  if (!res.ok) {
    const body: { error?: string } | null = await res.json().catch(() => null);
    return body?.error ?? `retry failed with HTTP ${res.status}`;
  }
  refresh();
  return null;
}
