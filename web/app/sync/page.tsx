import { Suspense } from "react";
import { listVouchers, type Voucher, type VoucherStatus } from "./api";
import { AutoRefresh } from "./auto-refresh";
import { formatRupees } from "./format";
import { AlertIcon } from "./icons";
import styles from "./sync.module.css";
import { VoucherSection } from "./voucher-section";

const groups: VoucherStatus[] = ["failed", "pending", "pushed"];

export default function SyncPage() {
  return (
    <>
      <header className={styles.topbar}>
        <div className={styles.brand}>
          <span className={styles.logo}>S</span>
          <div>
            <div className={styles.brandName}>Sutra</div>
            <div className={styles.brandSub}>Job-work accounting</div>
          </div>
        </div>
        <span className={styles.live}>
          <span className={styles.liveDot} />
          Live · refreshes every 3s
        </span>
      </header>

      <main className={styles.main}>
        <div className={styles.pageHead}>
          <h1>Voucher sync</h1>
          <p>Every job-work receipt creates an accounting voucher. This is where each one stands with the accounting system.</p>
        </div>
        <AutoRefresh intervalMs={3000} />
        <Suspense fallback={<DashboardSkeleton />}>
          <Dashboard />
        </Suspense>
      </main>
    </>
  );
}

async function Dashboard() {
  const vouchers = await listVouchers();
  if (vouchers instanceof Error) {
    return (
      <div className={styles.alert} role="alert">
        <AlertIcon />
        <div>
          <strong>Can’t load vouchers</strong>
          <p>{vouchers.message}</p>
          <p>
            Check that the API is running (<code>go run ./cmd/api</code>) and that <code>SUTRA_API_URL</code> in{" "}
            <code>web/.env.local</code> points at it.
          </p>
        </div>
      </div>
    );
  }

  const byStatus = (s: VoucherStatus) => vouchers.filter((v) => v.status === s);
  return (
    <>
      <Stats vouchers={vouchers} />
      {groups.map((status) => (
        <VoucherSection key={status} status={status} vouchers={byStatus(status)} />
      ))}
    </>
  );
}

function Stats({ vouchers }: { vouchers: Voucher[] }) {
  const count = (s: VoucherStatus) => vouchers.filter((v) => v.status === s).length;
  const pushed = vouchers.filter((v) => v.status === "pushed");
  const pushedPaise = pushed.reduce((sum, v) => sum + v.amount_paise, 0);

  const cards = [
    { tone: styles.failed, label: "Failed", value: count("failed"), hint: "need a manual retry" },
    { tone: styles.pending, label: "Pending", value: count("pending"), hint: "queued or in flight" },
    { tone: styles.pushed, label: "Pushed", value: count("pushed"), hint: "confirmed by accounting" },
    { tone: styles.value, label: "Value pushed", value: formatRupees(pushedPaise), hint: `across ${pushed.length} vouchers` },
  ];
  return (
    <div className={styles.stats}>
      {cards.map((c) => (
        <div key={c.label} className={`${styles.stat} ${c.tone}`}>
          <div className={styles.statLabel}>{c.label}</div>
          <div className={styles.statValue}>{c.value}</div>
          <div className={styles.statHint}>{c.hint}</div>
        </div>
      ))}
    </div>
  );
}

function DashboardSkeleton() {
  return (
    <>
      <div className={styles.stats}>
        {[0, 1, 2, 3].map((i) => (
          <div key={i} className={`${styles.skeleton} ${styles.skeletonStat}`} />
        ))}
      </div>
      <div className={`${styles.skeleton} ${styles.skeletonTable}`} />
    </>
  );
}
