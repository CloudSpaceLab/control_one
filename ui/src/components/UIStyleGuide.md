# Professional Enterprise UI Style Guide

## Overview

This style guide defines the professional enterprise dashboard standards for the Control One UI, ensuring optimal component placement, visual hierarchy, and user experience across all pages.

## Layout Architecture

### Enterprise Layout Variants

#### 1. Dashboard Layout (`variant="dashboard"`)
- **Purpose**: Executive overview with maximum bird's eye perspective
- **Structure**: Executive overview at top, main content grid below
- **Use Cases**: Main dashboard, high-level monitoring views
- **Key Features**: Prominent KPI cards, quick actions, activity feeds

#### 2. Management Layout (`variant="management"`)
- **Purpose**: Detailed operations and form interactions
- **Structure**: Two-column layout with main content and sticky sidebar
- **Use Cases**: Templates, tenants, nodes, jobs management pages
- **Key Features**: Forms in main area, filters/details in sidebar

#### 3. Detail Layout (`variant="detail"`)
- **Purpose**: Focused single-item views
- **Structure**: Centered, max-width container
- **Use Cases**: Detailed item views, focused workflows
- **Key Features**: Concentrated layout, minimal distractions

## Component Placement Standards

### Executive Overview Section

**Position**: Top of dashboard pages
**Purpose**: Bird's eye view with critical metrics

```tsx
<ExecutiveOverview 
  title="📊 Executive Overview"
  subtitle="Real-time system posture and performance metrics"
>
  {/* 4 KPI cards maximum */}
  <StatCard />
  <StatCard />
  <StatCard />
  <StatCard />
</ExecutiveOverview>
```

**Guidelines**:
- Maximum 4 KPI cards for optimal scanning
- Use consistent icon positioning (left of title)
- Maintain 2.5rem bottom margin
- Always include descriptive subtitles

### Management Panel Placement

#### Primary Panels (`position="primary"`)
- **Location**: Main content area
- **Purpose**: Core functionality, primary forms, main lists
- **Styling**: Enhanced borders, stronger visual hierarchy
- **Use**: Create forms, main content lists, primary actions

#### Secondary Panels (`position="secondary"`)
- **Location**: Sidebar or supporting content areas
- **Purpose**: Filters, secondary actions, supporting information
- **Styling**: Subtle borders, supporting visual weight
- **Use**: Filter panels, detail views, secondary actions

#### Tertiary Panels (`position="tertiary"`)
- **Location**: Footer areas, supplementary content
- **Purpose**: Pagination, metadata, optional controls
- **Styling**: Minimal borders, lowest visual hierarchy
- **Use**: Pagination controls, metadata displays

### Action Zone Standards

#### Primary Actions (`variant="primary"`)
- **Position**: Bottom-right of forms/panels
- **Alignment**: `alignment="right"`
- **Purpose**: Main form submission, primary actions
- **Styling**: Enhanced visual separation with top border

```tsx
<ActionZone alignment="right" variant="primary">
  <button className="primary-button">Submit</button>
  <button className="ghost-button">Cancel</button>
</ActionZone>
```

#### Secondary Actions (`variant="secondary"`)
- **Position**: Inline with content, top-right of cards
- **Alignment**: `alignment="right"` or `alignment="center"`
- **Purpose**: Quick actions, navigation buttons
- **Styling**: No border, integrated with content

#### Floating Actions (`variant="floating"`)
- **Position**: Fixed bottom-right of viewport
- **Purpose**: Global actions, always-accessible controls
- **Use**: Quick create buttons, global actions

### Content Grid Standards

#### Column Guidelines
- **1 Column**: Forms, detailed content, focused workflows
- **2 Columns**: Basic form layouts, simple content pairs
- **3 Columns**: Complex form fields, multi-field groups
- **4 Columns**: Compact data display, dashboard widgets

#### Gap Standards
- **Small (`gap="sm"`)**: 1rem - Compact layouts
- **Medium (`gap="md"`)**: 1.5rem - Standard layouts (default)
- **Large (`gap="lg"`)**: 2rem - Spacious layouts

## Button Placement Hierarchy

### 1. Primary Actions
- **Style**: `primary-button`
- **Position**: Rightmost in action zones
- **Use**: Form submission, main actions
- **Priority**: Always most prominent

