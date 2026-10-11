"use client";

import { useEffect } from "react";
import { useRouter } from "next/navigation";

// AutoRefresh re-renders the page's server components on an interval,
// so statuses change on screen as the worker pushes vouchers.
export function AutoRefresh({ intervalMs }: { intervalMs: number }) {
  const router = useRouter();
  useEffect(() => {
    const timer = setInterval(() => router.refresh(), intervalMs);
    return () => clearInterval(timer);
  }, [router, intervalMs]);
  return null;
}
