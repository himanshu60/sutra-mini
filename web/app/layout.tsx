import type { Metadata } from "next";
import "./globals.css";

export const metadata: Metadata = {
  title: "Sutra — voucher sync",
  description: "Accounting vouchers and their push status",
};

export default function RootLayout({ children }: LayoutProps<"/">) {
  return (
    <html lang="en">
      <body>{children}</body>
    </html>
  );
}
