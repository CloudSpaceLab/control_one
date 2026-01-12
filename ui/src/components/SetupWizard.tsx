import { useState, useCallback } from 'react';
import { useNavigate } from 'react-router-dom';
import { useAuth } from '../providers/AuthProvider';
import './SetupWizard.css';

interface WizardStep {
  id: string;
  title: string;
  description: string;
  component: React.ComponentType<{
    onNext: () => void;
    onPrevious: () => void;
    onComplete: () => void;
    data: WizardData;
    updateData: (data: Partial<WizardData>) => void;
    isCreating?: boolean;
    createError?: string | null;
  }>;
}

interface WizardData {
  // Step 1: Organization Setup
  organizationName: string;
  adminEmail: string;
  adminName: string;
  
  // Step 2: First Tenant
  tenantName: string;
  tenantDescription: string;
  tenantEnvironment: 'development' | 'staging' | 'production';
  
  // Step 3: Node Registration
  nodeRegistrationMethod: 'bootstrap' | 'manual';
  bootstrapToken?: string;
  nodeCount: number;
  
  // Step 4: Basic Configuration
  enableCompliance: boolean;
  enableMonitoring: boolean;
  complianceFrameworks: string[];
  
  // Step 5: Review & Complete
  confirmed: boolean;
}

const INITIAL_DATA: WizardData = {
  organizationName: '',
  adminEmail: '',
  adminName: '',
  tenantName: '',
  tenantDescription: '',
  tenantEnvironment: 'development',
  nodeRegistrationMethod: 'bootstrap',
  nodeCount: 1,
  enableCompliance: true,
  enableMonitoring: true,
  complianceFrameworks: [],
  confirmed: false,
};

export function SetupWizard(): JSX.Element {
  const navigate = useNavigate();
  const { apiClient } = useAuth();
  const [currentStepIndex, setCurrentStepIndex] = useState(0);
  const [wizardData, setWizardData] = useState<WizardData>(INITIAL_DATA);
  const [isCreating, setIsCreating] = useState(false);
  const [createError, setCreateError] = useState<string | null>(null);

  const updateData = useCallback((data: Partial<WizardData>) => {
    setWizardData(prev => ({ ...prev, ...data }));
  }, []);

  const goToStep = useCallback((stepIndex: number) => {
    if (stepIndex >= 0 && stepIndex < steps.length) {
      setCurrentStepIndex(stepIndex);
    }
  }, []);

  const onNext = useCallback(() => {
    if (currentStepIndex < steps.length - 1) {
      goToStep(currentStepIndex + 1);
    }
  }, [currentStepIndex, goToStep]);

  const onPrevious = useCallback(() => {
    if (currentStepIndex > 0) {
      goToStep(currentStepIndex - 1);
    }
  }, [currentStepIndex, goToStep]);

  const onComplete = useCallback(async () => {
    setIsCreating(true);
    setCreateError(null);
    
    try {
      // Create the first tenant
      await apiClient.createTenant({
        name: wizardData.tenantName,
      });
      
      console.log('Setup completed successfully:', wizardData);
      
      // Navigate to dashboard after completion
      navigate('/', { replace: true });
    } catch (error) {
      console.error('Setup failed:', error);
      setCreateError(error instanceof Error ? error.message : 'Setup failed. Please try again.');
    } finally {
      setIsCreating(false);
    }
  }, [wizardData, navigate, apiClient]);

  const currentStep = steps[currentStepIndex];
  const StepComponent = currentStep.component;

  return (
    <div className="setup-wizard">
      <div className="wizard-header">
        <div className="wizard-progress">
          {steps.map((step, index) => (
            <div
              key={step.id}
              className={`progress-step ${index <= currentStepIndex ? 'active' : ''} ${index === currentStepIndex ? 'current' : ''}`}
            >
              <div className="step-indicator">
                {index < currentStepIndex ? '✓' : index + 1}
              </div>
              <div className="step-label">
                <div className="step-title">{step.title}</div>
                <div className="step-description">{step.description}</div>
              </div>
            </div>
          ))}
        </div>
      </div>

      <div className="wizard-content">
        <div className="wizard-step">
          <StepComponent
            onNext={onNext}
            onPrevious={onPrevious}
            onComplete={onComplete}
            data={wizardData}
            updateData={updateData}
            isCreating={isCreating}
            createError={createError}
          />
        </div>
      </div>
    </div>
  );
}

