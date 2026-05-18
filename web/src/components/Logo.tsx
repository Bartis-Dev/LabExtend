// LabExtend mark: a terminal-window frame with a prompt-and-cursor inside.
// `currentColor` drives the frame so the logo inherits text color in the
// navbar; the prompt chevron + cursor stay accent for the brand punch.
export function Logo({ className }: { className?: string }) {
  return (
    <svg
      viewBox="0 0 24 24"
      fill="none"
      xmlns="http://www.w3.org/2000/svg"
      className={className}
      aria-hidden
    >
      <rect
        x="2"
        y="3.5"
        width="20"
        height="17"
        rx="3"
        stroke="currentColor"
        strokeWidth="1.5"
        opacity="0.85"
      />
      <line
        x1="2"
        y1="7.5"
        x2="22"
        y2="7.5"
        stroke="currentColor"
        strokeWidth="1.5"
        opacity="0.85"
      />
      <circle cx="5" cy="5.5" r="0.6" fill="currentColor" opacity="0.5" />
      <circle cx="7" cy="5.5" r="0.6" fill="currentColor" opacity="0.5" />
      <circle cx="9" cy="5.5" r="0.6" fill="currentColor" opacity="0.5" />
      <path
        d="M6 11.5L9.5 14L6 16.5"
        stroke="var(--accent)"
        strokeWidth="1.8"
        strokeLinecap="round"
        strokeLinejoin="round"
      />
      <line
        x1="11.5"
        y1="17"
        x2="17.5"
        y2="17"
        stroke="var(--accent)"
        strokeWidth="1.8"
        strokeLinecap="round"
      />
    </svg>
  );
}
