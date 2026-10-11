export default function Switch({
  checked,
  onChange,
  label,
  showLabel = true,
  disabled,
  title,
}: {
  checked: boolean;
  onChange: (checked: boolean) => void;
  label: string;
  showLabel?: boolean;
  disabled?: boolean;
  title?: string;
}) {
  return (
    <label className="switch" title={title}>
      <input
        type="checkbox"
        role="switch"
        checked={checked}
        disabled={disabled}
        onChange={(event) => onChange(event.target.checked)}
      />
      <span className="switch-track" aria-hidden="true" />
      <span className={showLabel ? undefined : "sr-only"}>{label}</span>
    </label>
  );
}
