"use client";

import { useState, useTransition } from "react";
import { retryVoucher } from "./actions";

export function RetryButton({ id }: { id: number }) {
  const [pending, startTransition] = useTransition();
  const [error, setError] = useState<string | null>(null);

  return (
    <>
      <button
        disabled={pending}
        onClick={() => startTransition(async () => setError(await retryVoucher(id)))}
      >
        {pending ? "Retrying…" : "Retry"}
      </button>
      {error && <span role="alert"> {error}</span>}
    </>
  );
}
