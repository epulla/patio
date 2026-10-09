export default function Switch({
  checked,
  onChange,
  label,
  showLabel = true,
}: {
  checked: boolean;
  onChange: (checked: boolean) => void;
  label: string;
  showLabel?: boolean;
}) {
  return (
    <label className="switch">
      <input type="checkbox" role="switch" checked={checked} onChange={(event) => onChange(event.target.checked)} />
      <span className="switch-track" aria-hidden="true" />
      <span className={showLabel ? undefined : "sr-only"}>{label}</span>
    </label>
  );
}
