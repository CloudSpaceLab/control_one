import { useState, useEffect } from 'react';
import { ExtendedTemplate, TemplateType, TemplateExecutionRequest, TemplateExecutionResult } from '../lib/extendedTemplateTypes';
import { validateTemplateParameters, getDefaultParameters, createExecutionRequest } from '../lib/extendedTemplateUtils';
import { useExtendedTemplates } from '../hooks/useExtendedTemplates';
import { useTenants } from '../hooks/useTenants';
import { useNodes } from '../hooks/useNodes';
import { useToast } from '../providers/ToastProvider';

interface TemplateWizardProps {
  isOpen: boolean;
  onClose: () => void;
  initialTemplateType?: TemplateType;
  preselectedTemplateId?: string;
}

interface WizardStep {
  id: string;
  title: string;
  description: string;
}

const WIZARD_STEPS: WizardStep[] = [
  {
    id: 'template-selection',
    title: 'Select Template',
    description: 'Choose a template to execute',
  },
  {
    id: 'target-selection',
    title: 'Select Target',
    description: 'Choose where to apply this template',
  },
  {
    id: 'parameter-configuration',
    title: 'Configure Parameters',
    description: 'Customize template parameters',
  },
  {
    id: 'review',
    title: 'Review & Execute',
    description: 'Review your choices and execute',
  },
];

