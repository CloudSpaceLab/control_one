import { TemplateType } from './extendedTemplateTypes';
export function getTemplateIcon(template) {
    switch (template.type) {
        case TemplateType.JOB:
            return '⚙️';
        case TemplateType.CONFIG:
            return '⚡';
        case TemplateType.COMPLIANCE:
            return '🛡️';
        default:
            return '📋';
    }
}
export function getTemplateTypeLabel(type) {
    switch (type) {
        case TemplateType.JOB:
            return 'Job Template';
        case TemplateType.CONFIG:
            return 'Configuration';
        case TemplateType.COMPLIANCE:
            return 'Compliance';
        default:
            return 'Template';
    }
}
export function getTemplateTypeDescription(type) {
    switch (type) {
        case TemplateType.JOB:
            return 'Automated provisioning and operational tasks';
        case TemplateType.CONFIG:
            return 'Reusable configuration for tenants and nodes';
        case TemplateType.COMPLIANCE:
            return 'Security scanning and compliance enforcement';
        default:
            return 'Template resource';
    }
}
export function getTemplateStatus(template) {
    if (template.archived_at) {
        return 'archived';
    }
    return 'active';
}
export function getTemplateStatusColor(status) {
    switch (status) {
        case 'active':
            return 'green';
        case 'archived':
            return 'gray';
        case 'draft':
            return 'yellow';
        default:
            return 'blue';
    }
}
export function summarizeTemplates(templates) {
    const byType = templates.reduce((acc, template) => {
        acc[template.type] = (acc[template.type] || 0) + 1;
        return acc;
    }, {});
    const active = templates.filter(t => !t.archived_at).length;
    const archived = templates.filter(t => t.archived_at).length;
    const providers = [...new Set(templates.map(t => t.provider))].length;
    // Mock calculations for recently used and popular
    const recently_used = Math.floor(active * 0.3);
    const popular = Math.floor(active * 0.2);
    return {
        total: templates.length,
        by_type: {
            [TemplateType.JOB]: byType[TemplateType.JOB] || 0,
            [TemplateType.CONFIG]: byType[TemplateType.CONFIG] || 0,
            [TemplateType.COMPLIANCE]: byType[TemplateType.COMPLIANCE] || 0,
        },
        active,
        archived,
        providers,
        recently_used,
        popular,
    };
}
export function filterTemplates(templates, filter) {
    return templates.filter(template => {
        if (filter.type && filter.type !== 'all' && template.type !== filter.type) {
            return false;
        }
        if (filter.provider && template.provider !== filter.provider) {
            return false;
        }
        if (filter.name_prefix && !template.name.toLowerCase().startsWith(filter.name_prefix.toLowerCase())) {
            return false;
        }
        if (!filter.include_archived && template.archived_at) {
            return false;
        }
        if (filter.labels) {
            const templateLabels = Object.entries(template.labels || {});
            const hasAllLabels = Object.entries(filter.labels).every(([key, value]) => templateLabels.some(([tKey, tValue]) => tKey === key && tValue === value));
            if (!hasAllLabels) {
                return false;
            }
        }
        return true;
    });
}
export function validateTemplateParameters(template, parameters) {
    const errors = [];
    switch (template.type) {
        case TemplateType.CONFIG:
            const configTemplate = template;
            if (configTemplate.validation_rules) {
                for (const rule of configTemplate.validation_rules) {
                    const value = parameters[rule.field];
                    if (rule.required && (value === undefined || value === null)) {
                        errors.push(`${rule.field} is required`);
                        continue;
                    }
                    if (value !== undefined && value !== null) {
                        // Type validation
                        if (!validateType(value, rule.type)) {
                            errors.push(`${rule.field} must be of type ${rule.type}`);
                        }
                        // Length validation for strings
                        if (rule.type === 'string' && typeof value === 'string') {
                            if (rule.min_length && value.length < rule.min_length) {
                                errors.push(`${rule.field} must be at least ${rule.min_length} characters`);
                            }
                            if (rule.max_length && value.length > rule.max_length) {
                                errors.push(`${rule.field} must be no more than ${rule.max_length} characters`);
                            }
                        }
                        // Pattern validation
                        if (rule.pattern && typeof value === 'string') {
                            const regex = new RegExp(rule.pattern);
                            if (!regex.test(value)) {
                                errors.push(`${rule.field} does not match required pattern`);
                            }
                        }
                        // Allowed values validation
                        if (rule.allowed_values && !rule.allowed_values.includes(value)) {
                            errors.push(`${rule.field} must be one of: ${rule.allowed_values.join(', ')}`);
                        }
                    }
                }
            }
            break;
        case TemplateType.JOB:
            const jobTemplate = template;
            // Validate job-specific parameters
            if (jobTemplate.timeout_seconds && parameters.timeout_seconds) {
                const timeout = Number(parameters.timeout_seconds);
                if (isNaN(timeout) || timeout <= 0) {
                    errors.push('timeout_seconds must be a positive number');
                }
            }
            break;
    }
    return { valid: errors.length === 0, errors };
}
function validateType(value, expectedType) {
    switch (expectedType) {
        case 'string':
            return typeof value === 'string';
        case 'number':
            return typeof value === 'number' && !isNaN(value);
        case 'boolean':
            return typeof value === 'boolean';
        case 'object':
            return typeof value === 'object' && value !== null && !Array.isArray(value);
        case 'array':
            return Array.isArray(value);
        default:
            return true;
    }
}
export function createExecutionRequest(template, targetType, targetId, parameters) {
    return {
        template_id: template.id,
        template_type: template.type,
        target_type: targetType,
        target_id: targetId,
        parameters: parameters || {},
        dry_run: false,
    };
}
export function getDefaultParameters(template) {
    switch (template.type) {
        case TemplateType.CONFIG:
            const configTemplate = template;
            return configTemplate.default_values || {};
        case TemplateType.JOB:
            const jobTemplate = template;
            return jobTemplate.default_payload || {};
        case TemplateType.COMPLIANCE:
            return {};
        default:
            return {};
    }
}
export function getTemplateProviders(templates) {
    return [...new Set(templates.map(t => t.provider))].sort();
}
export function getTemplateLabels(templates) {
    const labelMap = {};
    templates.forEach(template => {
        Object.entries(template.labels || {}).forEach(([key, value]) => {
            if (!labelMap[key]) {
                labelMap[key] = [];
            }
            if (!labelMap[key].includes(value)) {
                labelMap[key].push(value);
            }
        });
    });
    // Sort the values
    Object.keys(labelMap).forEach(key => {
        labelMap[key].sort();
    });
    return labelMap;
}
export function isJobTemplate(template) {
    return template.type === TemplateType.JOB;
}
export function isConfigTemplate(template) {
    return template.type === TemplateType.CONFIG;
}
export function isComplianceTemplate(template) {
    return template.type === TemplateType.COMPLIANCE;
}
