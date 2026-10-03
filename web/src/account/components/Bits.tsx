import type { ComponentChildren, JSX } from 'preact';
import type { Role } from '../../api';
import { Icon, type IconName } from '../../components/Icon';
import { useI18n } from '../../i18n';
import { navigate, plainClick } from '../../router';
import { avatarClass, initial } from '../colors';

/** A link to another of the website's pages, opened in this tab without loading anew. */
export function Link({ href, onClick, ...rest }: JSX.HTMLAttributes<HTMLAnchorElement> & { href: string }) {
  return (
    <a
      href={href}
      {...rest}
      onClick={(e) => {
        onClick?.(e);
        if (!plainClick(e)) return;
        e.preventDefault();
        navigate(href);
      }}
    />
  );
}

export function Avatar({ id, name, size, pending }: { id: string; name: string; size?: 'xs' | 'sm' | 'lg'; pending?: boolean }) {
  return (
    <span class={`av ${size ?? ''} ${pending ? 'pend' : avatarClass(id)}`} aria-hidden="true">
      {initial(name)}
    </span>
  );
}

/** A few people side by side, overlapping, and how many more there are. */
export function AvatarStack({ people, max = 4 }: { people: { id: string; name: string; pending?: boolean }[]; max?: number }) {
  const more = people.length - max;
  return (
    <span class="avs" aria-hidden="true">
      {people.slice(0, more > 0 ? max - 1 : max).map((p) => (
        <Avatar key={p.id} id={p.id} name={p.name} size="xs" pending={p.pending} />
      ))}
      {more > 0 && <span class="av xs more">+{more + 1}</span>}
    </span>
  );
}

/** An on/off switch, for a row that is the button; locked shows it can't change. */
export function Switch({ on, locked }: { on: boolean; locked?: boolean }) {
  return <span class={`sw${on ? ' on' : ''}${locked ? ' lock' : ''}`} aria-hidden="true" />;
}

export function RoleBadge({ role, crown }: { role: Role; crown?: boolean }) {
  const { t } = useI18n();
  return (
    <span class={`role ${role === 'admin' ? 'admin' : ''}`}>
      {crown && role === 'admin' && <Icon name="crown" />}
      {t(role === 'admin' ? 'role.admin' : 'role.member')}
    </span>
  );
}

interface RowProps {
  icon: IconName;
  title: string;
  sub?: string;
  /** A row that leads somewhere has an arrow; one that does something at once has none. */
  chevron?: boolean;
  tone?: 'accent' | 'danger';
  /** The page the row stands for is open next to the list (settings on a computer). */
  current?: boolean;
  href?: string;
  onClick?: () => void;
  right?: ComponentChildren;
}

/** A row of a settings list: an icon, a title with a line under it, and maybe an arrow. */
export function Row({ icon, title, sub, chevron = true, tone, current, href, onClick, right }: RowProps) {
  const inner = (
    <>
      <span class={`ri ${tone === 'accent' ? 'acc' : tone === 'danger' ? 'dang' : ''}`}>
        <Icon name={icon} />
      </span>
      <span class="rt">
        <b>{title}</b>
        {sub && <span>{sub}</span>}
      </span>
      {right}
      {chevron && <Icon name="chev" class="chev" />}
    </>
  );
  const cls = `row${tone === 'danger' ? ' dang' : ''}${current ? ' on' : ''}`;
  if (href) {
    return (
      <Link href={href} class={cls} aria-current={current ? 'page' : undefined}>
        {inner}
      </Link>
    );
  }
  return (
    <button type="button" class={cls} onClick={onClick}>
      {inner}
    </button>
  );
}

/** Says something for a few seconds, with maybe one thing to do about it, such as Undo. */
export function ToastView({ text, action, onDone }: { text: string; action?: { label: string; run: () => void }; onDone: () => void }) {
  return (
    <div class="toast" role="status">
      <span>{text}</span>
      {action && (
        <button
          type="button"
          class="tbtn"
          onClick={() => {
            action.run();
            onDone();
          }}
        >
          {action.label}
        </button>
      )}
    </div>
  );
}