export function TemplateWizard({ isOpen, onClose, initialTemplateType, preselectedTemplateId }: TemplateWizardProps): JSX.Element {
  const { executeTemplate, filter: filterTemplates } = useExtendedTemplates();
  const { data: tenants } = useTenants();
  const { data: nodes } = useNodes();
  const { showToast } = useToast();

  const [currentStep, setCurrentStep] = useState(0);
  const [selectedTemplate, setSelectedTemplate] = useState<ExtendedTemplate | null>(null);
  const [selectedTargetType, setSelectedTargetType] = useState<'tenant' | 'node' | 'global'>('tenant');
  const [selectedTargetId, setSelectedTargetId] = useState<string>('');
  const [parameters, setParameters] = useState<Record<string, unknown>>({});
  const [isExecuting, setIsExecuting] = useState(false);
  const [executionResult, setExecutionResult] = useState<TemplateExecutionResult | null>(null);
  const [validationErrors, setValidationErrors] = useState<string[]>([]);

  // Filter templates based on initial type
  const availableTemplates = filterTemplates({
    type: initialTemplateType || 'all',
    include_archived: false,
  });

  useEffect(() => {
    if (preselectedTemplateId) {
      const template = availableTemplates.find(t => t.id === preselectedTemplateId);
      if (template) {
        setSelectedTemplate(template);
        setParameters(getDefaultParameters(template));
        setCurrentStep(1); // Skip to target selection
      }
    }
  }, [preselectedTemplateId, availableTemplates]);

  const resetWizard = () => {
    setCurrentStep(0);
    setSelectedTemplate(null);
    setSelectedTargetType('tenant');
    setSelectedTargetId('');
    setParameters({});
    setExecutionResult(null);
    setValidationErrors([]);
  };

  const handleClose = () => {
    resetWizard();
    onClose();
  };

  const canProceed = (): boolean => {
    switch (currentStep) {
      case 0: // template selection
        return selectedTemplate !== null;
      case 1: // target selection
        if (selectedTargetType === 'global') return true;
        return selectedTargetId !== '';
      case 2: // parameter configuration
        return validationErrors.length === 0;
      case 3: // review
        return true;
      default:
        return false;
    }
  };

  const nextStep = () => {
    if (canProceed() && currentStep < WIZARD_STEPS.length - 1) {
      setCurrentStep(currentStep + 1);
    }
  };

  const prevStep = () => {
    if (currentStep > 0) {
      setCurrentStep(currentStep - 1);
    }
  };

  const handleTemplateSelect = (template: ExtendedTemplate) => {
    setSelectedTemplate(template);
    setParameters(getDefaultParameters(template));
    setValidationErrors([]);
  };

  const handleParameterChange = (key: string, value: unknown) => {
    const newParameters = { ...parameters, [key]: value };
    setParameters(newParameters);
    
    if (selectedTemplate) {
      const validation = validateTemplateParameters(selectedTemplate, newParameters);
      setValidationErrors(validation.errors);
    }
  };

  const handleExecute = async () => {
    if (!selectedTemplate) return;

    setIsExecuting(true);
    try {
      const request = createExecutionRequest(
        selectedTemplate,
        selectedTargetType,
        selectedTargetType === 'global' ? undefined : selectedTargetId,
        parameters
      );

      const result = await executeTemplate(request);
      setExecutionResult(result);
      setCurrentStep(WIZARD_STEPS.length - 1); // Move to final step
      showToast('Template executed successfully!', 'success');
    } catch (error) {
      showToast(`Failed to execute template: ${error instanceof Error ? error.message : 'Unknown error'}`, 'error');
    } finally {
      setIsExecuting(false);
    }
  };

  const getTargetOptions = () => {
    switch (selectedTargetType) {
      case 'tenant':
        return tenants || [];
      case 'node':
        return nodes || [];
      default:
        return [];
    }
  };

  if (!isOpen) return <></>;

  return (
    <div className="wizard-overlay">
      <div className="wizard-container">
        <div className="wizard-header">
          <h2>Template Execution Wizard</h2>
          <button className="ghost-button" onClick={handleClose}>✕</button>
        </div>

        <div className="wizard-progress">
          {WIZARD_STEPS.map((step, index) => (
            <div
              key={step.id}
              className={`progress-step ${index === currentStep ? 'active' : index < currentStep ? 'completed' : ''}`}
            >
              <div className="step-number">{index + 1}</div>
              <div className="step-info">
                <div className="step-title">{step.title}</div>
                <div className="step-description">{step.description}</div>
              </div>
            </div>
          ))}
        </div>

        <div className="wizard-content">
          {currentStep === 0 && (
            <TemplateSelectionStep
              templates={availableTemplates}
              selectedTemplate={selectedTemplate}
              onSelect={handleTemplateSelect}
            />
          )}

          {currentStep === 1 && (
            <TargetSelectionStep
              targetType={selectedTargetType}
              targetId={selectedTargetId}
              targetTypeOptions={['tenant', 'node', 'global']}
              targetOptions={getTargetOptions()}
              onTargetTypeChange={setSelectedTargetType}
              onTargetIdChange={setSelectedTargetId}
            />
          )}

          {currentStep === 2 && selectedTemplate && (
            <ParameterConfigurationStep
              template={selectedTemplate}
              parameters={parameters}
              validationErrors={validationErrors}
              onParameterChange={handleParameterChange}
            />
          )}

          {currentStep === 3 && (
            <ReviewStep
              template={selectedTemplate}
              targetType={selectedTargetType}
              targetId={selectedTargetId}
              parameters={parameters}
              executionResult={executionResult}
              isExecuting={isExecuting}
            />
          )}
        </div>

        <div className="wizard-actions">
          {currentStep > 0 && (
            <button className="ghost-button" onClick={prevStep} disabled={isExecuting}>
              Previous
            </button>
          )}
          
          {currentStep < WIZARD_STEPS.length - 1 ? (
            <button
              className="primary-button"
              onClick={nextStep}
              disabled={!canProceed()}
            >
              Next
            </button>
          ) : (
            <button
              className="primary-button"
              onClick={handleExecute}
              disabled={!canProceed() || isExecuting || executionResult !== null}
            >
              {isExecuting ? 'Executing...' : 'Execute Template'}
            </button>
          )}
          
          <button className="ghost-button" onClick={handleClose} disabled={isExecuting}>
            Cancel
          </button>
        </div>
      </div>
    </div>
  );
}

// Step Components
function TemplateSelectionStep({
  templates,
  selectedTemplate,
  onSelect,
}: {
  templates: ExtendedTemplate[];
  selectedTemplate: ExtendedTemplate | null;
  onSelect: (template: ExtendedTemplate) => void;
}): JSX.Element {
  return (
    <div className="step-content">
      <h3>Choose a Template</h3>
      <div className="template-grid">
        {templates.map(template => (
          <div
            key={template.id}
            className={`template-card ${selectedTemplate?.id === template.id ? 'selected' : ''}`}
            onClick={() => onSelect(template)}
          >
            <div className="template-header">
              <span className="template-icon">{template.type === 'job' ? '⚙️' : template.type === 'config' ? '⚡' : '🛡️'}</span>
              <h4>{template.name}</h4>
            </div>
            <p>{template.description}</p>
            <div className="template-meta">
              <span className="provider">{template.provider}</span>
              <span className="type">{template.type}</span>
            </div>
          </div>
        ))}
      </div>
    </div>
  );
}