// Step Components
function QuickSetupStep({ onNext, data, updateData }: WizardStepProps): JSX.Element {
  const [isCreating, setIsCreating] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const handleQuickSetup = async () => {
    setIsCreating(true);
    setError(null);
    
    try {
      // Simulate quick setup - in real implementation this would call APIs
      await new Promise(resolve => setTimeout(resolve, 2000));
      
      // Update data with default values
      updateData({
        organizationName: data.organizationName || 'Default Organization',
        adminEmail: data.adminEmail || 'admin@company.com',
        adminName: data.adminName || 'System Administrator',
        tenantName: data.tenantName || 'Production Environment',
        tenantEnvironment: 'production' as const,
        nodeRegistrationMethod: 'bootstrap' as const,
        nodeCount: 1,
        enableCompliance: true,
        enableMonitoring: true,
        complianceFrameworks: ['CIS Benchmarks'],
        confirmed: true
      });
      
      onNext();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Quick setup failed');
    } finally {
      setIsCreating(false);
    }
  };

  return (
    <div className="wizard-step-content">
      <h2>Quick Setup</h2>
      <p>Get started with Control One in just a few clicks using our recommended configuration.</p>
      
      <div className="quick-setup-options">
        <div className="setup-option">
          <h3>🚀 One-Click Setup</h3>
          <p>Use our recommended configuration for immediate deployment</p>
          <ul>
            <li>Production-ready tenant</li>
            <li>Security compliance enabled</li>
            <li>Monitoring and telemetry</li>
            <li>Bootstrap node registration</li>
          </ul>
          <button 
            type="button" 
            className="primary-button" 
            onClick={handleQuickSetup}
            disabled={isCreating}
          >
            {isCreating ? 'Setting up...' : 'Start Quick Setup'}
          </button>
        </div>

        <div className="setup-option">
          <h3>⚙️ Custom Setup</h3>
          <p>Configure every aspect of your Control One deployment</p>
          <ul>
            <li>Custom organization details</li>
            <li>Multiple tenant environments</li>
            <li>Advanced compliance frameworks</li>
            <li>Custom node configurations</li>
          </ul>
          <button 
            type="button" 
            className="ghost-button" 
            onClick={onNext}
          >
            Configure Manually
          </button>
        </div>
      </div>

      {error && <div className="form-error">{error}</div>}
      
      <div className="wizard-actions">
        <button type="button" className="ghost-button" onClick={() => window.history.back()}>
          Back
        </button>
      </div>
    </div>
  );
}
  function OrganizationStep({ onNext, data, updateData }: WizardStepProps): JSX.Element {
  const [errors, setErrors] = useState<Record<string, string>>({});

  const validateAndNext = () => {
    const newErrors: Record<string, string> = {};
    
    if (!data.organizationName.trim()) {
      newErrors.organizationName = 'Organization name is required';
    }
    if (!data.adminEmail.trim()) {
      newErrors.adminEmail = 'Admin email is required';
    } else if (!/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(data.adminEmail)) {
      newErrors.adminEmail = 'Invalid email format';
    }
    if (!data.adminName.trim()) {
      newErrors.adminName = 'Admin name is required';
    }

    if (Object.keys(newErrors).length === 0) {
      onNext();
    } else {
      setErrors(newErrors);
    }
  };

  return (
    <div className="wizard-step-content">
      <h2>Organization Setup</h2>
      <p>Let's start by configuring your organization and administrator account.</p>
      
      <div className="form-grid">
        <div className="form-field">
          <label htmlFor="organizationName">Organization Name *</label>
          <input
            id="organizationName"
            type="text"
            value={data.organizationName}
            onChange={(e) => updateData({ organizationName: e.target.value })}
            placeholder="Acme Corporation"
            className={errors.organizationName ? 'error' : ''}
          />
          {errors.organizationName && <span className="error-message">{errors.organizationName}</span>}
        </div>

        <div className="form-field">
          <label htmlFor="adminName">Admin Name *</label>
          <input
            id="adminName"
            type="text"
            value={data.adminName}
            onChange={(e) => updateData({ adminName: e.target.value })}
            placeholder="John Doe"
            className={errors.adminName ? 'error' : ''}
          />
          {errors.adminName && <span className="error-message">{errors.adminName}</span>}
        </div>

        <div className="form-field">
          <label htmlFor="adminEmail">Admin Email *</label>
          <input
            id="adminEmail"
            type="email"
            value={data.adminEmail}
            onChange={(e) => updateData({ adminEmail: e.target.value })}
            placeholder="admin@acme.com"
            className={errors.adminEmail ? 'error' : ''}
          />
          {errors.adminEmail && <span className="error-message">{errors.adminEmail}</span>}
        </div>
      </div>

      <div className="wizard-actions">
        <button type="button" className="primary-button" onClick={validateAndNext}>
          Continue to Tenant Setup
        </button>
      </div>
    </div>
  );
}

