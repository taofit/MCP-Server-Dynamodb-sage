export function Logo({ className = "w-5 h-5" }: { className?: string }) {
  return (
    <svg
      viewBox="0 0 32 32"
      fill="none"
      xmlns="http://www.w3.org/2000/svg"
      className={className}
    >
      {/* Database cylinders */}
      <ellipse cx="16" cy="8" rx="10" ry="4" fill="#f59e0b" opacity="0.9" />
      <path
        d="M6 8v8c0 2.21 4.48 4 10 4s10-1.79 10-4V8"
        stroke="#f59e0b"
        strokeWidth="1.5"
        fill="none"
      />
      <path
        d="M6 14c0 2.21 4.48 4 10 4s10-1.79 10-4"
        stroke="#f59e0b"
        strokeWidth="1.5"
        fill="none"
        opacity="0.6"
      />
      <ellipse
        cx="16"
        cy="24"
        rx="10"
        ry="4"
        fill="#f59e0b"
        opacity="0.3"
      />
      {/* Sparkle / sage dot */}
      <circle cx="22" cy="6" r="1.5" fill="#fbbf24" />
      <path
        d="M22 3v2M22 7v2M25 6h-2M20 6h-2"
        stroke="#fbbf24"
        strokeWidth="0.8"
        strokeLinecap="round"
      />
    </svg>
  );
}
