import type { Voucher, VoucherStatus } from "./api";
import { formatRupees, formatTime, typeLabel } from "./format";
import { CheckIcon } from "./icons";
import { RetryButton } from "./retry-button";
import styles from "./sync.module.css";

const copy: Record<VoucherStatus, { title: string; hint: string; empty: string; detail: string }> = {
  failed: {
    title: "Failed",
    hint: "Automatic retries stopped. Fix the cause, then retry.",
    empty: "No failed vouchers — all clear.",
    detail: "Reason",
  },
  pending: {
    title: "Pending",
    hint: "Queued, waiting out a backoff, or being pushed right now.",
    empty: "Nothing waiting to be pushed.",
    detail: "State",
  },
  pushed: {
    title: "Pushed",
    hint: "Confirmed by the accounting system.",
    empty: "No vouchers pushed yet.",
    detail: "Accounting ref",
  },
};

export function VoucherSection({ status, vouchers }: { status: VoucherStatus; vouchers: Voucher[] }) {
  const text = copy[status];
  return (
    <section className={styles.section}>
      <header className={styles.sectionHead}>
        <span className={`${styles.badge} ${styles[status]}`}>{text.title}</span>
        <span className={styles.count}>{vouchers.length}</span>
        <p className={styles.hint}>{text.hint}</p>
      </header>

      {vouchers.length === 0 ? (
        <div className={styles.empty}>
          <CheckIcon />
          {text.empty}
        </div>
      ) : (
        <div className={styles.tableCard}>
          <table className={styles.table}>
            <thead>
              <tr>
                <th>Voucher</th>
                <th>Receipt</th>
                <th className={styles.right}>Amount</th>
                <th className={styles.center}>Attempts</th>
                <th>{text.detail}</th>
                <th>Updated (IST)</th>
                {status === "failed" && <th aria-label="Actions" />}
              </tr>
            </thead>
            <tbody>
              {vouchers.map((v) => (
                <tr key={v.id}>
                  <td>
                    <div className={styles.primary}>#{v.id}</div>
                    <div className={styles.sub}>{typeLabel(v.type)}</div>
                  </td>
                  <td className={styles.muted}>#{v.source_id}</td>
                  <td className={`${styles.right} ${styles.amount}`}>{formatRupees(v.amount_paise)}</td>
                  <td className={styles.center}>
                    <span className={styles.attempts}>{v.attempts}</span>
                  </td>
                  <td>
                    <Detail v={v} />
                  </td>
                  <td className={styles.muted}>{formatTime(v.updated_at)}</td>
                  {status === "failed" && (
                    <td className={styles.right}>
                      <RetryButton id={v.id} />
                    </td>
                  )}
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </section>
  );
}

function Detail({ v }: { v: Voucher }) {
  if (v.status === "pushed") {
    return <code className={styles.ref}>{v.external_ref}</code>;
  }
  if (v.status === "failed") {
    return <span className={styles.error}>{v.last_error}</span>;
  }
  if (v.locked_by) {
    return (
      <span className={`${styles.chip} ${styles.chipActive}`}>
        <span className={styles.spinner} />
        Pushing · {v.locked_by}
      </span>
    );
  }
  if (v.last_error) {
    return (
      <>
        <span className={styles.chip}>Retry at {formatTime(v.next_attempt_at)}</span>
        <div className={styles.subError}>{v.last_error}</div>
      </>
    );
  }
  return <span className={styles.chip}>Queued</span>;
}