function TenantStep({ onNext, onPrevious, data, updateData }: WizardStepProps): JSX.Element {
  const [errors, setErrors] = useState<Record<string, string>>({});

  const validateAndNext = () => {
    const newErrors: Record<string, string> = {};
    
    if (!data.tenantName.trim()) {
      newErrors.tenantName = 'Tenant name is required';
    }

    if (Object.keys(newErrors).length === 0) {
      onNext();
    } else {
      setErrors(newErrors);
    }
  };

  return (
    <div className="wizard-step-content">
      <h2>Create Your First Tenant</h2>
      <p>Tenants provide isolation boundaries for infrastructure, policies, and compliance.</p>
      
      <div className="form-grid">
        <div className="form-field">
          <label htmlFor="tenantName">Tenant Name *</label>
          <input
            id="tenantName"
            type="text"
            value={data.tenantName}
            onChange={(e) => updateData({ tenantName: e.target.value })}
            placeholder="Production Environment"
            className={errors.tenantName ? 'error' : ''}
          />
          {errors.tenantName && <span className="error-message">{errors.tenantName}</span>}
        </div>

        <div className="form-field">
          <label htmlFor="tenantDescription">Description</label>
          <textarea
            id="tenantDescription"
            value={data.tenantDescription}
            onChange={(e) => updateData({ tenantDescription: e.target.value })}
            placeholder="Main production environment for customer workloads"
            rows={3}
          />
        </div>

        <div className="form-field">
          <label htmlFor="tenantEnvironment">Environment Type</label>
          <select
            id="tenantEnvironment"
            value={data.tenantEnvironment}
            onChange={(e) => updateData({ tenantEnvironment: e.target.value as 'development' | 'staging' | 'production' })}
          >
            <option value="development">Development</option>
            <option value="staging">Staging</option>
            <option value="production">Production</option>
          </select>
        </div>
      </div>

      <div className="wizard-actions">
        <button type="button" className="ghost-button" onClick={onPrevious}>
          Previous
        </button>
        <button type="button" className="primary-button" onClick={validateAndNext}>
          Continue to Node Registration
        </button>
      </div>
    </div>
  );
}

