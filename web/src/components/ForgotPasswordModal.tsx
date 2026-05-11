import { Modal } from './Modal';

export function ForgotPasswordModal({
  open,
  onClose,
}: {
  open: boolean;
  onClose: () => void;
}) {
  return (
    <Modal open={open} onClose={onClose} title="Reset your password">
      <div className="space-y-3 text-sm text-fg-muted">
        <p>There is no in-app password recovery (this is a single-user system).</p>
        <p>To reset:</p>
        <ol className="ml-4 list-decimal space-y-2">
          <li>
            Set the environment variable{' '}
            <code className="rounded bg-bg-elevated px-1.5 py-0.5 text-fg">
              LABEXTEND_PASSWORD_RESET=true
            </code>{' '}
            and restart LabExtend.
          </li>
          <li>
            On next start the existing user is removed and you will see the setup wizard
            again.
          </li>
          <li>
            After completing setup, remove the variable (or set{' '}
            <code className="rounded bg-bg-elevated px-1.5 py-0.5 text-fg">
              LABEXTEND_PASSWORD_RESET=false
            </code>
            ) and restart once more.
          </li>
        </ol>
      </div>
      <div className="mt-6 flex justify-end">
        <button
          onClick={onClose}
          className="rounded bg-accent px-4 py-2 text-white hover:bg-accent-hover"
        >
          Got it
        </button>
      </div>
    </Modal>
  );
}
