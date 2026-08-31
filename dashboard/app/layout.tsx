import type { Metadata } from "next";
import "./styles.css";

export const metadata: Metadata = {
  title: "Emisell Payment Platform",
  description: "Operations view for the Emisell Payment App Kernel",
};

export default function RootLayout({ children }: Readonly<{ children: React.ReactNode }>) {
  return (
    <html lang="id">
      <body>{children}</body>
    </html>
  );
}
