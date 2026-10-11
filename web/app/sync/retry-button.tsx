"use client";

import { useState, useTransition } from "react";
import { retryVoucher } from "./actions";
import { RefreshIcon } from "./icons";
import styles from "./sync.module.css";

export function RetryButton({ id }: { id: number }) {
  const [pending, startTransition] = useTransition();
  const [error, setError] = useState<string | null>(null);

  return (
    <div className={styles.retry}>
      <button
        className={styles.button}
        disabled={pending}
        onClick={() => startTransition(async () => setError(await retryVoucher(id)))}
      >
        <RefreshIcon className={pending ? styles.spin : undefined} />
        {pending ? "Retrying…" : "Retry"}
      </button>
      {error && (
        <span role="alert" className={styles.retryError}>
          {error}
        </span>
      )}
    </div>
  );
}
