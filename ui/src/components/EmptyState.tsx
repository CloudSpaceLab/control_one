import './EmptyState.css';

interface EmptyStateProps {
  icon?: React.ReactNode;
  title: string;
  description?: string;
  primaryAction?: { label: string; onClick: () => void };
  secondaryAction?: { label: string; onClick: () => void };
}

// EmptyState is the standard "nothing to show yet" component. Use it
// everywhere a list, table, or pane has no rows — never raw text. Real empty
// states explain *what* the page does, *why* it's empty, and *what to do next*.
export function EmptyState({ icon, title, description, primaryAction, secondaryAction }: EmptyStateProps): JSX.Element {
  return (
    <div className="co-empty">
      {icon ? <div className="co-empty__icon" aria-hidden="true">{icon}</div> : null}
      <h3 className="co-empty__title">{title}</h3>
      {description ? <p className="co-empty__desc">{description}</p> : null}
      {primaryAction || secondaryAction ? (
        <div className="co-empty__actions">
          {primaryAction ? (
            <button type="button" className="primary-button" onClick={primaryAction.onClick}>
              {primaryAction.label}
            </button>
          ) : null}
          {secondaryAction ? (
            <button type="button" className="secondary-button" onClick={secondaryAction.onClick}>
              {secondaryAction.label}
            </button>
          ) : null}
        </div>
      ) : null}
    </div>
  );
}