### 2. Secondary Actions
- **Style**: `ghost-button`
- **Position**: Left of primary button
- **Use**: Cancel, close, navigate
- **Priority**: Secondary to primary

### 3. Destructive Actions
- **Style**: `danger-button`
- **Position**: Separate from primary/secondary
- **Use**: Delete, archive, remove
- **Priority**: Visually distinct, careful placement

## Form Layout Standards

### Field Organization
```tsx
<ContentGrid columns={2} gap="md">
  <div className="form-field">
    <label htmlFor="field1">Field 1</label>
    <input id="field1" type="text" />
  </div>
  <div className="form-field">
    <label htmlFor="field2">Field 2</label>
    <input id="field2" type="text" />
  </div>
</ContentGrid>
```

### Type-Specific Field Groups
- **Basic Info**: 2-column grid (name, description)
- **Configuration**: 3-column grid (type, provider, settings)
- **Advanced Options**: 1-column full width
- **Checkboxes**: Inline layout with `checkbox-inline` class

### Form Action Placement
- Always use `ActionZone` with `variant="primary"`
- Position at bottom of form with top border
- Right-align for standard LTR layouts
- Include loading states and validation feedback

## Visual Hierarchy Standards

### Typography Scale
- **Executive Titles**: 2.5-3rem, font-weight 700
- **Panel Titles**: 1.4rem, font-weight 700
- **Section Headers**: 1.1rem, font-weight 600
- **Body Text**: 0.95rem, font-weight 400
- **Small Text**: 0.75-0.8rem, font-weight 500

### Color Hierarchy
- **Primary Actions**: Blue gradient (`#6366f1` to `#38bdf8`)
- **Secondary Elements**: Muted tones (`#94a3b8`)
- **Destructive Actions**: Red gradient (`#f87171` to `#ef4444`)
- **Success States**: Green tones (`#22c55e`, `#86efac`)
- **Background Layers**: Progressive transparency (`rgba(15, 23, 42, 0.95)`)

### Spacing Standards
- **Section Gaps**: 2-2.5rem
- **Panel Padding**: 1.75rem
- **Content Gaps**: 1.5rem
- **Field Spacing**: 1.25rem
- **Component Margins**: 0.75rem

## Responsive Behavior

### Desktop (>1200px)
- Full multi-column layouts
- Sticky sidebars
- Hover states and micro-interactions

### Tablet (768px-1200px)
- Single-column stacks
- Non-sticky sidebars
- Maintained action zone positioning

### Mobile (<768px)
- Full-width panels
- Stacked action zones
- Simplified navigation
- Touch-optimized spacing

## Implementation Checklist

### For New Pages
- [ ] Choose appropriate layout variant
- [ ] Implement executive overview if dashboard
- [ ] Use ManagementPanel with correct positions
- [ ] Apply ActionZone standards
- [ ] Follow content grid guidelines
- [ ] Implement responsive breakpoints
- [ ] Add loading states and error handling

### For Forms
- [ ] Use ContentGrid for field layout
- [ ] Apply proper label/input associations
- [ ] Include validation feedback
- [ ] Position actions with ActionZone
- [ ] Add loading states
- [ ] Handle keyboard navigation

### For Lists/Tables
- [ ] Use appropriate panel position
- [ ] Include filtering in sidebar
- [ ] Add pagination controls
- [ ] Implement empty states
- [ ] Add hover states and selection

## Accessibility Standards

### Keyboard Navigation
- Tab order follows visual hierarchy
- Focus indicators on all interactive elements
- Skip links for main content areas
- Proper ARIA labels and roles

### Screen Reader Support
- Semantic HTML structure
- Descriptive titles and subtitles
- Form field associations
- Action zone descriptions

### Visual Accessibility
- High contrast ratios (4.5:1 minimum)
- Focus indicators visible
- Color not sole information carrier
- Consistent sizing and spacing

## Performance Considerations

### CSS Optimization
- Use CSS Grid for layouts
- Minimize reflows with proper containment
- Optimize animations with transform/opacity
- Use backdrop-filter sparingly

### Component Architecture
- Lazy load non-critical panels
- Implement virtual scrolling for large lists
- Debounce filter inputs
- Optimize re-renders with proper memoization

This style guide ensures consistent, professional, and accessible enterprise dashboard experiences across the Control One application.
