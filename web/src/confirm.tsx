import { useEffect, useId, useRef } from "react";
import { createRoot } from "react-dom/client";

// window.confirm is a silent no-op (returns false) in the macOS Wails build.
export function confirmDialog(message: string, confirmLabel = "Delete", danger = true): Promise<boolean> {
  const host = document.body.appendChild(document.createElement("div"));
  const root = createRoot(host);
  return new Promise((resolve) => {
    root.render(
      <ConfirmDialog
        message={message}
        confirmLabel={confirmLabel}
        danger={danger}
        onDone={(confirmed) => {
          resolve(confirmed);
          setTimeout(() => {
            root.unmount();
            host.remove();
          });
        }}
      />,
    );
  });
}

function ConfirmDialog({
  message,
  confirmLabel,
  danger,
  onDone,
}: {
  message: string;
  confirmLabel: string;
  danger: boolean;
  onDone: (confirmed: boolean) => void;
}) {
  const dialog = useRef<HTMLDialogElement>(null);
  const id = useId();

  useEffect(() => {
    if (!dialog.current?.open) dialog.current?.showModal();
  }, []);

  return (
    <dialog
      ref={dialog}
      className="create-dialog confirm-dialog"
      aria-labelledby={id}
      onClose={(event) => onDone(event.currentTarget.returnValue === "confirm")}
    >
      <form method="dialog" className="panel create-modal confirm-modal">
        <p id={id}>{message}</p>
        <div className="modal-actions">
          <button value="cancel" autoFocus>
            Cancel
          </button>
          <button value="confirm" className={danger ? "danger" : "primary"}>
            {confirmLabel}
          </button>
        </div>
      </form>
    </dialog>
  );
}
