import { Suspense } from "react";
import { listVouchers, type Voucher, type VoucherStatus } from "./api";
import { AutoRefresh } from "./auto-refresh";
import { RetryButton } from "./retry-button";

const groups: VoucherStatus[] = ["pending", "failed", "pushed"];

export default function SyncPage() {
  return (
    <main>
      <h1>Voucher sync</h1>
      <p>Refreshes every 3 seconds.</p>
      <AutoRefresh intervalMs={3000} />
      <Suspense fallback={<p>Loading vouchers…</p>}>
        <VoucherGroups />
      </Suspense>
    </main>
  );
}

async function VoucherGroups() {
  const vouchers = await listVouchers();
  if (vouchers instanceof Error) {
    return <p role="alert">Could not load vouchers. {vouchers.message}</p>;
  }
  return groups.map((status) => (
    <VoucherTable key={status} status={status} vouchers={vouchers.filter((v) => v.status === status)} />
  ));
}

function VoucherTable({ status, vouchers }: { status: VoucherStatus; vouchers: Voucher[] }) {
  return (
    <section>
      <h2>
        {status} ({vouchers.length})
      </h2>
      {vouchers.length === 0 ? (
        <p>No {status} vouchers.</p>
      ) : (
        <table>
          <thead>
            <tr>
              <th>Voucher</th>
              <th>Type</th>
              <th>Receipt</th>
              <th>Amount</th>
              <th>Attempts</th>
              <th>{status === "pushed" ? "External ref" : "Status / last error"}</th>
              <th>Updated</th>
              {status === "failed" && <th />}
            </tr>
          </thead>
          <tbody>
            {vouchers.map((v) => (
              <tr key={v.id}>
                <td>{v.id}</td>
                <td>{v.type}</td>
                <td>{v.source_id}</td>
                <td>{formatRupees(v.amount_paise)}</td>
                <td>{v.attempts}</td>
                <td>{detail(v)}</td>
                <td>{new Date(v.updated_at).toLocaleTimeString("en-IN")}</td>
                {status === "failed" && (
                  <td>
                    <RetryButton id={v.id} />
                  </td>
                )}
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </section>
  );
}

function detail(v: Voucher): string {
  if (v.status === "pushed") return v.external_ref ?? "";
  if (v.status === "failed") return v.last_error ?? "";
  if (v.locked_by) return `pushing (worker ${v.locked_by})`;
  if (v.last_error) {
    return `retry at ${new Date(v.next_attempt_at).toLocaleTimeString("en-IN")}; last error: ${v.last_error}`;
  }
  return "queued";
}

// formatRupees uses integer arithmetic on paise, so no floating-point rounding is involved.
function formatRupees(paise: number): string {
  const rupees = Math.trunc(paise / 100).toLocaleString("en-IN");
  return `₹${rupees}.${String(paise % 100).padStart(2, "0")}`;
}
