import type { ReactNode } from "react";
import Menu from "./Menu";
import { Icon, IconButton, type IconName } from "./icons";

export default function SplitButton({
  icon,
  label,
  onClick,
  menuLabel,
  children,
}: {
  icon: IconName;
  label: string;
  onClick: () => void;
  menuLabel: string;
  children: (close: (focusTrigger?: boolean) => void) => ReactNode;
}) {
  return (
    <div className="split-button" role="group" aria-label={label}>
      <IconButton className="ghost" icon={icon} label={label} onClick={onClick} />
      <Menu label={menuLabel} buttonClassName="icon ghost split-toggle" button={<Icon name="chevronDown" />}>
        {children}
      </Menu>
    </div>
  );
}
