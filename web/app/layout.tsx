import type { Metadata } from 'next';
import './globals.css';
import NavBar from '../components/NavBar';

export const metadata: Metadata = {
  title: 'AI Cloud Ops',
  description: 'AI-Native Alibaba Cloud Multi-Account Ops Console',
};

// Root layout: nav + content. Dark mode is controlled by .dark on <html>.
// M2-10: dark mode toggle lives in NavBar and persists in localStorage.
export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="en" suppressHydrationWarning>
      <head>
        <script
          dangerouslySetInnerHTML={{
            __html: `(function(){try{var t=localStorage.getItem('theme');if(t==='dark'||(!t&&window.matchMedia('(prefers-color-scheme: dark)').matches)){document.documentElement.classList.add('dark')}}catch(e){}})();`,
          }}
        />
      </head>
      <body>
        <NavBar />
        <main className="max-w-7xl mx-auto px-4 py-6">{children}</main>
      </body>
    </html>
  );
}