function NodeRegistrationStep({ onNext, onPrevious, data, updateData }: WizardStepProps): JSX.Element {
  const [errors, setErrors] = useState<Record<string, string>>({});

  const validateAndNext = () => {
    const newErrors: Record<string, string> = {};
    
    if (data.nodeCount < 1) {
      newErrors.nodeCount = 'At least one node is required';
    }

    if (Object.keys(newErrors).length === 0) {
      onNext();
    } else {
      setErrors(newErrors);
    }
  };

  return (
    <div className="wizard-step-content">
      <h2>Register Nodes</h2>
      <p>Configure how nodes will be discovered and registered with your control plane.</p>
      
      <div className="form-grid">
        <div className="form-field">
          <label>Registration Method</label>
          <div className="radio-group">
            <label className="radio-option">
              <input
                type="radio"
                name="registrationMethod"
                value="bootstrap"
                checked={data.nodeRegistrationMethod === 'bootstrap'}
                onChange={() => updateData({ nodeRegistrationMethod: 'bootstrap' })}
              />
              <div className="radio-content">
                <strong>Bootstrap Token</strong>
                <p>Generate a secure token for automatic node registration</p>
              </div>
            </label>
            <label className="radio-option">
              <input
                type="radio"
                name="registrationMethod"
                value="manual"
                checked={data.nodeRegistrationMethod === 'manual'}
                onChange={() => updateData({ nodeRegistrationMethod: 'manual' })}
              />
              <div className="radio-content">
                <strong>Manual Registration</strong>
                <p>Manually register each node through the UI</p>
              </div>
            </label>
          </div>
        </div>

        {data.nodeRegistrationMethod === 'bootstrap' && (
          <div className="form-field">
            <label htmlFor="bootstrapToken">Bootstrap Token</label>
            <input
              id="bootstrapToken"
              type="text"
              value={data.bootstrapToken || ''}
              onChange={(e) => updateData({ bootstrapToken: e.target.value })}
              placeholder="Generated automatically if left empty"
            />
            <small>A secure token will be generated if you leave this empty</small>
          </div>
        )}

        <div className="form-field">
          <label htmlFor="nodeCount">Expected Node Count</label>
          <input
            id="nodeCount"
            type="number"
            min="1"
            value={data.nodeCount}
            onChange={(e) => updateData({ nodeCount: parseInt(e.target.value) || 1 })}
            className={errors.nodeCount ? 'error' : ''}
          />
          {errors.nodeCount && <span className="error-message">{errors.nodeCount}</span>}
          <small>Number of nodes you expect to register initially</small>
        </div>
      </div>

      <div className="wizard-actions">
        <button type="button" className="ghost-button" onClick={onPrevious}>
          Previous
        </button>
        <button type="button" className="primary-button" onClick={validateAndNext}>
          Continue to Configuration
        </button>
      </div>
    </div>
  );
}

function ConfigurationStep({ onNext, onPrevious, data, updateData }: WizardStepProps): JSX.Element {
  const frameworks = ['CIS Benchmarks', 'SOC2', 'HIPAA', 'PCI-DSS', 'GDPR'];
  
  const toggleFramework = (framework: string) => {
    const updated = data.complianceFrameworks.includes(framework)
      ? data.complianceFrameworks.filter(f => f !== framework)
      : [...data.complianceFrameworks, framework];
    updateData({ complianceFrameworks: updated });
  };

  return (
    <div className="wizard-step-content">
      <h2>Basic Configuration</h2>
      <p>Configure essential monitoring and compliance settings for your infrastructure.</p>
      
      <div className="form-grid">
        <div className="form-field">
          <label className="checkbox-label">
            <input
              type="checkbox"
              checked={data.enableMonitoring}
              onChange={(e) => updateData({ enableMonitoring: e.target.checked })}
            />
            <div className="checkbox-content">
              <strong>Enable Monitoring</strong>
              <p>Collect metrics, logs, and telemetry from all nodes</p>
            </div>
          </label>
        </div>

        <div className="form-field">
          <label className="checkbox-label">
            <input
              type="checkbox"
              checked={data.enableCompliance}
              onChange={(e) => updateData({ enableCompliance: e.target.checked })}
            />
            <div className="checkbox-content">
              <strong>Enable Compliance Scanning</strong>
              <p>Automated compliance checks and reporting</p>
            </div>
          </label>
        </div>

        {data.enableCompliance && (
          <div className="form-field">
            <label>Compliance Frameworks</label>
            <div className="checkbox-group">
              {frameworks.map(framework => (
                <label key={framework} className="checkbox-option">
                  <input
                    type="checkbox"
                    checked={data.complianceFrameworks.includes(framework)}
                    onChange={() => toggleFramework(framework)}
                  />
                  {framework}
                </label>
              ))}
            </div>
          </div>
        )}
      </div>

      <div className="wizard-actions">
        <button type="button" className="ghost-button" onClick={onPrevious}>
          Previous
        </button>
        <button type="button" className="primary-button" onClick={onNext}>
          Review & Complete Setup
        </button>
      </div>
    </div>
  );
}

