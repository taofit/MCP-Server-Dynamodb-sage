import type { Metadata } from "next";
import { Geist, Geist_Mono } from "next/font/google";
import "./globals.css";
import { ThemeProvider } from "@/components/layout/ThemeProvider";
import { Sidebar } from "@/components/layout/Sidebar";
import { Header } from "@/components/layout/Header";
import { SSEProvider } from "@/components/SSEProvider";
import { Toaster } from "sonner";

const geistSans = Geist({
  variable: "--font-geist-sans",
  subsets: ["latin"],
});

const geistMono = Geist_Mono({
  variable: "--font-geist-mono",
  subsets: ["latin"],
});

export const metadata: Metadata = {
  title: "DynamoDB Sage",
  description: "Natural Language Interface for Amazon DynamoDB",
  icons: { icon: "/icon.svg" },
};

export default function RootLayout({
  children,
}: Readonly<{
  children: React.ReactNode;
}>) {
  return (
    <html
      lang="en"
      className={`${geistSans.variable} ${geistMono.variable} h-full`}
      suppressHydrationWarning
    >
      <body className="h-full antialiased">
        <ThemeProvider>
          <SSEProvider>
            <div className="flex h-full">
              <Sidebar />
              <div className="flex flex-col flex-1 min-w-0">
                <Header />
                <main className="flex-1 flex flex-col overflow-auto min-h-0">{children}</main>
              </div>
            </div>
            <Toaster richColors position="bottom-right" />
          </SSEProvider>
        </ThemeProvider>
      </body>
    </html>
  );
}
