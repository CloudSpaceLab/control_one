import type { Template } from './api';

export const DEFAULT_TEMPLATE_BODY = `{
  "steps": [
    {
      "name": "noop",
      "action": "log",
      "params": {
        "message": "Define provisioning steps"
      }
    }
  ]
}`;

export const DEFAULT_METADATA_SCHEMA = `{
  "$schema": "http://json-schema.org/draft-07/schema#",
  "type": "object",
  "properties": {}
}`;

export function templateStatus(template: Template): string {
  if (template.archived_at) {
    return 'Archived';
  }
  if (template.promoted_version?.version) {
    return `Promoted v${template.promoted_version.version}`;
  }
  return 'Draft';
}

export function parseTemplateLabels(input: string): Record<string, string> | undefined {
  const trimmed = input.trim();
  if (!trimmed) {
    return undefined;
  }
  const parsed = JSON.parse(trimmed);
  if (typeof parsed !== 'object' || parsed === null) {
    throw new Error('Labels must be a JSON object');
  }
  const normalized: Record<string, string> = {};
  for (const [key, value] of Object.entries(parsed)) {
    if (typeof value !== 'string') {
      throw new Error('Label values must be strings');
    }
    normalized[key] = value;
  }
  return normalized;
}

export function parseJsonInput(input?: string): unknown {
  if (!input) {
    return undefined;
  }
  const trimmed = input.trim();
  if (!trimmed) {
    return undefined;
  }
  return JSON.parse(trimmed);
}