function TargetSelectionStep({
  targetType,
  targetId,
  targetTypeOptions,
  targetOptions,
  onTargetTypeChange,
  onTargetIdChange,
}: {
  targetType: 'tenant' | 'node' | 'global';
  targetId: string;
  targetTypeOptions: ('tenant' | 'node' | 'global')[];
  targetOptions: any[];
  onTargetTypeChange: (type: 'tenant' | 'node' | 'global') => void;
  onTargetIdChange: (id: string) => void;
}): JSX.Element {
  return (
    <div className="step-content">
      <h3>Select Target</h3>
      <div className="form-field">
        <label>Target Type</label>
        <select value={targetType} onChange={(e) => onTargetTypeChange(e.target.value as any)}>
          {targetTypeOptions.map(type => (
            <option key={type} value={type}>
              {type.charAt(0).toUpperCase() + type.slice(1)}
            </option>
          ))}
        </select>
      </div>

      {targetType !== 'global' && (
        <div className="form-field">
          <label>Select {targetType}</label>
          <select value={targetId} onChange={(e) => onTargetIdChange(e.target.value)}>
            <option value="">Choose a {targetType}...</option>
            {targetOptions.map(option => (
              <option key={option.id} value={option.id}>
                {option.name || option.hostname}
              </option>
            ))}
          </select>
        </div>
      )}
    </div>
  );
}

function ParameterConfigurationStep({
  template,
  parameters,
  validationErrors,
  onParameterChange,
}: {
  template: ExtendedTemplate;
  parameters: Record<string, unknown>;
  validationErrors: string[];
  onParameterChange: (key: string, value: unknown) => void;
}): JSX.Element {
  return (
    <div className="step-content">
      <h3>Configure Parameters</h3>
      <p className="step-description">Customize the template parameters for your specific needs.</p>

      {template.type === 'config' && (template as any).validation_rules && (
        <div className="parameter-fields">
          {(template as any).validation_rules.map((rule: any) => (
            <div key={rule.field} className="form-field">
              <label htmlFor={rule.field}>
                {rule.field}
                {rule.required && <span className="required">*</span>}
              </label>
              <input
                id={rule.field}
                type={rule.type === 'number' ? 'number' : 'text'}
                value={(parameters[rule.field] as string) || ''}
                onChange={(e) => onParameterChange(rule.field, rule.type === 'number' ? Number(e.target.value) : e.target.value)}
                required={rule.required}
              />
              {rule.description && <small>{rule.description}</small>}
            </div>
          ))}
        </div>
      )}

      {template.type === 'job' && (
        <div className="parameter-fields">
          <div className="form-field">
            <label htmlFor="timeout_seconds">Timeout (seconds)</label>
            <input
              id="timeout_seconds"
              type="number"
              value={(parameters.timeout_seconds as string) || ''}
              onChange={(e) => onParameterChange('timeout_seconds', Number(e.target.value))}
              placeholder="300"
            />
          </div>
        </div>
      )}

      {validationErrors.length > 0 && (
        <div className="validation-errors">
          <h4>Validation Errors:</h4>
          <ul>
            {validationErrors.map((error, index) => (
              <li key={index} className="error">{error}</li>
            ))}
          </ul>
        </div>
      )}
    </div>
  );
}

function ReviewStep({
  template,
  targetType,
  targetId,
  parameters,
  executionResult,
  isExecuting,
}: {
  template: ExtendedTemplate | null;
  targetType: 'tenant' | 'node' | 'global';
  targetId: string;
  parameters: Record<string, unknown>;
  executionResult: TemplateExecutionResult | null;
  isExecuting: boolean;
}): JSX.Element {
  return (
    <div className="step-content">
      <h3>Review & Execute</h3>
      
      {!executionResult ? (
        <div className="review-section">
          <div className="review-item">
            <h4>Template</h4>
            <p>{template?.name} ({template?.type})</p>
          </div>
          
          <div className="review-item">
            <h4>Target</h4>
            <p>
              {targetType === 'global' ? 'Global' : `${targetType}: ${targetId}`}
            </p>
          </div>
          
          <div className="review-item">
            <h4>Parameters</h4>
            <pre>{JSON.stringify(parameters || {}, null, 2)}</pre>
          </div>
          
          <div className="execution-warning">
            <p>⚠️ This will execute the template immediately. Please review all settings above.</p>
          </div>
        </div>
      ) : (
        <div className="execution-result">
          <h4>Execution Result</h4>
          <div className={`status ${executionResult.status}`}>
            Status: {executionResult.status}
          </div>
          {executionResult.result && (
            <div className="result-details">
              <h5>Details:</h5>
              <pre>{JSON.stringify(executionResult.result || {}, null, 2)}</pre>
            </div>
          )}
        </div>
      )}
    </div>
  );
}
