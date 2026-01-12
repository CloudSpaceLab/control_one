import { ReactNode } from 'react';

interface EnterpriseLayoutProps {
  children: ReactNode;
  variant?: 'dashboard' | 'management' | 'detail';
  className?: string;
}

/**
 * Enterprise-grade layout container with optimized component placement
 * for professional dashboards and management interfaces
 */
export function EnterpriseLayout({ 
  children, 
  variant = 'dashboard',
  className = '' 
}: EnterpriseLayoutProps) {
  return (
    <div className={`enterprise-layout enterprise-layout--${variant} ${className}`}>
      {children}
    </div>
  );
}

interface ExecutiveOverviewProps {
  children: ReactNode;
  title: string;
  subtitle?: string;
  className?: string;
}

/**
 * Executive overview section for bird's eye dashboard perspective
 * Optimized for maximum information density with professional hierarchy
 */
export function ExecutiveOverview({ 
  children, 
  title, 
  subtitle,
  className = '' 
}: ExecutiveOverviewProps) {
  return (
    <section className={`executive-overview ${className}`}>
      <div className="executive-overview__header">
        <div className="executive-overview__title-section">
          <h1 className="executive-overview__title">{title}</h1>
          {subtitle && (
            <p className="executive-overview__subtitle">{subtitle}</p>
          )}
        </div>
        <div className="executive-overview__actions">
          {/* Executive action buttons positioned top-right */}
        </div>
      </div>
      <div className="executive-overview__kpi-grid">
        {children}
      </div>
    </section>
  );
}

interface ManagementPanelProps {
  children: ReactNode;
  title: string;
  icon?: string;
  subtitle?: string;
  className?: string;
  position?: 'primary' | 'secondary' | 'tertiary';
}

/**
 * Professional management panel with consistent positioning
 * Optimized for forms, filters, and detail views
 */
export function ManagementPanel({ 
  children, 
  title, 
  icon,
  subtitle,
  className = '',
  position = 'primary'
}: ManagementPanelProps) {
  return (
    <div className={`management-panel management-panel--${position} ${className}`}>
      <div className="management-panel__header">
        <div className="management-panel__title-section">
          {icon && <span className="management-panel__icon">{icon}</span>}
          <div>
            <h2 className="management-panel__title">{title}</h2>
            {subtitle && (
              <p className="management-panel__subtitle">{subtitle}</p>
            )}
          </div>
        </div>
      </div>
      <div className="management-panel__content">
        {children}
      </div>
    </div>
  );
}

interface ActionZoneProps {
  children: ReactNode;
  variant?: 'primary' | 'secondary' | 'floating';
  alignment?: 'left' | 'center' | 'right';
  className?: string;
}

/**
 * Standardized action zone for consistent button and widget placement
 */
export function ActionZone({ 
  children, 
  variant = 'primary',
  alignment = 'right',
  className = '' 
}: ActionZoneProps) {
  return (
    <div className={`action-zone action-zone--${variant} action-zone--${alignment} ${className}`}>
      {children}
    </div>
  );
}

interface ContentGridProps {
  children: ReactNode;
  columns?: 1 | 2 | 3 | 4;
  gap?: 'sm' | 'md' | 'lg';
  className?: string;
}

/**
 * Professional content grid for optimal information organization
 */
export function ContentGrid({ 
  children, 
  columns = 2,
  gap = 'md',
  className = '' 
}: ContentGridProps) {
  return (
    <div className={`content-grid content-grid--${columns}-col content-grid--gap-${gap} ${className}`}>
      {children}
    </div>
  );
}
