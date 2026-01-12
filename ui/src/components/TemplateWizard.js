import { Fragment as _Fragment, jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
import { useState, useEffect } from 'react';
import { validateTemplateParameters, getDefaultParameters, createExecutionRequest } from '../lib/extendedTemplateUtils';
import { useExtendedTemplates } from '../hooks/useExtendedTemplates';
import { useTenants } from '../hooks/useTenants';
import { useNodes } from '../hooks/useNodes';
import { useToast } from '../providers/ToastProvider';
const WIZARD_STEPS = [
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
export function TemplateWizard({ isOpen, onClose, initialTemplateType, preselectedTemplateId }) {
    const { executeTemplate, filter: filterTemplates } = useExtendedTemplates();
    const { data: tenants } = useTenants();
    const { data: nodes } = useNodes();
    const { showToast } = useToast();
    const [currentStep, setCurrentStep] = useState(0);
    const [selectedTemplate, setSelectedTemplate] = useState(null);
    const [selectedTargetType, setSelectedTargetType] = useState('tenant');
    const [selectedTargetId, setSelectedTargetId] = useState('');
    const [parameters, setParameters] = useState({});
    const [isExecuting, setIsExecuting] = useState(false);
    const [executionResult, setExecutionResult] = useState(null);
    const [validationErrors, setValidationErrors] = useState([]);
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
    const canProceed = () => {
        switch (currentStep) {
            case 0: // template selection
                return selectedTemplate !== null;
            case 1: // target selection
                if (selectedTargetType === 'global')
                    return true;
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
    const handleTemplateSelect = (template) => {
        setSelectedTemplate(template);
        setParameters(getDefaultParameters(template));
        setValidationErrors([]);
    };
    const handleParameterChange = (key, value) => {
        const newParameters = { ...parameters, [key]: value };
        setParameters(newParameters);
        if (selectedTemplate) {
            const validation = validateTemplateParameters(selectedTemplate, newParameters);
            setValidationErrors(validation.errors);
        }
    };
    const handleExecute = async () => {
        if (!selectedTemplate)
            return;
        setIsExecuting(true);
        try {
            const request = createExecutionRequest(selectedTemplate, selectedTargetType, selectedTargetType === 'global' ? undefined : selectedTargetId, parameters);
            const result = await executeTemplate(request);
            setExecutionResult(result);
            setCurrentStep(WIZARD_STEPS.length - 1); // Move to final step
            showToast('Template executed successfully!', 'success');
        }
        catch (error) {
            showToast(`Failed to execute template: ${error instanceof Error ? error.message : 'Unknown error'}`, 'error');
        }
        finally {
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
    if (!isOpen)
        return _jsx(_Fragment, {});
    return (_jsx("div", { className: "wizard-overlay", children: _jsxs("div", { className: "wizard-container", children: [_jsxs("div", { className: "wizard-header", children: [_jsx("h2", { children: "Template Execution Wizard" }), _jsx("button", { className: "ghost-button", onClick: handleClose, children: "\u2715" })] }), _jsx("div", { className: "wizard-progress", children: WIZARD_STEPS.map((step, index) => (_jsxs("div", { className: `progress-step ${index === currentStep ? 'active' : index < currentStep ? 'completed' : ''}`, children: [_jsx("div", { className: "step-number", children: index + 1 }), _jsxs("div", { className: "step-info", children: [_jsx("div", { className: "step-title", children: step.title }), _jsx("div", { className: "step-description", children: step.description })] })] }, step.id))) }), _jsxs("div", { className: "wizard-content", children: [currentStep === 0 && (_jsx(TemplateSelectionStep, { templates: availableTemplates, selectedTemplate: selectedTemplate, onSelect: handleTemplateSelect })), currentStep === 1 && (_jsx(TargetSelectionStep, { targetType: selectedTargetType, targetId: selectedTargetId, targetTypeOptions: ['tenant', 'node', 'global'], targetOptions: getTargetOptions(), onTargetTypeChange: setSelectedTargetType, onTargetIdChange: setSelectedTargetId })), currentStep === 2 && selectedTemplate && (_jsx(ParameterConfigurationStep, { template: selectedTemplate, parameters: parameters, validationErrors: validationErrors, onParameterChange: handleParameterChange })), currentStep === 3 && (_jsx(ReviewStep, { template: selectedTemplate, targetType: selectedTargetType, targetId: selectedTargetId, parameters: parameters, executionResult: executionResult, isExecuting: isExecuting }))] }), _jsxs("div", { className: "wizard-actions", children: [currentStep > 0 && (_jsx("button", { className: "ghost-button", onClick: prevStep, disabled: isExecuting, children: "Previous" })), currentStep < WIZARD_STEPS.length - 1 ? (_jsx("button", { className: "primary-button", onClick: nextStep, disabled: !canProceed(), children: "Next" })) : (_jsx("button", { className: "primary-button", onClick: handleExecute, disabled: !canProceed() || isExecuting || executionResult !== null, children: isExecuting ? 'Executing...' : 'Execute Template' })), _jsx("button", { className: "ghost-button", onClick: handleClose, disabled: isExecuting, children: "Cancel" })] })] }) }));
}
// Step Components
function TemplateSelectionStep({ templates, selectedTemplate, onSelect, }) {
    return (_jsxs("div", { className: "step-content", children: [_jsx("h3", { children: "Choose a Template" }), _jsx("div", { className: "template-grid", children: templates.map(template => (_jsxs("div", { className: `template-card ${selectedTemplate?.id === template.id ? 'selected' : ''}`, onClick: () => onSelect(template), children: [_jsxs("div", { className: "template-header", children: [_jsx("span", { className: "template-icon", children: template.type === 'job' ? '⚙️' : template.type === 'config' ? '⚡' : '🛡️' }), _jsx("h4", { children: template.name })] }), _jsx("p", { children: template.description }), _jsxs("div", { className: "template-meta", children: [_jsx("span", { className: "provider", children: template.provider }), _jsx("span", { className: "type", children: template.type })] })] }, template.id))) })] }));
}
function TargetSelectionStep({ targetType, targetId, targetTypeOptions, targetOptions, onTargetTypeChange, onTargetIdChange, }) {
    return (_jsxs("div", { className: "step-content", children: [_jsx("h3", { children: "Select Target" }), _jsxs("div", { className: "form-field", children: [_jsx("label", { children: "Target Type" }), _jsx("select", { value: targetType, onChange: (e) => onTargetTypeChange(e.target.value), children: targetTypeOptions.map(type => (_jsx("option", { value: type, children: type.charAt(0).toUpperCase() + type.slice(1) }, type))) })] }), targetType !== 'global' && (_jsxs("div", { className: "form-field", children: [_jsxs("label", { children: ["Select ", targetType] }), _jsxs("select", { value: targetId, onChange: (e) => onTargetIdChange(e.target.value), children: [_jsxs("option", { value: "", children: ["Choose a ", targetType, "..."] }), targetOptions.map(option => (_jsx("option", { value: option.id, children: option.name || option.hostname }, option.id)))] })] }))] }));
}
function ParameterConfigurationStep({ template, parameters, validationErrors, onParameterChange, }) {
    return (_jsxs("div", { className: "step-content", children: [_jsx("h3", { children: "Configure Parameters" }), _jsx("p", { className: "step-description", children: "Customize the template parameters for your specific needs." }), template.type === 'config' && template.validation_rules && (_jsx("div", { className: "parameter-fields", children: template.validation_rules.map((rule) => (_jsxs("div", { className: "form-field", children: [_jsxs("label", { htmlFor: rule.field, children: [rule.field, rule.required && _jsx("span", { className: "required", children: "*" })] }), _jsx("input", { id: rule.field, type: rule.type === 'number' ? 'number' : 'text', value: parameters[rule.field] || '', onChange: (e) => onParameterChange(rule.field, rule.type === 'number' ? Number(e.target.value) : e.target.value), required: rule.required }), rule.description && _jsx("small", { children: rule.description })] }, rule.field))) })), template.type === 'job' && (_jsx("div", { className: "parameter-fields", children: _jsxs("div", { className: "form-field", children: [_jsx("label", { htmlFor: "timeout_seconds", children: "Timeout (seconds)" }), _jsx("input", { id: "timeout_seconds", type: "number", value: parameters.timeout_seconds || '', onChange: (e) => onParameterChange('timeout_seconds', Number(e.target.value)), placeholder: "300" })] }) })), validationErrors.length > 0 && (_jsxs("div", { className: "validation-errors", children: [_jsx("h4", { children: "Validation Errors:" }), _jsx("ul", { children: validationErrors.map((error, index) => (_jsx("li", { className: "error", children: error }, index))) })] }))] }));
}
function ReviewStep({ template, targetType, targetId, parameters, executionResult, isExecuting, }) {
    return (_jsxs("div", { className: "step-content", children: [_jsx("h3", { children: "Review & Execute" }), !executionResult ? (_jsxs("div", { className: "review-section", children: [_jsxs("div", { className: "review-item", children: [_jsx("h4", { children: "Template" }), _jsxs("p", { children: [template?.name, " (", template?.type, ")"] })] }), _jsxs("div", { className: "review-item", children: [_jsx("h4", { children: "Target" }), _jsx("p", { children: targetType === 'global' ? 'Global' : `${targetType}: ${targetId}` })] }), _jsxs("div", { className: "review-item", children: [_jsx("h4", { children: "Parameters" }), _jsx("pre", { children: JSON.stringify(parameters || {}, null, 2) })] }), _jsx("div", { className: "execution-warning", children: _jsx("p", { children: "\u26A0\uFE0F This will execute the template immediately. Please review all settings above." }) })] })) : (_jsxs("div", { className: "execution-result", children: [_jsx("h4", { children: "Execution Result" }), _jsxs("div", { className: `status ${executionResult.status}`, children: ["Status: ", executionResult.status] }), executionResult.result && (_jsxs("div", { className: "result-details", children: [_jsx("h5", { children: "Details:" }), _jsx("pre", { children: JSON.stringify(executionResult.result || {}, null, 2) })] }))] }))] }));
}
