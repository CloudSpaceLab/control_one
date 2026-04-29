package compliance

import "sort"

// ControlMapping describes a single control within a compliance framework.
type ControlMapping struct {
	Framework   string
	ControlID   string
	Title       string
	Description string
}

// FrameworkControls is a catalog of controls for each supported framework.
var FrameworkControls = map[string][]ControlMapping{
	"SOC2": {
		{Framework: "SOC2", ControlID: "CC1.1", Title: "COSO Principle 1", Description: "The entity demonstrates a commitment to integrity and ethical values."},
		{Framework: "SOC2", ControlID: "CC6.1", Title: "Logical and Physical Access Controls", Description: "The entity implements logical access security software, infrastructure, and architectures."},
		{Framework: "SOC2", ControlID: "CC7.2", Title: "System Operations", Description: "The entity monitors system components for anomalies that indicate malicious acts."},
		{Framework: "SOC2", ControlID: "CC8.1", Title: "Change Management", Description: "The entity authorizes, designs, develops or acquires, configures, documents, tests, approves, and implements changes."},
		{Framework: "SOC2", ControlID: "CC9.1", Title: "Risk Mitigation", Description: "The entity identifies, selects, and develops risk mitigation activities for risks."},
		{Framework: "SOC2", ControlID: "A1.1", Title: "Availability", Description: "The entity maintains, monitors, and evaluates current processing capacity and use."},
	},
	"ISO27001": {
		{Framework: "ISO27001", ControlID: "A.5.1", Title: "Information security policies", Description: "Management direction for information security."},
		{Framework: "ISO27001", ControlID: "A.6.1", Title: "Internal organisation", Description: "Management framework to initiate and control implementation."},
		{Framework: "ISO27001", ControlID: "A.9.1", Title: "Access control policy", Description: "Limit access to information and information processing facilities."},
		{Framework: "ISO27001", ControlID: "A.12.1", Title: "Operational procedures and responsibilities", Description: "Ensure correct and secure operations of information processing."},
		{Framework: "ISO27001", ControlID: "A.16.1", Title: "Management of information security incidents", Description: "Ensure consistent and effective approach to security incidents."},
	},
	"HIPAA": {
		{Framework: "HIPAA", ControlID: "164.308(a)(1)", Title: "Security Management Process", Description: "Implement policies and procedures to prevent, detect, contain, and correct security violations."},
		{Framework: "HIPAA", ControlID: "164.308(a)(3)", Title: "Workforce Security", Description: "Implement policies to ensure workforce members have appropriate access."},
		{Framework: "HIPAA", ControlID: "164.308(a)(5)", Title: "Security Awareness and Training", Description: "Implement a security awareness and training program."},
		{Framework: "HIPAA", ControlID: "164.312(a)(1)", Title: "Access Control", Description: "Implement technical policies restricting access to authorized users."},
		{Framework: "HIPAA", ControlID: "164.312(b)", Title: "Audit Controls", Description: "Implement hardware, software, and procedural mechanisms to record activity."},
	},
	"PCI-DSS": {
		{Framework: "PCI-DSS", ControlID: "1.1", Title: "Network Security Controls", Description: "Install and maintain network security controls."},
		{Framework: "PCI-DSS", ControlID: "2.1", Title: "Vendor Defaults", Description: "Apply secure configurations to all system components."},
		{Framework: "PCI-DSS", ControlID: "6.1", Title: "Vulnerability Management", Description: "Identify security vulnerabilities and protect system components."},
		{Framework: "PCI-DSS", ControlID: "8.1", Title: "User Identification and Authentication", Description: "Define and implement policies for user identification management."},
		{Framework: "PCI-DSS", ControlID: "10.1", Title: "Logging and Monitoring", Description: "Implement audit logs to detect anomalies and suspicious activity."},
	},
	"GDPR": {
		{Framework: "GDPR", ControlID: "Art.5", Title: "Principles of processing", Description: "Personal data shall be processed lawfully, fairly and transparently."},
		{Framework: "GDPR", ControlID: "Art.25", Title: "Data protection by design", Description: "Implement appropriate technical and organisational measures."},
		{Framework: "GDPR", ControlID: "Art.32", Title: "Security of processing", Description: "Implement appropriate technical measures to ensure security."},
		{Framework: "GDPR", ControlID: "Art.33", Title: "Breach notification", Description: "Notify supervisory authority within 72 hours of becoming aware."},
	},
}

// ListFrameworks returns a sorted list of supported framework names.
func ListFrameworks() []string {
	keys := make([]string, 0, len(FrameworkControls))
	for k := range FrameworkControls {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