function ReviewStep({ onComplete, onPrevious, data, isCreating, createError }: WizardStepProps): JSX.Element {
  const [confirmed, setConfirmed] = useState(false);

  const handleComplete = () => {
    if (confirmed) {
      onComplete();
    }
  };

  return (
    <div className="wizard-step-content">
      <h2>Review & Complete Setup</h2>
      <p>Review your configuration before completing the setup process.</p>
      
      {createError && (
        <div className="form-error" style={{ marginBottom: '1rem' }}>
          {createError}
        </div>
      )}
      
      <div className="review-section">
        <h3>Organization Details</h3>
        <dl>
          <dt>Organization Name</dt>
          <dd>{data.organizationName || 'Not specified'}</dd>
          <dt>Admin Name</dt>
          <dd>{data.adminName || 'Not specified'}</dd>
          <dt>Admin Email</dt>
          <dd>{data.adminEmail || 'Not specified'}</dd>
        </dl>
      </div>

      <div className="review-section">
        <h3>Tenant Configuration</h3>
        <dl>
          <dt>Tenant Name</dt>
          <dd>{data.tenantName || 'Not specified'}</dd>
          <dt>Description</dt>
          <dd>{data.tenantDescription || 'No description'}</dd>
          <dt>Environment</dt>
          <dd>{data.tenantEnvironment}</dd>
        </dl>
      </div>

      <div className="review-section">
        <h3>Node Registration</h3>
        <dl>
          <dt>Registration Method</dt>
          <dd>{data.nodeRegistrationMethod === 'bootstrap' ? 'Bootstrap Token' : 'Manual'}</dd>
          {data.nodeRegistrationMethod === 'bootstrap' && (
            <>
              <dt>Bootstrap Token</dt>
              <dd>{data.bootstrapToken || 'Will be generated automatically'}</dd>
            </>
          )}
          <dt>Expected Node Count</dt>
          <dd>{data.nodeCount}</dd>
        </dl>
      </div>

      <div className="review-section">
        <h3>Configuration</h3>
        <dl>
          <dt>Monitoring</dt>
          <dd>{data.enableMonitoring ? 'Enabled' : 'Disabled'}</dd>
          <dt>Compliance Scanning</dt>
          <dd>{data.enableCompliance ? 'Enabled' : 'Disabled'}</dd>
          {data.enableCompliance && data.complianceFrameworks.length > 0 && (
            <>
              <dt>Frameworks</dt>
              <dd>{data.complianceFrameworks.join(', ')}</dd>
            </>
          )}
        </dl>
      </div>

      <div className="form-field">
        <label className="checkbox-label">
          <input
            type="checkbox"
            checked={confirmed}
            onChange={(event) => setConfirmed(event.target.checked)}
            disabled={isCreating}
          />
          I confirm that the configuration above is correct and I want to complete the setup.
        </label>
      </div>

      <div className="wizard-actions">
        <button type="button" className="ghost-button" onClick={onPrevious} disabled={isCreating}>
          Previous
        </button>
        <button 
          type="button" 
          className="primary-button" 
          onClick={handleComplete}
          disabled={!confirmed || isCreating}
        >
          {isCreating ? 'Creating...' : 'Complete Setup'}
        </button>
      </div>
    </div>
  );
}

// Wizard steps definition
const steps: WizardStep[] = [
  {
    id: 'quick-setup',
    title: 'Quick Setup',
    description: 'Get started fast',
    component: QuickSetupStep,
  },
  {
    id: 'organization',
    title: 'Organization',
    description: 'Setup your organization',
    component: OrganizationStep,
  },
  {
    id: 'tenant',
    title: 'Tenant',
    description: 'Create first tenant',
    component: TenantStep,
  },
  {
    id: 'nodes',
    title: 'Nodes',
    description: 'Register nodes',
    component: NodeRegistrationStep,
  },
  {
    id: 'configuration',
    title: 'Configuration',
    description: 'Basic settings',
    component: ConfigurationStep,
  },
  {
    id: 'review',
    title: 'Review',
    description: 'Complete setup',
    component: ReviewStep,
  },
];

type WizardStepProps = {
  onNext: () => void;
  onPrevious: () => void;
  onComplete: () => void;
  data: WizardData;
  updateData: (data: Partial<WizardData>) => void;
  isCreating?: boolean;
  createError?: string | null;
};
