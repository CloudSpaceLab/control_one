import { useState, ReactNode } from 'react';

interface DemandFormProps {
  title: string;
  icon: string;
  summary?: string;
  children: ReactNode;
  defaultExpanded?: boolean;
}

export function DemandForm({ title, icon, summary, children, defaultExpanded = false }: DemandFormProps): JSX.Element {
  const [isExpanded, setIsExpanded] = useState(defaultExpanded);

  const toggleExpanded = () => {
    setIsExpanded(!isExpanded);
  };

  return (
    <div className={`demand-form ${isExpanded ? 'expanded' : 'collapsed'}`}>
      <div className="demand-form-header" onClick={toggleExpanded}>
        <div className="demand-form-title">
          <div className="demand-form-icon">{icon}</div>
          <div>
            <div>{title}</div>
            {summary && <div className="demand-form-summary">{summary}</div>}
          </div>
        </div>
        <div className="demand-form-toggle">
          <svg width="16" height="16" viewBox="0 0 16 16" fill="none">
            <path d="M4 6L8 10L12 6" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"/>
          </svg>
        </div>
      </div>
      <div className="demand-form-content">
        {children}
      </div>
    </div>
  );
}
