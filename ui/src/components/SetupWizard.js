import { jsx as _jsx, jsxs as _jsxs, Fragment as _Fragment } from "react/jsx-runtime";
import { useState, useCallback } from 'react';
import { useNavigate } from 'react-router-dom';
import { useAuth } from '../providers/AuthProvider';
import './SetupWizard.css';
const INITIAL_DATA = {
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
export function SetupWizard() {
    const navigate = useNavigate();
    const { apiClient } = useAuth();
    const [currentStepIndex, setCurrentStepIndex] = useState(0);
    const [wizardData, setWizardData] = useState(INITIAL_DATA);
    const [isCreating, setIsCreating] = useState(false);
    const [createError, setCreateError] = useState(null);
    const updateData = useCallback((data) => {
        setWizardData(prev => ({ ...prev, ...data }));
    }, []);
    const goToStep = useCallback((stepIndex) => {
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
        }
        catch (error) {
            console.error('Setup failed:', error);
            setCreateError(error instanceof Error ? error.message : 'Setup failed. Please try again.');
        }
        finally {
            setIsCreating(false);
        }
    }, [wizardData, navigate, apiClient]);
    const currentStep = steps[currentStepIndex];
    const StepComponent = currentStep.component;
    return (_jsxs("div", { className: "setup-wizard", children: [_jsx("div", { className: "wizard-header", children: _jsx("div", { className: "wizard-progress", children: steps.map((step, index) => (_jsxs("div", { className: `progress-step ${index <= currentStepIndex ? 'active' : ''} ${index === currentStepIndex ? 'current' : ''}`, children: [_jsx("div", { className: "step-indicator", children: index < currentStepIndex ? '✓' : index + 1 }), _jsxs("div", { className: "step-label", children: [_jsx("div", { className: "step-title", children: step.title }), _jsx("div", { className: "step-description", children: step.description })] })] }, step.id))) }) }), _jsx("div", { className: "wizard-content", children: _jsx("div", { className: "wizard-step", children: _jsx(StepComponent, { onNext: onNext, onPrevious: onPrevious, onComplete: onComplete, data: wizardData, updateData: updateData, isCreating: isCreating, createError: createError }) }) })] }));
}
// Step Components
function QuickSetupStep({ onNext, data, updateData }) {
    const [isCreating, setIsCreating] = useState(false);
    const [error, setError] = useState(null);
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
                tenantEnvironment: 'production',
                nodeRegistrationMethod: 'bootstrap',
                nodeCount: 1,
                enableCompliance: true,
                enableMonitoring: true,
                complianceFrameworks: ['CIS Benchmarks'],
                confirmed: true
            });
            onNext();
        }
        catch (err) {
            setError(err instanceof Error ? err.message : 'Quick setup failed');
        }
        finally {
            setIsCreating(false);
        }
    };
    return (_jsxs("div", { className: "wizard-step-content", children: [_jsx("h2", { children: "Quick Setup" }), _jsx("p", { children: "Get started with Control One in just a few clicks using our recommended configuration." }), _jsxs("div", { className: "quick-setup-options", children: [_jsxs("div", { className: "setup-option", children: [_jsx("h3", { children: "\uD83D\uDE80 One-Click Setup" }), _jsx("p", { children: "Use our recommended configuration for immediate deployment" }), _jsxs("ul", { children: [_jsx("li", { children: "Production-ready tenant" }), _jsx("li", { children: "Security compliance enabled" }), _jsx("li", { children: "Monitoring and telemetry" }), _jsx("li", { children: "Bootstrap node registration" })] }), _jsx("button", { type: "button", className: "primary-button", onClick: handleQuickSetup, disabled: isCreating, children: isCreating ? 'Setting up...' : 'Start Quick Setup' })] }), _jsxs("div", { className: "setup-option", children: [_jsx("h3", { children: "\u2699\uFE0F Custom Setup" }), _jsx("p", { children: "Configure every aspect of your Control One deployment" }), _jsxs("ul", { children: [_jsx("li", { children: "Custom organization details" }), _jsx("li", { children: "Multiple tenant environments" }), _jsx("li", { children: "Advanced compliance frameworks" }), _jsx("li", { children: "Custom node configurations" })] }), _jsx("button", { type: "button", className: "ghost-button", onClick: onNext, children: "Configure Manually" })] })] }), error && _jsx("div", { className: "form-error", children: error }), _jsx("div", { className: "wizard-actions", children: _jsx("button", { type: "button", className: "ghost-button", onClick: () => window.history.back(), children: "Back" }) })] }));
}
function OrganizationStep({ onNext, data, updateData }) {
    const [errors, setErrors] = useState({});
    const validateAndNext = () => {
        const newErrors = {};
        if (!data.organizationName.trim()) {
            newErrors.organizationName = 'Organization name is required';
        }
        if (!data.adminEmail.trim()) {
            newErrors.adminEmail = 'Admin email is required';
        }
        else if (!/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(data.adminEmail)) {
            newErrors.adminEmail = 'Invalid email format';
        }
        if (!data.adminName.trim()) {
            newErrors.adminName = 'Admin name is required';
        }
        if (Object.keys(newErrors).length === 0) {
            onNext();
        }
        else {
            setErrors(newErrors);
        }
    };
    return (_jsxs("div", { className: "wizard-step-content", children: [_jsx("h2", { children: "Organization Setup" }), _jsx("p", { children: "Let's start by configuring your organization and administrator account." }), _jsxs("div", { className: "form-grid", children: [_jsxs("div", { className: "form-field", children: [_jsx("label", { htmlFor: "organizationName", children: "Organization Name *" }), _jsx("input", { id: "organizationName", type: "text", value: data.organizationName, onChange: (e) => updateData({ organizationName: e.target.value }), placeholder: "Acme Corporation", className: errors.organizationName ? 'error' : '' }), errors.organizationName && _jsx("span", { className: "error-message", children: errors.organizationName })] }), _jsxs("div", { className: "form-field", children: [_jsx("label", { htmlFor: "adminName", children: "Admin Name *" }), _jsx("input", { id: "adminName", type: "text", value: data.adminName, onChange: (e) => updateData({ adminName: e.target.value }), placeholder: "John Doe", className: errors.adminName ? 'error' : '' }), errors.adminName && _jsx("span", { className: "error-message", children: errors.adminName })] }), _jsxs("div", { className: "form-field", children: [_jsx("label", { htmlFor: "adminEmail", children: "Admin Email *" }), _jsx("input", { id: "adminEmail", type: "email", value: data.adminEmail, onChange: (e) => updateData({ adminEmail: e.target.value }), placeholder: "admin@acme.com", className: errors.adminEmail ? 'error' : '' }), errors.adminEmail && _jsx("span", { className: "error-message", children: errors.adminEmail })] })] }), _jsx("div", { className: "wizard-actions", children: _jsx("button", { type: "button", className: "primary-button", onClick: validateAndNext, children: "Continue to Tenant Setup" }) })] }));
}
function TenantStep({ onNext, onPrevious, data, updateData }) {
    const [errors, setErrors] = useState({});
    const validateAndNext = () => {
        const newErrors = {};
        if (!data.tenantName.trim()) {
            newErrors.tenantName = 'Tenant name is required';
        }
        if (Object.keys(newErrors).length === 0) {
            onNext();
        }
        else {
            setErrors(newErrors);
        }
    };
    return (_jsxs("div", { className: "wizard-step-content", children: [_jsx("h2", { children: "Create Your First Tenant" }), _jsx("p", { children: "Tenants provide isolation boundaries for infrastructure, policies, and compliance." }), _jsxs("div", { className: "form-grid", children: [_jsxs("div", { className: "form-field", children: [_jsx("label", { htmlFor: "tenantName", children: "Tenant Name *" }), _jsx("input", { id: "tenantName", type: "text", value: data.tenantName, onChange: (e) => updateData({ tenantName: e.target.value }), placeholder: "Production Environment", className: errors.tenantName ? 'error' : '' }), errors.tenantName && _jsx("span", { className: "error-message", children: errors.tenantName })] }), _jsxs("div", { className: "form-field", children: [_jsx("label", { htmlFor: "tenantDescription", children: "Description" }), _jsx("textarea", { id: "tenantDescription", value: data.tenantDescription, onChange: (e) => updateData({ tenantDescription: e.target.value }), placeholder: "Main production environment for customer workloads", rows: 3 })] }), _jsxs("div", { className: "form-field", children: [_jsx("label", { htmlFor: "tenantEnvironment", children: "Environment Type" }), _jsxs("select", { id: "tenantEnvironment", value: data.tenantEnvironment, onChange: (e) => updateData({ tenantEnvironment: e.target.value }), children: [_jsx("option", { value: "development", children: "Development" }), _jsx("option", { value: "staging", children: "Staging" }), _jsx("option", { value: "production", children: "Production" })] })] })] }), _jsxs("div", { className: "wizard-actions", children: [_jsx("button", { type: "button", className: "ghost-button", onClick: onPrevious, children: "Previous" }), _jsx("button", { type: "button", className: "primary-button", onClick: validateAndNext, children: "Continue to Node Registration" })] })] }));
}
function NodeRegistrationStep({ onNext, onPrevious, data, updateData }) {
    const [errors, setErrors] = useState({});
    const validateAndNext = () => {
        const newErrors = {};
        if (data.nodeCount < 1) {
            newErrors.nodeCount = 'At least one node is required';
        }
        if (Object.keys(newErrors).length === 0) {
            onNext();
        }
        else {
            setErrors(newErrors);
        }
    };
    return (_jsxs("div", { className: "wizard-step-content", children: [_jsx("h2", { children: "Register Nodes" }), _jsx("p", { children: "Configure how nodes will be discovered and registered with your control plane." }), _jsxs("div", { className: "form-grid", children: [_jsxs("div", { className: "form-field", children: [_jsx("label", { children: "Registration Method" }), _jsxs("div", { className: "radio-group", children: [_jsxs("label", { className: "radio-option", children: [_jsx("input", { type: "radio", name: "registrationMethod", value: "bootstrap", checked: data.nodeRegistrationMethod === 'bootstrap', onChange: () => updateData({ nodeRegistrationMethod: 'bootstrap' }) }), _jsxs("div", { className: "radio-content", children: [_jsx("strong", { children: "Bootstrap Token" }), _jsx("p", { children: "Generate a secure token for automatic node registration" })] })] }), _jsxs("label", { className: "radio-option", children: [_jsx("input", { type: "radio", name: "registrationMethod", value: "manual", checked: data.nodeRegistrationMethod === 'manual', onChange: () => updateData({ nodeRegistrationMethod: 'manual' }) }), _jsxs("div", { className: "radio-content", children: [_jsx("strong", { children: "Manual Registration" }), _jsx("p", { children: "Manually register each node through the UI" })] })] })] })] }), data.nodeRegistrationMethod === 'bootstrap' && (_jsxs("div", { className: "form-field", children: [_jsx("label", { htmlFor: "bootstrapToken", children: "Bootstrap Token" }), _jsx("input", { id: "bootstrapToken", type: "text", value: data.bootstrapToken || '', onChange: (e) => updateData({ bootstrapToken: e.target.value }), placeholder: "Generated automatically if left empty" }), _jsx("small", { children: "A secure token will be generated if you leave this empty" })] })), _jsxs("div", { className: "form-field", children: [_jsx("label", { htmlFor: "nodeCount", children: "Expected Node Count" }), _jsx("input", { id: "nodeCount", type: "number", min: "1", value: data.nodeCount, onChange: (e) => updateData({ nodeCount: parseInt(e.target.value) || 1 }), className: errors.nodeCount ? 'error' : '' }), errors.nodeCount && _jsx("span", { className: "error-message", children: errors.nodeCount }), _jsx("small", { children: "Number of nodes you expect to register initially" })] })] }), _jsxs("div", { className: "wizard-actions", children: [_jsx("button", { type: "button", className: "ghost-button", onClick: onPrevious, children: "Previous" }), _jsx("button", { type: "button", className: "primary-button", onClick: validateAndNext, children: "Continue to Configuration" })] })] }));
}
function ConfigurationStep({ onNext, onPrevious, data, updateData }) {
    const frameworks = ['CIS Benchmarks', 'SOC2', 'HIPAA', 'PCI-DSS', 'GDPR'];
    const toggleFramework = (framework) => {
        const updated = data.complianceFrameworks.includes(framework)
            ? data.complianceFrameworks.filter(f => f !== framework)
            : [...data.complianceFrameworks, framework];
        updateData({ complianceFrameworks: updated });
    };
    return (_jsxs("div", { className: "wizard-step-content", children: [_jsx("h2", { children: "Basic Configuration" }), _jsx("p", { children: "Configure essential monitoring and compliance settings for your infrastructure." }), _jsxs("div", { className: "form-grid", children: [_jsx("div", { className: "form-field", children: _jsxs("label", { className: "checkbox-label", children: [_jsx("input", { type: "checkbox", checked: data.enableMonitoring, onChange: (e) => updateData({ enableMonitoring: e.target.checked }) }), _jsxs("div", { className: "checkbox-content", children: [_jsx("strong", { children: "Enable Monitoring" }), _jsx("p", { children: "Collect metrics, logs, and telemetry from all nodes" })] })] }) }), _jsx("div", { className: "form-field", children: _jsxs("label", { className: "checkbox-label", children: [_jsx("input", { type: "checkbox", checked: data.enableCompliance, onChange: (e) => updateData({ enableCompliance: e.target.checked }) }), _jsxs("div", { className: "checkbox-content", children: [_jsx("strong", { children: "Enable Compliance Scanning" }), _jsx("p", { children: "Automated compliance checks and reporting" })] })] }) }), data.enableCompliance && (_jsxs("div", { className: "form-field", children: [_jsx("label", { children: "Compliance Frameworks" }), _jsx("div", { className: "checkbox-group", children: frameworks.map(framework => (_jsxs("label", { className: "checkbox-option", children: [_jsx("input", { type: "checkbox", checked: data.complianceFrameworks.includes(framework), onChange: () => toggleFramework(framework) }), framework] }, framework))) })] }))] }), _jsxs("div", { className: "wizard-actions", children: [_jsx("button", { type: "button", className: "ghost-button", onClick: onPrevious, children: "Previous" }), _jsx("button", { type: "button", className: "primary-button", onClick: onNext, children: "Review & Complete Setup" })] })] }));
}
function ReviewStep({ onComplete, onPrevious, data, isCreating, createError }) {
    const [confirmed, setConfirmed] = useState(false);
    const handleComplete = () => {
        if (confirmed) {
            onComplete();
        }
    };
    return (_jsxs("div", { className: "wizard-step-content", children: [_jsx("h2", { children: "Review & Complete Setup" }), _jsx("p", { children: "Review your configuration before completing the setup process." }), createError && (_jsx("div", { className: "form-error", style: { marginBottom: '1rem' }, children: createError })), _jsxs("div", { className: "review-section", children: [_jsx("h3", { children: "Organization Details" }), _jsxs("dl", { children: [_jsx("dt", { children: "Organization Name" }), _jsx("dd", { children: data.organizationName || 'Not specified' }), _jsx("dt", { children: "Admin Name" }), _jsx("dd", { children: data.adminName || 'Not specified' }), _jsx("dt", { children: "Admin Email" }), _jsx("dd", { children: data.adminEmail || 'Not specified' })] })] }), _jsxs("div", { className: "review-section", children: [_jsx("h3", { children: "Tenant Configuration" }), _jsxs("dl", { children: [_jsx("dt", { children: "Tenant Name" }), _jsx("dd", { children: data.tenantName || 'Not specified' }), _jsx("dt", { children: "Description" }), _jsx("dd", { children: data.tenantDescription || 'No description' }), _jsx("dt", { children: "Environment" }), _jsx("dd", { children: data.tenantEnvironment })] })] }), _jsxs("div", { className: "review-section", children: [_jsx("h3", { children: "Node Registration" }), _jsxs("dl", { children: [_jsx("dt", { children: "Registration Method" }), _jsx("dd", { children: data.nodeRegistrationMethod === 'bootstrap' ? 'Bootstrap Token' : 'Manual' }), data.nodeRegistrationMethod === 'bootstrap' && (_jsxs(_Fragment, { children: [_jsx("dt", { children: "Bootstrap Token" }), _jsx("dd", { children: data.bootstrapToken || 'Will be generated automatically' })] })), _jsx("dt", { children: "Expected Node Count" }), _jsx("dd", { children: data.nodeCount })] })] }), _jsxs("div", { className: "review-section", children: [_jsx("h3", { children: "Configuration" }), _jsxs("dl", { children: [_jsx("dt", { children: "Monitoring" }), _jsx("dd", { children: data.enableMonitoring ? 'Enabled' : 'Disabled' }), _jsx("dt", { children: "Compliance Scanning" }), _jsx("dd", { children: data.enableCompliance ? 'Enabled' : 'Disabled' }), data.enableCompliance && data.complianceFrameworks.length > 0 && (_jsxs(_Fragment, { children: [_jsx("dt", { children: "Frameworks" }), _jsx("dd", { children: data.complianceFrameworks.join(', ') })] }))] })] }), _jsx("div", { className: "form-field", children: _jsxs("label", { className: "checkbox-label", children: [_jsx("input", { type: "checkbox", checked: confirmed, onChange: (event) => setConfirmed(event.target.checked), disabled: isCreating }), "I confirm that the configuration above is correct and I want to complete the setup."] }) }), _jsxs("div", { className: "wizard-actions", children: [_jsx("button", { type: "button", className: "ghost-button", onClick: onPrevious, disabled: isCreating, children: "Previous" }), _jsx("button", { type: "button", className: "primary-button", onClick: handleComplete, disabled: !confirmed || isCreating, children: isCreating ? 'Creating...' : 'Complete Setup' })] })] }));
}
// Wizard steps definition
const steps = [
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
